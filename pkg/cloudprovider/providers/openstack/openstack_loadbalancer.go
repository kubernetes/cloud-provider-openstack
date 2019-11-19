/*
Copyright 2016 The Kubernetes Authors.

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
	"context"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	v2monitors "github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/monitors"
	v2pools "github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions"
	neutrontags "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/external"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/networks"
	neutronports "github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	"github.com/gophercloud/gophercloud/pagination"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog"

	v1service "k8s.io/cloud-provider-openstack/pkg/api/v1/service"
	cpoutil "k8s.io/cloud-provider-openstack/pkg/util"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

// Note: when creating a new Loadbalancer (VM), it can take some time before it is ready for use,
// this timeout is used for waiting until the Loadbalancer provisioning status goes to ACTIVE state.
const (
	// loadbalancerActive* is configuration of exponential backoff for
	// going into ACTIVE loadbalancer provisioning status. Starting with 1
	// seconds, multiplying by 1.2 with each step and taking 19 steps at maximum
	// it will time out after 128s, which roughly corresponds to 120s
	loadbalancerActiveInitDelay = 1 * time.Second
	loadbalancerActiveFactor    = 1.2
	loadbalancerActiveSteps     = 19

	// loadbalancerDelete* is configuration of exponential backoff for
	// waiting for delete operation to complete. Starting with 1
	// seconds, multiplying by 1.2 with each step and taking 13 steps at maximum
	// it will time out after 32s, which roughly corresponds to 30s
	loadbalancerDeleteInitDelay = 1 * time.Second
	loadbalancerDeleteFactor    = 1.2
	loadbalancerDeleteSteps     = 13

	activeStatus = "ACTIVE"
	errorStatus  = "ERROR"

	ServiceAnnotationLoadBalancerConnLimit            = "loadbalancer.openstack.org/connection-limit"
	ServiceAnnotationLoadBalancerFloatingNetworkID    = "loadbalancer.openstack.org/floating-network-id"
	ServiceAnnotationLoadBalancerFloatingSubnet       = "loadbalancer.openstack.org/floating-subnet"
	ServiceAnnotationLoadBalancerFloatingSubnetID     = "loadbalancer.openstack.org/floating-subnet-id"
	ServiceAnnotationLoadBalancerInternal             = "service.beta.kubernetes.io/openstack-internal-load-balancer"
	ServiceAnnotationLoadBalancerClass                = "loadbalancer.openstack.org/class"
	ServiceAnnotationLoadBalancerKeepFloatingIP       = "loadbalancer.openstack.org/keep-floatingip"
	ServiceAnnotationLoadBalancerPortID               = "loadbalancer.openstack.org/port-id"
	ServiceAnnotationLoadBalancerProxyEnabled         = "loadbalancer.openstack.org/proxy-protocol"
	ServiceAnnotationLoadBalancerSubnetID             = "loadbalancer.openstack.org/subnet-id"
	ServiceAnnotationLoadBalancerTimeoutClientData    = "loadbalancer.openstack.org/timeout-client-data"
	ServiceAnnotationLoadBalancerTimeoutMemberConnect = "loadbalancer.openstack.org/timeout-member-connect"
	ServiceAnnotationLoadBalancerTimeoutMemberData    = "loadbalancer.openstack.org/timeout-member-data"
	ServiceAnnotationLoadBalancerTimeoutTCPInspect    = "loadbalancer.openstack.org/timeout-tcp-inspect"
	ServiceAnnotationLoadBalancerXForwardedFor        = "loadbalancer.openstack.org/x-forwarded-for"

	// ServiceAnnotationLoadBalancerInternal is the annotation used on the service
	// to indicate that we want an internal loadbalancer service.
	// If the value of ServiceAnnotationLoadBalancerInternal is false, it indicates that we want an external loadbalancer service. Default to false.
)

// LbaasV2 is a LoadBalancer implementation for Neutron LBaaS v2 API
type LbaasV2 struct {
	LoadBalancer
}

func networkExtensions(client *gophercloud.ServiceClient) (map[string]bool, error) {
	seen := make(map[string]bool)

	pager := extensions.List(client)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		exts, err := extensions.ExtractExtensions(page)
		if err != nil {
			return false, err
		}
		for _, ext := range exts {
			seen[ext.Alias] = true
		}
		return true, nil
	})

	return seen, err
}

func getLoadBalancers(client *gophercloud.ServiceClient, opts loadbalancers.ListOpts) ([]loadbalancers.LoadBalancer, error) {
	allPages, err := loadbalancers.List(client, opts).AllPages()
	if err != nil {
		return nil, err
	}
	allLoadbalancers, err := loadbalancers.ExtractLoadBalancers(allPages)
	if err != nil {
		return nil, err
	}

	return allLoadbalancers, nil
}

// getLoadbalancerByName get the load balancer which is in valid status by the given name/legacy name.
func getLoadbalancerByName(client *gophercloud.ServiceClient, name string, legacyName string) (*loadbalancers.LoadBalancer, error) {
	var validLBs []loadbalancers.LoadBalancer

	opts := loadbalancers.ListOpts{
		Name: name,
	}
	allLoadbalancers, err := getLoadBalancers(client, opts)
	if err != nil {
		return nil, err
	}

	if len(allLoadbalancers) == 0 {
		if len(legacyName) > 0 {
			// Backoff to get load balnacer by legacy name.
			opts := loadbalancers.ListOpts{
				Name: legacyName,
			}
			allLoadbalancers, err = getLoadBalancers(client, opts)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, ErrNotFound
		}
	}

	for _, lb := range allLoadbalancers {
		// All the ProvisioningStatus could be found here https://developer.openstack.org/api-ref/load-balancer/v2/index.html#provisioning-status-codes
		if lb.ProvisioningStatus != "DELETED" && lb.ProvisioningStatus != "PENDING_DELETE" {
			validLBs = append(validLBs, lb)
		}
	}

	if len(validLBs) > 1 {
		return nil, ErrMultipleResults
	}
	if len(validLBs) == 0 {
		return nil, ErrNotFound
	}

	return &validLBs[0], nil
}

func getListenersByLoadBalancerID(client *gophercloud.ServiceClient, id string) ([]listeners.Listener, error) {
	var existingListeners []listeners.Listener
	err := listeners.List(client, listeners.ListOpts{LoadbalancerID: id}).EachPage(func(page pagination.Page) (bool, error) {
		listenerList, err := listeners.ExtractListeners(page)
		if err != nil {
			return false, err
		}
		for _, l := range listenerList {
			for _, lb := range l.Loadbalancers {
				if lb.ID == id {
					existingListeners = append(existingListeners, l)
					break
				}
			}
		}

		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return existingListeners, nil
}

// get listener for a port or nil if does not exist
func getListenerForPort(existingListeners []listeners.Listener, port corev1.ServicePort) *listeners.Listener {
	for _, l := range existingListeners {
		if listeners.Protocol(l.Protocol) == toListenersProtocol(port.Protocol) && l.ProtocolPort == int(port.Port) {
			return &l
		}
	}

	return nil
}

// Get pool for a listener. A listener always has exactly one pool.
func getPoolByListenerID(client *gophercloud.ServiceClient, loadbalancerID string, listenerID string) (*v2pools.Pool, error) {
	listenerPools := make([]v2pools.Pool, 0, 1)
	err := v2pools.List(client, v2pools.ListOpts{LoadbalancerID: loadbalancerID}).EachPage(func(page pagination.Page) (bool, error) {
		poolsList, err := v2pools.ExtractPools(page)
		if err != nil {
			return false, err
		}
		for _, p := range poolsList {
			for _, l := range p.Listeners {
				if l.ID == listenerID {
					listenerPools = append(listenerPools, p)
				}
			}
		}
		if len(listenerPools) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(listenerPools) == 0 {
		return nil, ErrNotFound
	} else if len(listenerPools) > 1 {
		return nil, ErrMultipleResults
	}

	return &listenerPools[0], nil
}

func getMembersByPoolID(client *gophercloud.ServiceClient, id string) ([]v2pools.Member, error) {
	var members []v2pools.Member
	err := v2pools.ListMembers(client, id, v2pools.ListMembersOpts{}).EachPage(func(page pagination.Page) (bool, error) {
		membersList, err := v2pools.ExtractMembers(page)
		if err != nil {
			return false, err
		}
		members = append(members, membersList...)

		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return members, nil
}

// Check if a member exists for node
func memberExists(members []v2pools.Member, addr string, port int) bool {
	for _, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			return true
		}
	}

	return false
}

func popListener(existingListeners []listeners.Listener, id string) []listeners.Listener {
	for i, existingListener := range existingListeners {
		if existingListener.ID == id {
			existingListeners[i] = existingListeners[len(existingListeners)-1]
			existingListeners = existingListeners[:len(existingListeners)-1]
			break
		}
	}

	return existingListeners
}

func popMember(members []v2pools.Member, addr string, port int) []v2pools.Member {
	for i, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			members[i] = members[len(members)-1]
			members = members[:len(members)-1]
		}
	}

	return members
}

func getSecurityGroupName(service *corev1.Service) string {
	securityGroupName := fmt.Sprintf("lb-sg-%s-%s-%s", service.UID, service.Namespace, service.Name)
	//OpenStack requires that the name of a security group is shorter than 255 bytes.
	if len(securityGroupName) > 255 {
		securityGroupName = securityGroupName[:255]
	}

	return securityGroupName
}

func getSecurityGroupRules(client *gophercloud.ServiceClient, opts rules.ListOpts) ([]rules.SecGroupRule, error) {

	pager := rules.List(client, opts)

	var securityRules []rules.SecGroupRule

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		ruleList, err := rules.ExtractRules(page)
		if err != nil {
			return false, err
		}
		securityRules = append(securityRules, ruleList...)
		return true, nil
	})

	if err != nil {
		return nil, err
	}

	return securityRules, nil
}

func waitLoadbalancerActiveProvisioningStatus(client *gophercloud.ServiceClient, loadbalancerID string) (string, error) {
	backoff := wait.Backoff{
		Duration: loadbalancerActiveInitDelay,
		Factor:   loadbalancerActiveFactor,
		Steps:    loadbalancerActiveSteps,
	}

	var provisioningStatus string
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		loadbalancer, err := loadbalancers.Get(client, loadbalancerID).Extract()
		if err != nil {
			return false, err
		}
		provisioningStatus = loadbalancer.ProvisioningStatus
		if loadbalancer.ProvisioningStatus == activeStatus {
			return true, nil
		} else if loadbalancer.ProvisioningStatus == errorStatus {
			return true, fmt.Errorf("loadbalancer has gone into ERROR state")
		} else {
			return false, nil
		}

	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("loadbalancer failed to go into ACTIVE provisioning status within allotted time")
	}
	return provisioningStatus, err
}

func waitLoadbalancerDeleted(client *gophercloud.ServiceClient, loadbalancerID string) error {
	backoff := wait.Backoff{
		Duration: loadbalancerDeleteInitDelay,
		Factor:   loadbalancerDeleteFactor,
		Steps:    loadbalancerDeleteSteps,
	}
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		_, err := loadbalancers.Get(client, loadbalancerID).Extract()
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("loadbalancer failed to delete within the allotted time")
	}

	return err
}

func toRuleProtocol(protocol corev1.Protocol) rules.RuleProtocol {
	switch protocol {
	case corev1.ProtocolTCP:
		return rules.ProtocolTCP
	case corev1.ProtocolUDP:
		return rules.ProtocolUDP
	default:
		return rules.RuleProtocol(strings.ToLower(string(protocol)))
	}
}

func toListenersProtocol(protocol corev1.Protocol) listeners.Protocol {
	switch protocol {
	case corev1.ProtocolTCP:
		return listeners.ProtocolTCP
	default:
		return listeners.Protocol(string(protocol))
	}
}

func createNodeSecurityGroup(client *gophercloud.ServiceClient, nodeSecurityGroupID string, port int, protocol corev1.Protocol, lbSecGroup string) error {
	v4NodeSecGroupRuleCreateOpts := rules.CreateOpts{
		Direction:     rules.DirIngress,
		PortRangeMax:  port,
		PortRangeMin:  port,
		Protocol:      toRuleProtocol(protocol),
		RemoteGroupID: lbSecGroup,
		SecGroupID:    nodeSecurityGroupID,
		EtherType:     rules.EtherType4,
	}

	v6NodeSecGroupRuleCreateOpts := rules.CreateOpts{
		Direction:     rules.DirIngress,
		PortRangeMax:  port,
		PortRangeMin:  port,
		Protocol:      toRuleProtocol(protocol),
		RemoteGroupID: lbSecGroup,
		SecGroupID:    nodeSecurityGroupID,
		EtherType:     rules.EtherType6,
	}

	_, err := rules.Create(client, v4NodeSecGroupRuleCreateOpts).Extract()

	if err != nil {
		return err
	}

	_, err = rules.Create(client, v6NodeSecGroupRuleCreateOpts).Extract()

	if err != nil {
		return err
	}
	return nil
}

func (lbaas *LbaasV2) createLoadBalancer(service *corev1.Service, name, clusterName string, lbClass *LBClass, internalAnnotation bool, vipPort string) (*loadbalancers.LoadBalancer, error) {
	createOpts := loadbalancers.CreateOpts{
		Name:        name,
		Description: fmt.Sprintf("Kubernetes external service %s/%s from cluster %s", service.Namespace, service.Name, clusterName),
		Provider:    lbaas.opts.LBProvider,
	}

	if vipPort != "" {
		createOpts.VipPortID = vipPort
	} else {
		if lbClass != nil && lbClass.SubnetID != "" {
			createOpts.VipSubnetID = lbClass.SubnetID
		} else {
			createOpts.VipSubnetID = lbaas.opts.SubnetID
		}
	}

	loadBalancerIP := service.Spec.LoadBalancerIP
	if loadBalancerIP != "" && internalAnnotation {
		createOpts.VipAddress = loadBalancerIP
	}

	loadbalancer, err := loadbalancers.Create(lbaas.lb, createOpts).Extract()
	if err != nil {
		return nil, fmt.Errorf("error creating loadbalancer %v: %v", createOpts, err)
	}
	return loadbalancer, nil
}

// GetLoadBalancer returns whether the specified load balancer exists and its status
func (lbaas *LbaasV2) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (*corev1.LoadBalancerStatus, bool, error) {
	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, service)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err == ErrNotFound {
		return nil, false, nil
	}
	if loadbalancer == nil {
		return nil, false, err
	}

	status := &corev1.LoadBalancerStatus{}

	portID := loadbalancer.VipPortID
	if portID != "" {
		floatIP, err := openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
		if err != nil {
			return nil, false, fmt.Errorf("failed when trying to get floating IP for port %s: %v", portID, err)
		}
		if floatIP != nil {
			status.Ingress = []corev1.LoadBalancerIngress{{IP: floatIP.FloatingIP}}
		} else {
			status.Ingress = []corev1.LoadBalancerIngress{{IP: loadbalancer.VipAddress}}
		}
	}

	return status, true, nil
}

// GetLoadBalancerName returns the constructed load balancer name.
func (lbaas *LbaasV2) GetLoadBalancerName(ctx context.Context, clusterName string, service *corev1.Service) string {
	name := fmt.Sprintf("kube_service_%s_%s_%s", clusterName, service.Namespace, service.Name)
	return cutString(name)
}

// GetLoadBalancerLegacyName returns the legacy load balancer name for backward compatibility.
func (lbaas *LbaasV2) GetLoadBalancerLegacyName(ctx context.Context, clusterName string, service *corev1.Service) string {
	return cloudprovider.DefaultLoadBalancerName(service)
}

// cutString makes sure the string length doesn't exceed 255, which is usually the maximum string length in OpenStack.
func cutString(original string) string {
	ret := original
	if len(original) > 255 {
		ret = original[:255]
	}
	return ret
}

// The LB needs to be configured with instance addresses on the same
// subnet as the LB (aka opts.SubnetID).  Currently we're just
// guessing that the node's InternalIP is the right address.
// In case no InternalIP can be found, ExternalIP is tried.
// If neither InternalIP nor ExternalIP can be found an error is
// returned.
func nodeAddressForLB(node *corev1.Node) (string, error) {
	addrs := node.Status.Addresses
	if len(addrs) == 0 {
		return "", ErrNoAddressFound
	}

	allowedAddrTypes := []corev1.NodeAddressType{corev1.NodeInternalIP, corev1.NodeExternalIP}

	for _, allowedAddrType := range allowedAddrTypes {
		for _, addr := range addrs {
			if addr.Type == allowedAddrType {
				return addr.Address, nil
			}
		}
	}

	return "", ErrNoAddressFound
}

//getStringFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's value or a specified defaultSetting
func getStringFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting string) string {
	klog.V(4).Infof("getStringFromServiceAnnotation(%v, %v, %v)", service, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		//if there is an annotation for this setting, set the "setting" var to it
		// annotationValue can be empty, it is working as designed
		// it makes possible for instance provisioning loadbalancer without floatingip
		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, annotationValue)
		return annotationValue
	}
	//if there is no annotation, set "settings" var to the value from cloud config
	klog.V(4).Infof("Could not find a Service Annotation; falling back on cloud-config setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting
}

func getIntFromServiceAnnotation(service *corev1.Service, annotationKey string) (int, bool) {
	intString := getStringFromServiceAnnotation(service, annotationKey, "")
	if len(intString) > 0 {
		annotationValue, err := strconv.Atoi(intString)
		if err == nil {
			klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, annotationValue)
			return annotationValue, true
		}
	}
	return 0, false
}

//getBoolFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's value or a specified defaultSetting
func getBoolFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting bool) (bool, error) {
	klog.V(4).Infof("getBoolFromServiceAnnotation(%v, %v, %v)", service, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		returnValue := false
		switch annotationValue {
		case "true":
			returnValue = true
		case "false":
			returnValue = false
		default:
			return returnValue, fmt.Errorf("unknown %s annotation: %v, specify \"true\" or \"false\" ", annotationKey, annotationValue)
		}

		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, returnValue)
		return returnValue, nil
	}
	klog.V(4).Infof("Could not find a Service Annotation; falling back to default setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting, nil
}

// getSubnetIDForLB returns subnet-id for a specific node
func getSubnetIDForLB(compute *gophercloud.ServiceClient, node corev1.Node) (string, error) {
	ipAddress, err := nodeAddressForLB(&node)
	if err != nil {
		return "", err
	}

	instanceID := node.Spec.ProviderID
	if ind := strings.LastIndex(instanceID, "/"); ind >= 0 {
		instanceID = instanceID[(ind + 1):]
	}

	interfaces, err := getAttachedInterfacesByID(compute, instanceID)
	if err != nil {
		return "", err
	}

	for _, intf := range interfaces {
		for _, fixedIP := range intf.FixedIPs {
			if fixedIP.IPAddress == ipAddress {
				return fixedIP.SubnetID, nil
			}
		}
	}

	return "", ErrNotFound
}

// getPorts gets all the filtered ports.
func getPorts(network *gophercloud.ServiceClient, listOpts neutronports.ListOpts) ([]neutronports.Port, error) {
	allPages, err := neutronports.List(network, listOpts).AllPages()
	if err != nil {
		return []neutronports.Port{}, err
	}
	allPorts, err := neutronports.ExtractPorts(allPages)
	if err != nil {
		return []neutronports.Port{}, err
	}

	return allPorts, nil
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
		allPorts, err := getPorts(network, listOpts)
		if err != nil {
			return err
		}

		for _, port := range allPorts {
			newSGs := append(port.SecurityGroups, sg)
			updateOpts := neutronports.UpdateOpts{SecurityGroups: &newSGs}
			res := neutronports.Update(network, port.ID, updateOpts)
			if res.Err != nil {
				return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
			}
			// Add the security group ID as a tag to the port in order to find all these ports when removing the security group.
			if err := neutrontags.Add(network, "ports", port.ID, sg).ExtractErr(); err != nil {
				return fmt.Errorf("failed to add tag %s to port %s: %v", sg, port.ID, res.Err)
			}
		}
	}

	return nil
}

// disassociateSecurityGroupForLB removes the given security group from the ports
func disassociateSecurityGroupForLB(network *gophercloud.ServiceClient, sg string) error {
	// Find all the ports that have the security group associated.
	listOpts := neutronports.ListOpts{TagsAny: sg}
	allPorts, err := getPorts(network, listOpts)
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
		res := neutronports.Update(network, port.ID, updateOpts)
		if res.Err != nil {
			return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
		}
		// Remove the security group ID tag from the port.
		if err := neutrontags.Delete(network, "ports", port.ID, sg).ExtractErr(); err != nil {
			return fmt.Errorf("failed to remove tag %s to port %s: %v", sg, port.ID, res.Err)
		}
	}

	return nil
}

// getNodeSecurityGroupIDForLB lists node-security-groups for specific nodes
func getNodeSecurityGroupIDForLB(compute *gophercloud.ServiceClient, network *gophercloud.ServiceClient, nodes []*corev1.Node) ([]string, error) {
	secGroupIDs := sets.NewString()

	for _, node := range nodes {
		nodeName := types.NodeName(node.Name)
		srv, err := getServerByName(compute, nodeName)
		if err != nil {
			return []string{}, err
		}

		// Get the security groups of all the ports on the worker nodes. In future, we could filter the ports by some way.
		// case 0: node1:SG1  node2:SG1  return SG1
		// case 1: node1:SG1  node2:SG2  return SG1,SG2
		// case 2: node1:SG1,SG2  node2:SG3,SG4  return SG1,SG2,SG3,SG4
		// case 3: node1:SG1,SG2  node2:SG2,SG3  return SG1,SG2,SG3
		listOpts := neutronports.ListOpts{DeviceID: srv.ID}
		allPorts, err := getPorts(network, listOpts)
		if err != nil {
			return []string{}, err
		}

		for _, port := range allPorts {
			for _, sg := range port.SecurityGroups {
				secGroupIDs.Insert(sg)
			}
		}
	}

	return secGroupIDs.List(), nil
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

// getFloatingNetworkIDForLB returns a floating-network-id for cluster.
func getFloatingNetworkIDForLB(client *gophercloud.ServiceClient) (string, error) {
	var floatingNetworkIds []string

	type NetworkWithExternalExt struct {
		networks.Network
		external.NetworkExternalExt
	}

	err := networks.List(client, networks.ListOpts{}).EachPage(func(page pagination.Page) (bool, error) {
		var externalNetwork []NetworkWithExternalExt
		err := networks.ExtractNetworksInto(page, &externalNetwork)
		if err != nil {
			return false, err
		}

		for _, externalNet := range externalNetwork {
			if externalNet.External {
				floatingNetworkIds = append(floatingNetworkIds, externalNet.ID)
			}
		}

		if len(floatingNetworkIds) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return "", ErrNotFound
		}

		if err == ErrMultipleResults {
			klog.V(4).Infof("find multiple external networks, pick the first one when there are no explicit configuration.")
			return floatingNetworkIds[0], nil
		}
		return "", err
	}

	if len(floatingNetworkIds) == 0 {
		return "", ErrNotFound
	}

	return floatingNetworkIds[0], nil
}

// TODO: This code currently ignores 'region' and always creates a
// loadbalancer in only the current OpenStack region.  We should take
// a list of regions (from config) and query/create loadbalancers in
// each region.

// EnsureLoadBalancer creates a new load balancer or updates the existing one.
func (lbaas *LbaasV2) EnsureLoadBalancer(ctx context.Context, clusterName string, apiService *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	serviceName := fmt.Sprintf("%s/%s", apiService.Namespace, apiService.Name)

	klog.V(4).Infof("EnsureLoadBalancer(%s, %s)", clusterName, serviceName)

	if len(nodes) == 0 {
		return nil, fmt.Errorf("there are no available nodes for LoadBalancer service %s", serviceName)
	}

	lbaas.opts.SubnetID = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerSubnetID, lbaas.opts.SubnetID)
	if len(lbaas.opts.SubnetID) == 0 {
		// Get SubnetID automatically.
		// The LB needs to be configured with instance addresses on the same subnet, so get SubnetID by one node.
		subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0])
		if err != nil {
			klog.Warningf("Failed to find subnet-id for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
			return nil, fmt.Errorf("no subnet-id for service %s/%s : subnet-id not set in cloud provider config, "+
				"and failed to find subnet-id from OpenStack: %v", apiService.Namespace, apiService.Name, err)
		}
		lbaas.opts.SubnetID = subnetID
	}

	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return nil, fmt.Errorf("no ports provided to openstack load balancer")
	}

	internalAnnotation := false
	var lbClass *LBClass
	var err error
	class := getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerClass, "")
	if class != "" {
		lbClass = lbaas.opts.LBClasses[class]
		if lbClass == nil {
			return nil, fmt.Errorf("invalid loadbalancer class %q", class)
		}
		// Only set the internalAnnotation to true when no FloatingNetwork information is provided
		if lbClass.FloatingNetworkID == "" && lbClass.FloatingSubnetID == "" {
			internalAnnotation = lbClass.SubnetID != ""
			if !internalAnnotation {
				return nil, fmt.Errorf("empty loadbalancer class configuration for class %q", class)
			}
		}
		klog.V(4).Infof("found loadbalancer class %q with %+v", class, lbClass)
	} else {
		internalAnnotation, err = getBoolFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerInternal, lbaas.opts.InternalLB)
		if err != nil {
			return nil, err
		}
	}

	floatingPool := getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerFloatingNetworkID, lbaas.opts.FloatingNetworkID)
	if lbClass != nil && lbClass.FloatingNetworkID != "" {
		floatingPool = lbClass.FloatingNetworkID
		klog.V(4).Infof("found floating network id %q from class %q", floatingPool, class)
	}
	if len(floatingPool) == 0 {
		var err error
		floatingPool, err = getFloatingNetworkIDForLB(lbaas.network)
		if err != nil {
			klog.Warningf("Failed to find floating-network-id for Service %s: %v", serviceName, err)
		}
	}

	switch internalAnnotation {
	case true:
		klog.V(4).Infof("Ensure an internal loadbalancer service.")
	case false:
		if len(floatingPool) != 0 {
			klog.V(4).Infof("Ensure an external loadbalancer service, using floatingPool: %v", floatingPool)
		} else {
			return nil, fmt.Errorf("floating-network-id or loadbalancer.openstack.org/floating-network-id should be specified when ensuring an external loadbalancer service")
		}
	}

	// Check for TCP protocol on each port
	for _, port := range ports {
		if port.Protocol != corev1.ProtocolTCP {
			return nil, fmt.Errorf("only TCP LoadBalancer is supported for openstack load balancers")
		}
	}

	var listenerAllowedCIDRs []string
	sourceRanges, err := v1service.GetLoadBalancerSourceRanges(apiService)
	if err != nil {
		return nil, fmt.Errorf("failed to get source ranges for loadbalancer service %s: %v", serviceName, err)
	}
	if lbaas.opts.UseOctavia && openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureVIPACL) {
		klog.V(4).Info("loadBalancerSourceRanges is suppported")
		listenerAllowedCIDRs = sourceRanges.StringSlice()
	} else if !v1service.IsAllowAll(sourceRanges) && !lbaas.opts.ManageSecurityGroups {
		return nil, fmt.Errorf("source range restrictions are not supported for openstack load balancers without managing security groups")
	}

	affinity := apiService.Spec.SessionAffinity
	var persistence *v2pools.SessionPersistence
	switch affinity {
	case corev1.ServiceAffinityNone:
		persistence = nil
	case corev1.ServiceAffinityClientIP:
		persistence = &v2pools.SessionPersistence{Type: "SOURCE_IP"}
	default:
		return nil, fmt.Errorf("unsupported load balancer affinity: %v", affinity)
	}

	// Use more meaningful name for the load balancer but still need to check the legacy name for backward compatibility.
	name := lbaas.GetLoadBalancerName(ctx, clusterName, apiService)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, apiService)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil {
		if err != ErrNotFound {
			return nil, fmt.Errorf("error getting loadbalancer for Service %s: %v", serviceName, err)
		}

		klog.V(2).Infof("Creating loadbalancer %s", name)

		portID := ""
		if lbClass == nil {
			portID = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerPortID, "")
		}
		loadbalancer, err = lbaas.createLoadBalancer(apiService, name, clusterName, lbClass, internalAnnotation, portID)
		if err != nil {
			return nil, fmt.Errorf("error creating loadbalancer %s: %v", name, err)
		}
	} else {
		klog.V(2).Infof("LoadBalancer %s already exists", loadbalancer.Name)
	}

	provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE, current provisioning status %s", provisioningStatus)
	}

	lbmethod := v2pools.LBMethod(lbaas.opts.LBMethod)
	if lbmethod == "" {
		lbmethod = v2pools.LBMethodRoundRobin
	}

	oldListeners, err := getListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting LB %s listeners: %v", loadbalancer.Name, err)
	}
	for portIndex, port := range ports {
		listener := getListenerForPort(oldListeners, port)
		climit := getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerConnLimit, "-1")
		connLimit := -1
		tmp, err := strconv.Atoi(climit)
		if err != nil {
			klog.V(4).Infof("Could not parse int value from \"%s\" error \"%v\" failing back to default", climit, err)
		} else {
			connLimit = tmp
		}

		keepClientIP := false
		if listener == nil {
			listenerProtocol := listeners.Protocol(port.Protocol)
			keepClientIP, err = getBoolFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerXForwardedFor, false)
			if err != nil {
				return nil, err
			}
			if keepClientIP {
				listenerProtocol = listeners.ProtocolHTTP
			}

			listenerCreateOpt := listeners.CreateOpts{
				Name:           cutString(fmt.Sprintf("listener_%d_%s", portIndex, name)),
				Protocol:       listenerProtocol,
				ProtocolPort:   int(port.Port),
				ConnLimit:      &connLimit,
				LoadbalancerID: loadbalancer.ID,
			}

			if lbaas.opts.UseOctavia {
				if timeoutClientData, ok := getIntFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerTimeoutClientData); ok {
					listenerCreateOpt.TimeoutClientData = &timeoutClientData
				}
				if timeoutMemberData, ok := getIntFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerTimeoutMemberData); ok {
					listenerCreateOpt.TimeoutMemberData = &timeoutMemberData
				}
				if timeoutMemberConnect, ok := getIntFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerTimeoutMemberConnect); ok {
					listenerCreateOpt.TimeoutMemberConnect = &timeoutMemberConnect
				}
				if timeoutTCPInspect, ok := getIntFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerTimeoutTCPInspect); ok {
					listenerCreateOpt.TimeoutTCPInspect = &timeoutTCPInspect
				}
				if keepClientIP {
					listenerCreateOpt.InsertHeaders = map[string]string{"X-Forwarded-For": "true"}
				}
				if len(listenerAllowedCIDRs) > 0 {
					listenerCreateOpt.AllowedCIDRs = listenerAllowedCIDRs
				}
			}

			klog.V(4).Infof("Creating listener for port %d using protocol: %s", int(port.Port), listenerProtocol)

			listener, err = openstackutil.CreateListener(lbaas.lb, loadbalancer.ID, listenerCreateOpt)
			if err != nil {
				return nil, fmt.Errorf("failed to create listener for loadbalancer %s: %v", loadbalancer.ID, err)
			}

			klog.V(4).Infof("Listener %s created for loadbalancer %s", listener.ID, loadbalancer.ID)
		} else {
			listenerChanged := false
			updateOpts := listeners.UpdateOpts{}

			if lbaas.opts.UseOctavia {
				if connLimit != listener.ConnLimit {
					updateOpts.ConnLimit = &connLimit
					listenerChanged = true
				}
				if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureVIPACL) {
					if !cpoutil.StringListEqual(listenerAllowedCIDRs, listener.AllowedCIDRs) {
						updateOpts.AllowedCIDRs = &listenerAllowedCIDRs
						listenerChanged = true
					}
				}
			}

			if listenerChanged {
				if err := openstackutil.UpdateListener(lbaas.lb, loadbalancer.ID, listener.ID, updateOpts); err != nil {
					return nil, fmt.Errorf("failed to update listener %s of loadbalancer %s: %v", listener.ID, loadbalancer.ID, err)
				}

				klog.V(4).Infof("Listener %s updated for loadbalancer %s", listener.ID, loadbalancer.ID)
			}
		}

		// After all ports have been processed, remaining listeners are removed as obsolete.
		// Pop valid listeners.
		oldListeners = popListener(oldListeners, listener.ID)

		pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != ErrNotFound {
			return nil, fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
		}
		if pool == nil {
			// By default, use TCP as the pool protocol.
			poolProto := v2pools.ProtocolTCP

			useProxyProtocol, err := getBoolFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerProxyEnabled, false)
			if err != nil {
				return nil, err
			}
			if useProxyProtocol && keepClientIP {
				return nil, fmt.Errorf("annotation %s and %s cannot be used together", ServiceAnnotationLoadBalancerProxyEnabled, ServiceAnnotationLoadBalancerXForwardedFor)
			}
			if useProxyProtocol {
				poolProto = v2pools.ProtocolPROXY
			} else if keepClientIP {
				poolProto = v2pools.ProtocolHTTP
			}

			createOpt := v2pools.CreateOpts{
				Name:        cutString(fmt.Sprintf("pool_%d_%s", portIndex, name)),
				Protocol:    poolProto,
				LBMethod:    lbmethod,
				ListenerID:  listener.ID,
				Persistence: persistence,
			}

			klog.V(4).Infof("Creating pool for listener %s using protocol %s", listener.ID, poolProto)

			pool, err = v2pools.Create(lbaas.lb, createOpt).Extract()
			if err != nil {
				return nil, fmt.Errorf("error creating pool for listener %s: %v", listener.ID, err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating pool, current provisioning status %s", provisioningStatus)
			}

		}

		klog.V(4).Infof("Pool created for listener %s: %s", listener.ID, pool.ID)

		members, err := getMembersByPoolID(lbaas.lb, pool.ID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting pool members %s: %v", pool.ID, err)
		}
		for _, node := range nodes {
			addr, err := nodeAddressForLB(node)
			if err != nil {
				if err == ErrNotFound {
					// Node failure, do not create member
					klog.Warningf("Failed to create LB pool member for node %s: %v", node.Name, err)
					continue
				} else {
					return nil, fmt.Errorf("error getting address for node %s: %v", node.Name, err)
				}
			}

			if !memberExists(members, addr, int(port.NodePort)) {
				klog.V(4).Infof("Creating member for pool %s", pool.ID)
				_, err := v2pools.CreateMember(lbaas.lb, pool.ID, v2pools.CreateMemberOpts{
					Name:         cutString(fmt.Sprintf("member_%d_%s_%s", portIndex, node.Name, name)),
					ProtocolPort: int(port.NodePort),
					Address:      addr,
					SubnetID:     lbaas.opts.SubnetID,
				}).Extract()
				if err != nil {
					return nil, fmt.Errorf("error creating LB pool member for node: %s, %v", node.Name, err)
				}

				provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
				if err != nil {
					return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating member, current provisioning status %s", provisioningStatus)
				}
			} else {
				// After all members have been processed, remaining members are deleted as obsolete.
				members = popMember(members, addr, int(port.NodePort))
			}

			klog.V(4).Infof("Ensured pool %s has member for %s at %s", pool.ID, node.Name, addr)
		}

		// Delete obsolete members for this pool
		for _, member := range members {
			klog.V(4).Infof("Deleting obsolete member %s for pool %s address %s", member.ID, pool.ID, member.Address)
			err := v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error deleting obsolete member %s for pool %s address %s: %v", member.ID, pool.ID, member.Address, err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
			}
		}

		monitorID := pool.MonitorID
		if monitorID == "" && lbaas.opts.CreateMonitor {
			klog.V(4).Infof("Creating monitor for pool %s", pool.ID)
			monitor, err := v2monitors.Create(lbaas.lb, v2monitors.CreateOpts{
				Name:       cutString(fmt.Sprintf("monitor_%d_%s)", portIndex, name)),
				PoolID:     pool.ID,
				Type:       string(port.Protocol),
				Delay:      int(lbaas.opts.MonitorDelay.Duration.Seconds()),
				Timeout:    int(lbaas.opts.MonitorTimeout.Duration.Seconds()),
				MaxRetries: int(lbaas.opts.MonitorMaxRetries),
			}).Extract()
			if err != nil {
				return nil, fmt.Errorf("error creating LB pool healthmonitor: %v", err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating monitor, current provisioning status %s", provisioningStatus)
			}
			monitorID = monitor.ID
		} else if lbaas.opts.CreateMonitor == false {
			klog.V(4).Infof("Do not create monitor for pool %s when create-monitor is false", pool.ID)
		}

		if monitorID != "" {
			klog.V(4).Infof("Monitor for pool %s: %s", pool.ID, monitorID)
		}
	}

	// All remaining listeners are obsolete, delete
	for _, listener := range oldListeners {
		klog.V(4).Infof("Deleting obsolete listener %s:", listener.ID)
		// get pool for listener
		pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != ErrNotFound {
			return nil, fmt.Errorf("error getting pool for obsolete listener %s: %v", listener.ID, err)
		}
		if pool != nil {
			// get and delete monitor
			monitorID := pool.MonitorID
			if monitorID != "" {
				klog.V(4).Infof("Deleting obsolete monitor %s for pool %s", monitorID, pool.ID)
				err = v2monitors.Delete(lbaas.lb, monitorID).ExtractErr()
				if err != nil && !cpoerrors.IsNotFound(err) {
					return nil, fmt.Errorf("error deleting obsolete monitor %s for pool %s: %v", monitorID, pool.ID, err)
				}
				provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
				if err != nil {
					return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting monitor, current provisioning status %s", provisioningStatus)
				}
			}
			// get and delete pool members
			members, err := getMembersByPoolID(lbaas.lb, pool.ID)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error getting members for pool %s: %v", pool.ID, err)
			}
			if members != nil {
				for _, member := range members {
					klog.V(4).Infof("Deleting obsolete member %s for pool %s address %s", member.ID, pool.ID, member.Address)
					err := v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
					if err != nil && !cpoerrors.IsNotFound(err) {
						return nil, fmt.Errorf("error deleting obsolete member %s for pool %s address %s: %v", member.ID, pool.ID, member.Address, err)
					}
					provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
					if err != nil {
						return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
					}
				}
			}
			klog.V(4).Infof("Deleting obsolete pool %s for listener %s", pool.ID, listener.ID)
			// delete pool
			err = v2pools.Delete(lbaas.lb, pool.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error deleting obsolete pool %s for listener %s: %v", pool.ID, listener.ID, err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting pool, current provisioning status %s", provisioningStatus)
			}
		}
		// delete listener
		err = listeners.Delete(lbaas.lb, listener.ID).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, fmt.Errorf("error deleteting obsolete listener: %v", err)
		}
		provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting listener, current provisioning status %s", provisioningStatus)
		}
		klog.V(2).Infof("Deleted obsolete listener: %s", listener.ID)
	}

	// Priority of choosing VIP port floating IP:
	// 1. The floating IP that is already attached to the VIP port.
	// 2. Floating IP specified in Spec.LoadBalancerIP
	// 3. Create a new one
	portID := loadbalancer.VipPortID
	floatIP, err := openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
	if err != nil {
		return nil, fmt.Errorf("failed when getting floating IP for port %s: %v", portID, err)
	}

	if floatIP == nil && floatingPool != "" && !internalAnnotation {
		loadBalancerIP := apiService.Spec.LoadBalancerIP
		needCreate := true

		if loadBalancerIP != "" {
			opts := floatingips.ListOpts{
				FloatingIP: loadBalancerIP,
			}
			existingIPs, err := openstackutil.GetFloatingIPs(lbaas.network, opts)
			if err != nil {
				return nil, fmt.Errorf("failed when trying to get existing flaoting IP %s, error: %v", loadBalancerIP, err)
			}

			if len(existingIPs) == 0 {
				klog.Infof("Could not find the given floating IP %s, will create a new one.", loadBalancerIP)
			} else {
				floatingip := existingIPs[0]
				if len(floatingip.PortID) == 0 {
					floatUpdateOpts := floatingips.UpdateOpts{
						PortID: &portID,
					}
					floatIP, err = floatingips.Update(lbaas.network, floatingip.ID, floatUpdateOpts).Extract()
					if err != nil {
						return nil, fmt.Errorf("error updating LB floatingip %+v: %v", floatUpdateOpts, err)
					}
					needCreate = false
				} else {
					return nil, fmt.Errorf("floating IP %s is not available", loadBalancerIP)
				}
			}
		}

		if needCreate {
			klog.V(4).Infof("Creating floating IP for loadbalancer %s", loadbalancer.ID)
			floatIPOpts := floatingips.CreateOpts{
				FloatingNetworkID: floatingPool,
				PortID:            portID,
				Description:       fmt.Sprintf("Floating IP for Kubernetes external service %s from cluster %s", serviceName, clusterName),
			}

			if lbClass != nil {
				floatIPOpts.SubnetID = lbClass.FloatingSubnetID
			} else {
				// Defaulting floating subnet id if no lbClass is defined
				floatIPOpts.SubnetID = lbaas.opts.FloatingSubnetID
				// if ID is used, lets use that. Otherwise fail to name and query its ID
				floatingSubnet := getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerFloatingSubnetID, "")
				if floatingSubnet != "" {
					floatIPOpts.SubnetID = floatingSubnet
				} else {
					floatingSubnet = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerFloatingSubnet, "")
					if floatingSubnet != "" {
						lbSubnet, err := lbaas.getSubnet(floatingSubnet)
						if err != nil {
							return nil, fmt.Errorf("failed to find floatingip subnet: %v", err)
						}
						if lbSubnet != nil {
							floatIPOpts.SubnetID = lbSubnet.ID
						}
					}
				}
			}

			if loadBalancerIP != "" {
				klog.V(4).Infof("creating a new floating ip %s", loadBalancerIP)
				floatIPOpts.FloatingIP = loadBalancerIP
			}

			klog.V(4).Infof("creating floating ip with opts %+v", floatIPOpts)

			floatIP, err = floatingips.Create(lbaas.network, floatIPOpts).Extract()
			if err != nil {
				return nil, fmt.Errorf("error creating LB floatingip %+v: %v", floatIPOpts, err)
			}
		}
	}

	status := &corev1.LoadBalancerStatus{}

	if floatIP != nil {
		status.Ingress = []corev1.LoadBalancerIngress{{IP: floatIP.FloatingIP}}
	} else {
		status.Ingress = []corev1.LoadBalancerIngress{{IP: loadbalancer.VipAddress}}
	}

	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.ensureSecurityGroup(clusterName, apiService, nodes, loadbalancer)
		if err != nil {
			return status, fmt.Errorf("failed when reconciling security groups for LB service %v/%v: %v", apiService.Namespace, apiService.Name, err)
		}
	}

	return status, nil
}

func (lbaas *LbaasV2) getSubnet(subnet string) (*subnets.Subnet, error) {
	if subnet == "" {
		return nil, nil
	}

	allPages, err := subnets.List(lbaas.network, subnets.ListOpts{Name: subnet}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("error listing subnets: %v", err)
	}
	subs, err := subnets.ExtractSubnets(allPages)
	if err != nil {
		return nil, fmt.Errorf("error extracting subnets from pages: %v", err)
	}

	if len(subs) == 0 {
		return nil, fmt.Errorf("could not find subnet %s", subnet)
	}
	if len(subs) == 1 {
		return &subs[0], nil
	}
	return nil, fmt.Errorf("find multiple subnets with name %s", subnet)
}

// ensureSecurityGroup ensures security group exist for specific loadbalancer service.
// Creating security group for specific loadbalancer service when it does not exist.
func (lbaas *LbaasV2) ensureSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node, loadbalancer *loadbalancers.LoadBalancer) error {
	// find node-security-group for service
	var err error
	if len(lbaas.opts.NodeSecurityGroupIDs) == 0 && !lbaas.opts.UseOctavia {
		lbaas.opts.NodeSecurityGroupIDs, err = getNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes)
		if err != nil {
			return fmt.Errorf("failed to find node-security-group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
		}

		klog.V(4).Infof("find node-security-group %v for loadbalancer service %s/%s", lbaas.opts.NodeSecurityGroupIDs, apiService.Namespace, apiService.Name)
	}

	// get service ports
	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	// get service source ranges
	sourceRanges, err := v1service.GetLoadBalancerSourceRanges(apiService)
	if err != nil {
		return fmt.Errorf("failed to get source ranges for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
	}

	// ensure security group for LB
	lbSecGroupName := getSecurityGroupName(apiService)
	lbSecGroupID, err := groups.IDFromName(lbaas.network, lbSecGroupName)
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

		lbSecGroup, err := groups.Create(lbaas.network, lbSecGroupCreateOpts).Extract()
		if err != nil {
			return fmt.Errorf("failed to create Security Group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
		}
		lbSecGroupID = lbSecGroup.ID

		if !lbaas.opts.UseOctavia {
			//add rule in security group
			for _, port := range ports {
				for _, sourceRange := range sourceRanges.StringSlice() {
					ethertype := rules.EtherType4
					network, _, err := net.ParseCIDR(sourceRange)

					if err != nil {
						return fmt.Errorf("error parsing source range %s as a CIDR: %v", sourceRange, err)
					}

					if network.To4() == nil {
						ethertype = rules.EtherType6
					}

					lbSecGroupRuleCreateOpts := rules.CreateOpts{
						Direction:      rules.DirIngress,
						PortRangeMax:   int(port.Port),
						PortRangeMin:   int(port.Port),
						Protocol:       toRuleProtocol(port.Protocol),
						RemoteIPPrefix: sourceRange,
						SecGroupID:     lbSecGroup.ID,
						EtherType:      ethertype,
					}

					_, err = rules.Create(lbaas.network, lbSecGroupRuleCreateOpts).Extract()

					if err != nil {
						return fmt.Errorf("error occurred creating rule for SecGroup %s: %v", lbSecGroup.ID, err)
					}
				}
			}

			lbSecGroupRuleCreateOpts := rules.CreateOpts{
				Direction:      rules.DirIngress,
				PortRangeMax:   4, // ICMP: Code -  Values for ICMP  "Destination Unreachable: Fragmentation Needed and Don't Fragment was Set"
				PortRangeMin:   3, // ICMP: Type
				Protocol:       rules.ProtocolICMP,
				RemoteIPPrefix: "0.0.0.0/0", // The Fragmentation packet can come from anywhere along the path back to the sourceRange - we need to all this from all
				SecGroupID:     lbSecGroup.ID,
				EtherType:      rules.EtherType4,
			}

			_, err = rules.Create(lbaas.network, lbSecGroupRuleCreateOpts).Extract()

			if err != nil {
				return fmt.Errorf("error occurred creating rule for SecGroup %s: %v", lbSecGroup.ID, err)
			}

			lbSecGroupRuleCreateOpts = rules.CreateOpts{
				Direction:      rules.DirIngress,
				PortRangeMax:   0, // ICMP: Code - Values for ICMP "Packet Too Big"
				PortRangeMin:   2, // ICMP: Type
				Protocol:       rules.ProtocolICMP,
				RemoteIPPrefix: "::/0", // The Fragmentation packet can come from anywhere along the path back to the sourceRange - we need to all this from all
				SecGroupID:     lbSecGroup.ID,
				EtherType:      rules.EtherType6,
			}

			_, err = rules.Create(lbaas.network, lbSecGroupRuleCreateOpts).Extract()
			if err != nil {
				return fmt.Errorf("error occurred creating rule for SecGroup %s: %v", lbSecGroup.ID, err)
			}

			// get security groups of port
			portID := loadbalancer.VipPortID
			port, err := getPortByID(lbaas.network, portID)
			if err != nil {
				return err
			}

			// ensure the vip port has the security groups
			found := false
			for _, portSecurityGroups := range port.SecurityGroups {
				if portSecurityGroups == lbSecGroup.ID {
					found = true
					break
				}
			}

			// update loadbalancer vip port
			if !found {
				port.SecurityGroups = append(port.SecurityGroups, lbSecGroup.ID)
				updateOpts := neutronports.UpdateOpts{SecurityGroups: &port.SecurityGroups}
				res := neutronports.Update(lbaas.network, portID, updateOpts)
				if res.Err != nil {
					msg := fmt.Sprintf("Error occurred updating port %s for loadbalancer service %s/%s: %v", portID, apiService.Namespace, apiService.Name, res.Err)
					return fmt.Errorf(msg)
				}
			}
		}
	}

	// ensure rules for node security group
	for _, port := range ports {
		// If Octavia is used, the VIP port security group is already taken good care of, we only need to allow ingress
		// traffic from Octavia amphorae to the node port on the worker nodes.
		if lbaas.opts.UseOctavia {
			subnet, err := subnets.Get(lbaas.network, lbaas.opts.SubnetID).Extract()
			if err != nil {
				return fmt.Errorf("failed to find subnet %s from openstack: %v", lbaas.opts.SubnetID, err)
			}

			sgListopts := rules.ListOpts{
				Direction:      string(rules.DirIngress),
				Protocol:       string(port.Protocol),
				PortRangeMax:   int(port.NodePort),
				PortRangeMin:   int(port.NodePort),
				RemoteIPPrefix: subnet.CIDR,
				SecGroupID:     lbSecGroupID,
			}
			sgRules, err := getSecurityGroupRules(lbaas.network, sgListopts)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return fmt.Errorf("failed to find security group rules in %s: %v", lbSecGroupID, err)
			}
			if len(sgRules) != 0 {
				continue
			}

			// The Octavia amphorae and worker nodes are supposed to be in the same subnet. We allow the ingress traffic
			// from the amphorae to the specific node port on the nodes.
			sgRuleCreateOpts := rules.CreateOpts{
				Direction:      rules.DirIngress,
				PortRangeMax:   int(port.NodePort),
				PortRangeMin:   int(port.NodePort),
				Protocol:       toRuleProtocol(port.Protocol),
				RemoteIPPrefix: subnet.CIDR,
				SecGroupID:     lbSecGroupID,
				EtherType:      rules.EtherType4,
			}
			if _, err = rules.Create(lbaas.network, sgRuleCreateOpts).Extract(); err != nil {
				return fmt.Errorf("failed to create rule for security group %s: %v", lbSecGroupID, err)
			}

			if err := applyNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes, lbSecGroupID); err != nil {
				return err
			}
		} else {
			for _, nodeSecurityGroupID := range lbaas.opts.NodeSecurityGroupIDs {
				opts := rules.ListOpts{
					Direction:     string(rules.DirIngress),
					SecGroupID:    nodeSecurityGroupID,
					RemoteGroupID: lbSecGroupID,
					PortRangeMax:  int(port.NodePort),
					PortRangeMin:  int(port.NodePort),
					Protocol:      string(port.Protocol),
				}
				secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)
				if err != nil && !cpoerrors.IsNotFound(err) {
					msg := fmt.Sprintf("Error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, nodeSecurityGroupID, err)
					return fmt.Errorf(msg)
				}
				if len(secGroupRules) != 0 {
					// Do not add rule when find rules for remote group in the Node Security Group
					continue
				}

				// Add the rules in the Node Security Group
				err = createNodeSecurityGroup(lbaas.network, nodeSecurityGroupID, int(port.NodePort), port.Protocol, lbSecGroupID)
				if err != nil {
					return fmt.Errorf("error occurred creating security group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
				}
			}
		}
	}

	return nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
func (lbaas *LbaasV2) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	klog.V(4).Infof("UpdateLoadBalancer(%v, %s, %v)", clusterName, serviceName, nodes)

	lbaas.opts.SubnetID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerSubnetID, lbaas.opts.SubnetID)
	if len(lbaas.opts.SubnetID) == 0 && len(nodes) > 0 {
		// Get SubnetID automatically.
		// The LB needs to be configured with instance addresses on the same subnet, so get SubnetID by one node.
		subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0])
		if err != nil {
			klog.Warningf("Failed to find subnet-id for loadbalancer service %s/%s: %v", service.Namespace, service.Name, err)
			return fmt.Errorf("no subnet-id for service %s/%s : subnet-id not set in cloud provider config, "+
				"and failed to find subnet-id from OpenStack: %v", service.Namespace, service.Name, err)
		}
		lbaas.opts.SubnetID = subnetID
	}

	ports := service.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, service)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil {
		return err
	}
	if loadbalancer == nil {
		return fmt.Errorf("loadbalancer does not exist for Service %s", serviceName)
	}

	// Get all listeners for this loadbalancer, by "port key".
	type portKey struct {
		Protocol listeners.Protocol
		Port     int
	}
	var listenerIDs []string
	lbListeners := make(map[portKey]listeners.Listener)
	allListeners, err := getListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return fmt.Errorf("error getting listeners for LB %s: %v", loadbalancer.ID, err)
	}
	for _, l := range allListeners {
		key := portKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
		lbListeners[key] = l
		listenerIDs = append(listenerIDs, l.ID)
	}

	// Get all pools for this loadbalancer, by listener ID.
	lbPools := make(map[string]v2pools.Pool)
	for _, listenerID := range listenerIDs {
		pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listenerID)
		if err != nil {
			return fmt.Errorf("error getting pool for listener %s: %v", listenerID, err)
		}
		lbPools[listenerID] = *pool
	}

	// Compose Set of member (addresses) that _should_ exist
	addrs := make(map[string]*corev1.Node)
	for _, node := range nodes {
		addr, err := nodeAddressForLB(node)
		if err != nil {
			return err
		}
		addrs[addr] = node
	}

	// Check for adding/removing members associated with each port
	for portIndex, port := range ports {
		// Get listener associated with this port
		listener, ok := lbListeners[portKey{
			Protocol: toListenersProtocol(port.Protocol),
			Port:     int(port.Port),
		}]
		if !ok {
			return fmt.Errorf("loadbalancer %s does not contain required listener for port %d and protocol %s", loadbalancer.ID, port.Port, port.Protocol)
		}

		// Get pool associated with this listener
		pool, ok := lbPools[listener.ID]
		if !ok {
			return fmt.Errorf("loadbalancer %s does not contain required pool for listener %s", loadbalancer.ID, listener.ID)
		}

		// Find existing pool members (by address) for this port
		getMembers, err := getMembersByPoolID(lbaas.lb, pool.ID)
		if err != nil {
			return fmt.Errorf("error getting pool members %s: %v", pool.ID, err)
		}
		members := make(map[string]v2pools.Member)
		for _, member := range getMembers {
			members[member.Address] = member
		}

		// Add any new members for this port
		for addr, node := range addrs {
			if _, ok := members[addr]; ok && members[addr].ProtocolPort == int(port.NodePort) {
				// Already exists, do not create member
				continue
			}
			_, err := v2pools.CreateMember(lbaas.lb, pool.ID, v2pools.CreateMemberOpts{
				Name:         cutString(fmt.Sprintf("member_%d_%s_%s_", portIndex, node.Name, loadbalancer.Name)),
				Address:      addr,
				ProtocolPort: int(port.NodePort),
				SubnetID:     lbaas.opts.SubnetID,
			}).Extract()
			if err != nil {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating member, current provisioning status %s", provisioningStatus)
			}
		}

		// Remove any old members for this port
		for _, member := range members {
			if _, ok := addrs[member.Address]; ok && member.ProtocolPort == int(port.NodePort) {
				// Still present, do not delete member
				continue
			}
			err = v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
			}
		}
	}

	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.updateSecurityGroup(clusterName, service, nodes, loadbalancer)
		if err != nil {
			return fmt.Errorf("failed to update Security Group for loadbalancer service %s: %v", serviceName, err)
		}
	}

	return nil
}

// updateSecurityGroup updating security group for specific loadbalancer service.
func (lbaas *LbaasV2) updateSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node, loadbalancer *loadbalancers.LoadBalancer) error {
	originalNodeSecurityGroupIDs := lbaas.opts.NodeSecurityGroupIDs

	var err error
	lbaas.opts.NodeSecurityGroupIDs, err = getNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes)
	if err != nil {
		return fmt.Errorf("failed to find node-security-group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
	}
	klog.V(4).Infof("find node-security-group %v for loadbalancer service %s/%s", lbaas.opts.NodeSecurityGroupIDs, apiService.Namespace, apiService.Name)

	original := sets.NewString(originalNodeSecurityGroupIDs...)
	current := sets.NewString(lbaas.opts.NodeSecurityGroupIDs...)
	removals := original.Difference(current)

	// Generate Name
	lbSecGroupName := getSecurityGroupName(apiService)
	lbSecGroupID, err := groups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
	}

	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	for _, port := range ports {
		for removal := range removals {
			// Delete the rules in the Node Security Group
			opts := rules.ListOpts{
				Direction:     string(rules.DirIngress),
				SecGroupID:    removal,
				RemoteGroupID: lbSecGroupID,
				PortRangeMax:  int(port.NodePort),
				PortRangeMin:  int(port.NodePort),
				Protocol:      string(port.Protocol),
			}
			secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return fmt.Errorf("error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, removal, err)
			}

			for _, rule := range secGroupRules {
				res := rules.Delete(lbaas.network, rule.ID)
				if res.Err != nil && !cpoerrors.IsNotFound(res.Err) {
					return fmt.Errorf("error occurred deleting security group rule: %s: %v", rule.ID, res.Err)
				}
			}
		}

		for _, nodeSecurityGroupID := range lbaas.opts.NodeSecurityGroupIDs {
			opts := rules.ListOpts{
				Direction:     string(rules.DirIngress),
				SecGroupID:    nodeSecurityGroupID,
				RemoteGroupID: lbSecGroupID,
				PortRangeMax:  int(port.NodePort),
				PortRangeMin:  int(port.NodePort),
				Protocol:      string(port.Protocol),
			}
			secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return fmt.Errorf("error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, nodeSecurityGroupID, err)
			}
			if len(secGroupRules) != 0 {
				// Do not add rule when find rules for remote group in the Node Security Group
				continue
			}

			// Add the rules in the Node Security Group
			err = createNodeSecurityGroup(lbaas.network, nodeSecurityGroupID, int(port.NodePort), port.Protocol, lbSecGroupID)
			if err != nil {
				return fmt.Errorf("error occurred creating security group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
			}
		}
	}

	return nil
}

// EnsureLoadBalancerDeleted deletes the specified load balancer
func (lbaas *LbaasV2) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	klog.V(4).Infof("EnsureLoadBalancerDeleted(%s, %s)", clusterName, serviceName)

	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, service)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil && err != ErrNotFound {
		return err
	}
	if loadbalancer == nil {
		return nil
	}

	keepFloatingAnnotation, err := getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerKeepFloatingIP, false)
	if err != nil {
		return err
	}

	if !keepFloatingAnnotation {
		if loadbalancer.VipPortID != "" {
			portID := loadbalancer.VipPortID
			fip, err := openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
			if err != nil {
				return fmt.Errorf("failed to get floating IP for loadbalancer VIP port %s: %v", portID, err)
			}
			if fip != nil {
				if err := floatingips.Delete(lbaas.network, fip.ID).ExtractErr(); err != nil {
					return fmt.Errorf("failed to delete floating IP %s for loadbalancer VIP port %s: %v", fip.FloatingIP, portID, err)
				}
			}
		}
	}

	// delete the loadbalancer and all its sub-resources.
	if lbaas.opts.UseOctavia {
		deleteOpts := loadbalancers.DeleteOpts{Cascade: true}
		if err := loadbalancers.Delete(lbaas.lb, loadbalancer.ID, deleteOpts).ExtractErr(); err != nil {
			return fmt.Errorf("failed to delete loadbalancer %s: %v", loadbalancer.ID, err)
		}
	} else {
		// get all listeners associated with this loadbalancer
		listenerList, err := getListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return fmt.Errorf("error getting LB %s listeners: %v", loadbalancer.ID, err)
		}

		// get all pools (and health monitors) associated with this loadbalancer
		var poolIDs []string
		var monitorIDs []string
		for _, listener := range listenerList {
			pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listener.ID)
			if err != nil && err != ErrNotFound {
				return fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
			}
			if pool != nil {
				poolIDs = append(poolIDs, pool.ID)
				// If create-monitor of cloud-config is false, pool has not monitor.
				if pool.MonitorID != "" {
					monitorIDs = append(monitorIDs, pool.MonitorID)
				}
			}
		}

		// delete all monitors
		for _, monitorID := range monitorIDs {
			err := v2monitors.Delete(lbaas.lb, monitorID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting monitor, current provisioning status %s", provisioningStatus)
			}
		}

		// delete all members and pools
		for _, poolID := range poolIDs {
			// get members for current pool
			membersList, err := getMembersByPoolID(lbaas.lb, poolID)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return fmt.Errorf("error getting pool members %s: %v", poolID, err)
			}
			// delete all members for this pool
			for _, member := range membersList {
				err := v2pools.DeleteMember(lbaas.lb, poolID, member.ID).ExtractErr()
				if err != nil && !cpoerrors.IsNotFound(err) {
					return err
				}
				provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
				if err != nil {
					return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
				}
			}

			// delete pool
			err = v2pools.Delete(lbaas.lb, poolID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting pool, current provisioning status %s", provisioningStatus)
			}
		}

		// delete all listeners
		for _, listener := range listenerList {
			err := listeners.Delete(lbaas.lb, listener.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting listener, current provisioning status %s", provisioningStatus)
			}
		}

		// delete loadbalancer
		err = loadbalancers.Delete(lbaas.lb, loadbalancer.ID, loadbalancers.DeleteOpts{}).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return err
		}
		err = waitLoadbalancerDeleted(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return fmt.Errorf("failed to delete loadbalancer: %v", err)
		}
	}

	// Delete the Security Group
	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.EnsureSecurityGroupDeleted(clusterName, service)
		if err != nil {
			return fmt.Errorf("failed to delete Security Group for loadbalancer service %s: %v", serviceName, err)
		}
	}

	return nil
}

// EnsureSecurityGroupDeleted deleting security group for specific loadbalancer service.
func (lbaas *LbaasV2) EnsureSecurityGroupDeleted(clusterName string, service *corev1.Service) error {
	// Generate Name
	lbSecGroupName := getSecurityGroupName(service)
	lbSecGroupID, err := groups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		if isSecurityGroupNotFound(err) {
			// It is OK when the security group has been deleted by others.
			return nil
		}
		return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
	}

	if lbaas.opts.UseOctavia {
		// Disassociate the security group from the neutron ports on the nodes.
		if err := disassociateSecurityGroupForLB(lbaas.network, lbSecGroupID); err != nil {
			return fmt.Errorf("failed to disassociate security group %s: %v", lbSecGroupID, err)
		}
	}

	lbSecGroup := groups.Delete(lbaas.network, lbSecGroupID)
	if lbSecGroup.Err != nil && !cpoerrors.IsNotFound(lbSecGroup.Err) {
		return lbSecGroup.Err
	}

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
			secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)

			if err != nil && !cpoerrors.IsNotFound(err) {
				msg := fmt.Sprintf("error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, nodeSecurityGroupID, err)
				return fmt.Errorf(msg)
			}

			for _, rule := range secGroupRules {
				res := rules.Delete(lbaas.network, rule.ID)
				if res.Err != nil && !cpoerrors.IsNotFound(res.Err) {
					return fmt.Errorf("error occurred deleting security group rule: %s: %v", rule.ID, res.Err)
				}
			}
		}
	}

	return nil
}
