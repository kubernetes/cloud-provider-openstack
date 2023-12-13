/*
Copyright 2023 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package openstack

import (
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	neutrontags "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	neutronports "github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	secgroups "github.com/gophercloud/utils/openstack/networking/v2/extensions/security/groups"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"
	"k8s.io/utils/strings/slices"

	"k8s.io/cloud-provider-openstack/pkg/metrics"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

func getSecurityGroupName(service *corev1.Service) string {
	securityGroupName := fmt.Sprintf("lb-sg-%s-%s-%s", service.UID, service.Namespace, service.Name)
	//OpenStack requires that the name of a security group is shorter than 255 bytes.
	if len(securityGroupName) > 255 {
		securityGroupName = securityGroupName[:255]
	}

	return securityGroupName
}

// applyNodeSecurityGroupIDForLB associates the security group with the ports being members of the LB on the nodes.
func applyNodeSecurityGroupIDForLB(network *gophercloud.ServiceClient, svcConf *serviceConfig, nodes []*corev1.Node, sg string) error {
	for _, node := range nodes {
		serverID, _, err := instanceIDFromProviderID(node.Spec.ProviderID)
		if err != nil {
			return fmt.Errorf("error getting server ID from the node: %w", err)
		}

		addr, _ := nodeAddressForLB(node, svcConf.preferredIPFamily)
		if addr == "" {
			// If node has no viable address let's ignore it.
			continue
		}

		listOpts := neutronports.ListOpts{DeviceID: serverID}
		allPorts, err := openstackutil.GetPorts[PortWithPortSecurity](network, listOpts)
		if err != nil {
			return err
		}

		for _, port := range allPorts {
			// You can't assign an SG to a port with port_security_enabled=false, skip them.
			if !port.PortSecurityEnabled {
				continue
			}

			// If the Security Group is already present on the port, skip it.
			if slices.Contains(port.SecurityGroups, sg) {
				continue
			}

			// Only add SGs to the port actually attached to the LB
			if !isPortMember(port, addr, svcConf.lbMemberSubnetID) {
				continue
			}

			// Add the SG to the port
			// TODO(dulek): This isn't an atomic operation. In order to protect from lost update issues we should use
			//              `revision_number` handling to make sure our update to `security_groups` field wasn't preceded
			//              by a different one. Same applies to a removal of the SG.
			newSGs := append(port.SecurityGroups, sg)
			updateOpts := neutronports.UpdateOpts{SecurityGroups: &newSGs}
			mc := metrics.NewMetricContext("port", "update")
			res := neutronports.Update(network, port.ID, updateOpts)
			if mc.ObserveRequest(res.Err) != nil {
				return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
			}
		}
	}

	return nil
}

// disassociateSecurityGroupForLB removes the given security group from the ports
func disassociateSecurityGroupForLB(network *gophercloud.ServiceClient, sg string) error {
	// Find all the ports that have the security group associated.
	listOpts := neutronports.ListOpts{SecurityGroups: []string{sg}}
	allPorts, err := openstackutil.GetPorts[neutronports.Port](network, listOpts)
	if err != nil {
		return err
	}

	// Disassocate security group and remove the tag.
	for _, port := range allPorts {
		existingSGs := sets.NewString()
		for _, sgID := range port.SecurityGroups {
			existingSGs.Insert(sgID)
		}
		existingSGs.Delete(sg)

		// Update port security groups
		newSGs := existingSGs.List()
		// TODO(dulek): This should be done using Neutron's revision_number to make sure
		//              we don't trigger a lost update issue.
		updateOpts := neutronports.UpdateOpts{SecurityGroups: &newSGs}
		mc := metrics.NewMetricContext("port", "update")
		res := neutronports.Update(network, port.ID, updateOpts)
		if mc.ObserveRequest(res.Err) != nil {
			return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
		}

		// Remove the security group ID tag from the port. Please note we don't tag ports with SG IDs anymore,
		// so this stays for backward compatibility. It's reasonable to delete it in the future. 404s are ignored.
		if slices.Contains(port.Tags, sg) {
			mc = metrics.NewMetricContext("port_tag", "delete")
			err := neutrontags.Delete(network, "ports", port.ID, sg).ExtractErr()
			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("failed to remove tag %s to port %s: %v", sg, port.ID, res.Err)
			}
		}
	}

	return nil
}

// group, if it not present.
func (lbaas *LbaasV2) ensureSecurityRule(sgRuleCreateOpts rules.CreateOpts) error {
	mc := metrics.NewMetricContext("security_group_rule", "create")
	_, err := rules.Create(lbaas.network, sgRuleCreateOpts).Extract()
	if err != nil && cpoerrors.IsConflictError(err) {
		// Conflict means the SG rule already exists, so ignoring that error.
		klog.Warningf("Security group rule already found when trying to create it. This indicates concurrent "+
			"updates to the SG %s and is unexpected", sgRuleCreateOpts.SecGroupID)
		return mc.ObserveRequest(nil)
	} else if mc.ObserveRequest(err) != nil {
		return fmt.Errorf("failed to create rule for security group %s: %v", sgRuleCreateOpts.SecGroupID, err)
	}
	return nil
}

func compareSecurityGroupRuleAndCreateOpts(rule rules.SecGroupRule, opts rules.CreateOpts) bool {
	return rule.Direction == string(opts.Direction) &&
		strings.EqualFold(rule.Protocol, string(opts.Protocol)) &&
		rule.EtherType == string(opts.EtherType) &&
		rule.RemoteIPPrefix == opts.RemoteIPPrefix &&
		rule.PortRangeMin == opts.PortRangeMin &&
		rule.PortRangeMax == opts.PortRangeMax
}

func getRulesToCreateAndDelete(wantedRules []rules.CreateOpts, existingRules []rules.SecGroupRule) ([]rules.CreateOpts, []rules.SecGroupRule) {
	toCreate := make([]rules.CreateOpts, 0, len(wantedRules))     // Max is all rules need creation
	toDelete := make([]rules.SecGroupRule, 0, len(existingRules)) // Max will be all the existing rules to be deleted
	// Surely this can be done in a more efficient way. Is it worth optimizing if most of
	// the time we'll deal with just 1 or 2 elements in each array? I doubt it.
	for _, existingRule := range existingRules {
		found := false
		for _, wantedRule := range wantedRules {
			if compareSecurityGroupRuleAndCreateOpts(existingRule, wantedRule) {
				found = true
				break
			}
		}
		if !found {
			// in existingRules but not in wantedRules, delete
			toDelete = append(toDelete, existingRule)
		}
	}
	for _, wantedRule := range wantedRules {
		found := false
		for _, existingRule := range existingRules {
			if compareSecurityGroupRuleAndCreateOpts(existingRule, wantedRule) {
				found = true
				break
			}
		}
		if !found {
			// in wantedRules but not in exisitngRules, create
			toCreate = append(toCreate, wantedRule)
		}
	}

	return toCreate, toDelete
}

// ensureAndUpdateOctaviaSecurityGroup handles the creation and update of the security group and the securiry rules for the octavia load balancer
func (lbaas *LbaasV2) ensureAndUpdateOctaviaSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) error {
	// get service ports
	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	// ensure security group for LB
	lbSecGroupName := getSecurityGroupName(apiService)
	lbSecGroupID, err := secgroups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		// If the security group of LB not exist, create it later
		if cpoerrors.IsNotFound(err) {
			lbSecGroupID = ""
		} else {
			return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
		}
	}
	if len(lbSecGroupID) == 0 {
		// create security group
		lbSecGroupCreateOpts := groups.CreateOpts{
			Name:        lbSecGroupName,
			Description: fmt.Sprintf("Security Group for %s/%s Service LoadBalancer in cluster %s", apiService.Namespace, apiService.Name, clusterName),
		}

		mc := metrics.NewMetricContext("security_group", "create")
		lbSecGroup, err := groups.Create(lbaas.network, lbSecGroupCreateOpts).Extract()
		if mc.ObserveRequest(err) != nil {
			return fmt.Errorf("failed to create Security Group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
		}
		lbSecGroupID = lbSecGroup.ID
	}

	mc := metrics.NewMetricContext("subnet", "get")
	subnet, err := subnets.Get(lbaas.network, svcConf.lbMemberSubnetID).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf(
			"failed to find subnet %s from openstack: %v", svcConf.lbMemberSubnetID, err)
	}

	etherType := rules.EtherType4
	if netutils.IsIPv6CIDRString(subnet.CIDR) {
		etherType = rules.EtherType6
	}
	cidrs := []string{subnet.CIDR}
	if lbaas.opts.LBProvider == "ovn" {
		// OVN keeps the source IP of the incoming traffic. This means that we cannot just open the LB range, but we
		// need to open for the whole world. This can be restricted by using the service.spec.loadBalancerSourceRanges.
		// svcConf.allowedCIDR will give us the ranges calculated by GetLoadBalancerSourceRanges() earlier.
		cidrs = svcConf.allowedCIDR
	}

	existingRules, err := openstackutil.GetSecurityGroupRules(lbaas.network, rules.ListOpts{SecGroupID: lbSecGroupID})
	if err != nil {
		return fmt.Errorf(
			"failed to find security group rules in %s: %v", lbSecGroupID, err)
	}

	// List of the security group rules wanted in the SG.
	// Number of Ports plus the potential HealthCheckNodePort.
	wantedRules := make([]rules.CreateOpts, 0, len(ports)+1)

	if apiService.Spec.HealthCheckNodePort != 0 {
		// TODO(dulek): How should this work with OVN…? Do we need to allow all?
		//              Probably the traffic goes from the compute node?
		wantedRules = append(wantedRules,
			rules.CreateOpts{
				Direction:      rules.DirIngress,
				Protocol:       rules.ProtocolTCP,
				EtherType:      etherType,
				RemoteIPPrefix: subnet.CIDR,
				SecGroupID:     lbSecGroupID,
				PortRangeMin:   int(apiService.Spec.HealthCheckNodePort),
				PortRangeMax:   int(apiService.Spec.HealthCheckNodePort),
			},
		)
	}

	for _, port := range ports {
		if port.NodePort == 0 { // It's 0 when AllocateLoadBalancerNodePorts=False
			continue
		}
		for _, cidr := range cidrs {
			protocol := strings.ToLower(string(port.Protocol)) // K8s uses TCP, Neutron uses tcp, etc.
			wantedRules = append(wantedRules,
				rules.CreateOpts{
					Direction:      rules.DirIngress,
					Protocol:       rules.RuleProtocol(protocol),
					EtherType:      etherType,
					RemoteIPPrefix: cidr,
					SecGroupID:     lbSecGroupID,
					PortRangeMin:   int(port.NodePort),
					PortRangeMax:   int(port.NodePort),
				},
			)
		}
	}

	toCreate, toDelete := getRulesToCreateAndDelete(wantedRules, existingRules)

	// create new rules
	for _, opts := range toCreate {
		err := lbaas.ensureSecurityRule(opts)
		if err != nil {
			return fmt.Errorf("failed to apply security rule (%v), %w", opts, err)
		}
	}

	// delete unneeded rules
	for _, existingRule := range toDelete {
		klog.Infof("Deleting rule %s from security group %s (%s)", existingRule.ID, existingRule.SecGroupID, lbSecGroupName)
		mc := metrics.NewMetricContext("security_group_rule", "delete")
		err := rules.Delete(lbaas.network, existingRule.ID).ExtractErr()
		if err != nil && cpoerrors.IsNotFound(err) {
			// ignore 404
			klog.Warningf("Security group rule %s found missing when trying to delete it. This indicates concurrent "+
				"updates to the SG %s and is unexpected", existingRule.ID, existingRule.SecGroupID)
			return mc.ObserveRequest(nil)
		} else if mc.ObserveRequest(err) != nil {
			return fmt.Errorf("failed to delete security group rule %s: %w", existingRule.ID, err)
		}
	}

	if err := applyNodeSecurityGroupIDForLB(lbaas.network, svcConf, nodes, lbSecGroupID); err != nil {
		return err
	}
	return nil
}

// ensureSecurityGroupDeleted deleting security group for specific loadbalancer service.
func (lbaas *LbaasV2) ensureSecurityGroupDeleted(_ string, service *corev1.Service) error {
	// Generate Name
	lbSecGroupName := getSecurityGroupName(service)
	lbSecGroupID, err := secgroups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			// It is OK when the security group has been deleted by others.
			return nil
		}
		return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
	}

	// Disassociate the security group from the neutron ports on the nodes.
	if err := disassociateSecurityGroupForLB(lbaas.network, lbSecGroupID); err != nil {
		return fmt.Errorf("failed to disassociate security group %s: %v", lbSecGroupID, err)
	}

	mc := metrics.NewMetricContext("security_group", "delete")
	lbSecGroup := groups.Delete(lbaas.network, lbSecGroupID)
	if lbSecGroup.Err != nil && !cpoerrors.IsNotFound(lbSecGroup.Err) {
		return mc.ObserveRequest(lbSecGroup.Err)
	}
	_ = mc.ObserveRequest(nil)

	return nil
}
