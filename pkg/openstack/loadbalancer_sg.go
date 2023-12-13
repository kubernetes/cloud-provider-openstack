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
	"reflect"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	neutrontags "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	neutronports "github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	secgroups "github.com/gophercloud/utils/openstack/networking/v2/extensions/security/groups"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"

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

// applyNodeSecurityGroupIDForLB associates the security group with all the ports on the nodes.
func applyNodeSecurityGroupIDForLB(compute *gophercloud.ServiceClient, network *gophercloud.ServiceClient, nodes []*corev1.Node, sg string) error {
	for _, node := range nodes {
		nodeName := types.NodeName(node.Name)
		srv, err := getServerByName(compute, nodeName)
		if err != nil {
			return err
		}

		listOpts := neutronports.ListOpts{DeviceID: srv.ID}
		allPorts, err := openstackutil.GetPorts(network, listOpts)
		if err != nil {
			return err
		}

		for _, port := range allPorts {
			// If the Security Group is already present on the port, skip it.
			// As soon as this only supports Go 1.18, this can be replaces by
			// slices.Contains.
			if func() bool {
				for _, currentSG := range port.SecurityGroups {
					if currentSG == sg {
						return true
					}
				}
				return false
			}() {
				continue
			}

			newSGs := append(port.SecurityGroups, sg)
			updateOpts := neutronports.UpdateOpts{SecurityGroups: &newSGs}
			mc := metrics.NewMetricContext("port", "update")
			res := neutronports.Update(network, port.ID, updateOpts)
			if mc.ObserveRequest(res.Err) != nil {
				return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
			}
			// Add the security group ID as a tag to the port in order to find all these ports when removing the security group.
			mc = metrics.NewMetricContext("port_tag", "add")
			err := neutrontags.Add(network, "ports", port.ID, sg).ExtractErr()
			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("failed to add tag %s to port %s: %v", sg, port.ID, err)
			}
		}
	}

	return nil
}

// disassociateSecurityGroupForLB removes the given security group from the ports
func disassociateSecurityGroupForLB(network *gophercloud.ServiceClient, sg string) error {
	// Find all the ports that have the security group associated.
	listOpts := neutronports.ListOpts{TagsAny: sg}
	allPorts, err := openstackutil.GetPorts(network, listOpts)
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
		updateOpts := neutronports.UpdateOpts{SecurityGroups: &newSGs}
		mc := metrics.NewMetricContext("port", "update")
		res := neutronports.Update(network, port.ID, updateOpts)
		if mc.ObserveRequest(res.Err) != nil {
			return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
		}
		// Remove the security group ID tag from the port.
		mc = metrics.NewMetricContext("port_tag", "delete")
		err := neutrontags.Delete(network, "ports", port.ID, sg).ExtractErr()
		if mc.ObserveRequest(err) != nil {
			return fmt.Errorf("failed to remove tag %s to port %s: %v", sg, port.ID, res.Err)
		}
	}

	return nil
}

// isSecurityGroupNotFound return true while 'err' is object of gophercloud.ErrResourceNotFound
func isSecurityGroupNotFound(err error) bool {
	errType := reflect.TypeOf(err).String()
	errTypeSlice := strings.Split(errType, ".")
	errTypeValue := ""
	if len(errTypeSlice) != 0 {
		errTypeValue = errTypeSlice[len(errTypeSlice)-1]
	}
	if errTypeValue == "ErrResourceNotFound" {
		return true
	}

	return false
}

// group, if it not present.
func (lbaas *LbaasV2) ensureSecurityRule(
	direction rules.RuleDirection,
	protocol rules.RuleProtocol,
	etherType rules.RuleEtherType,
	remoteIPPrefix, secGroupID string,
	portRangeMin, portRangeMax int,
) error {
	sgListopts := rules.ListOpts{
		Direction:      string(direction),
		Protocol:       string(protocol),
		PortRangeMax:   portRangeMin,
		PortRangeMin:   portRangeMax,
		RemoteIPPrefix: remoteIPPrefix,
		SecGroupID:     secGroupID,
	}
	sgRules, err := openstackutil.GetSecurityGroupRules(lbaas.network, sgListopts)
	if err != nil && !cpoerrors.IsNotFound(err) {
		return fmt.Errorf(
			"failed to find security group rules in %s: %v", secGroupID, err)
	}
	if len(sgRules) != 0 {
		return nil
	}

	sgRuleCreateOpts := rules.CreateOpts{
		Direction:      direction,
		Protocol:       protocol,
		PortRangeMax:   portRangeMin,
		PortRangeMin:   portRangeMax,
		RemoteIPPrefix: remoteIPPrefix,
		SecGroupID:     secGroupID,
		EtherType:      etherType,
	}

	mc := metrics.NewMetricContext("security_group_rule", "create")
	_, err = rules.Create(lbaas.network, sgRuleCreateOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf(
			"failed to create rule for security group %s: %v",
			secGroupID, err)
	}
	return nil
}

// ensureSecurityGroup ensures security group exist for specific loadbalancer service.
// Creating security group for specific loadbalancer service when it does not exist.
func (lbaas *LbaasV2) ensureSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node,
	loadbalancer *loadbalancers.LoadBalancer, preferredIPFamily corev1.IPFamily, memberSubnetID string) error {

	return lbaas.ensureAndUpdateOctaviaSecurityGroup(clusterName, apiService, nodes, memberSubnetID)
}

// ensureAndUpdateOctaviaSecurityGroup handles the creation and update of the security group and the securiry rules for the octavia load balancer
func (lbaas *LbaasV2) ensureAndUpdateOctaviaSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node, memberSubnetID string) error {
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
		if isSecurityGroupNotFound(err) {
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
	subnet, err := subnets.Get(lbaas.network, memberSubnetID).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf(
			"failed to find subnet %s from openstack: %v", memberSubnetID, err)
	}

	etherType := rules.EtherType4
	if netutils.IsIPv6CIDRString(subnet.CIDR) {
		etherType = rules.EtherType6
	}

	if apiService.Spec.HealthCheckNodePort != 0 {
		err = lbaas.ensureSecurityRule(
			rules.DirIngress,
			rules.ProtocolTCP,
			etherType,
			subnet.CIDR,
			lbSecGroupID,
			int(apiService.Spec.HealthCheckNodePort),
			int(apiService.Spec.HealthCheckNodePort),
		)
		if err != nil {
			return fmt.Errorf(
				"failed to apply security rule for health check node port, %w",
				err)
		}
	}

	// ensure rules for node security group
	for _, port := range ports {
		if port.NodePort == 0 { // It's 0 when AllocateLoadBalancerNodePorts=False
			continue
		}
		err = lbaas.ensureSecurityRule(
			rules.DirIngress,
			rules.RuleProtocol(port.Protocol),
			etherType,
			subnet.CIDR,
			lbSecGroupID,
			int(port.NodePort),
			int(port.NodePort),
		)
		if err != nil {
			return fmt.Errorf(
				"failed to apply security rule for port %d, %w",
				port.NodePort, err)
		}

		if err := applyNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes, lbSecGroupID); err != nil {
			return err
		}
	}
	return nil
}

// updateSecurityGroup updating security group for specific loadbalancer service.
func (lbaas *LbaasV2) updateSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node, memberSubnetID string) error {
	return lbaas.ensureAndUpdateOctaviaSecurityGroup(clusterName, apiService, nodes, memberSubnetID)
}

// EnsureSecurityGroupDeleted deleting security group for specific loadbalancer service.
func (lbaas *LbaasV2) EnsureSecurityGroupDeleted(_ string, service *corev1.Service) error {
	// Generate Name
	lbSecGroupName := getSecurityGroupName(service)
	lbSecGroupID, err := secgroups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		if isSecurityGroupNotFound(err) {
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

	if len(lbaas.opts.NodeSecurityGroupIDs) == 0 {
		// Just happen when nodes have not Security Group, or should not happen
		// UpdateLoadBalancer and EnsureLoadBalancer can set lbaas.opts.NodeSecurityGroupIDs when it is empty
		// And service controller call UpdateLoadBalancer to set lbaas.opts.NodeSecurityGroupIDs when controller manager service is restarted.
		klog.Warningf("Can not find node-security-group from all the nodes of this cluster when delete loadbalancer service %s/%s",
			service.Namespace, service.Name)
	} else {
		// Delete the rules in the Node Security Group
		for _, nodeSecurityGroupID := range lbaas.opts.NodeSecurityGroupIDs {
			opts := rules.ListOpts{
				SecGroupID:    nodeSecurityGroupID,
				RemoteGroupID: lbSecGroupID,
			}
			secGroupRules, err := openstackutil.GetSecurityGroupRules(lbaas.network, opts)

			if err != nil && !cpoerrors.IsNotFound(err) {
				msg := fmt.Sprintf("error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, nodeSecurityGroupID, err)
				return fmt.Errorf(msg)
			}

			for _, rule := range secGroupRules {
				mc := metrics.NewMetricContext("security_group_rule", "delete")
				res := rules.Delete(lbaas.network, rule.ID)
				if res.Err != nil && !cpoerrors.IsNotFound(res.Err) {
					_ = mc.ObserveRequest(res.Err)
					return fmt.Errorf("error occurred deleting security group rule: %s: %v", rule.ID, res.Err)
				}
				_ = mc.ObserveRequest(nil)
			}
		}
	}

	return nil
}
