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
	"net/netip"
	"slices"
	"sync"

	v1 "k8s.io/api/core/v1"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/extraroutes"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	secgroups "github.com/gophercloud/utils/v2/openstack/networking/v2/extensions/security/groups"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

// Routes implements the cloudprovider.Routes for OpenStack clouds
type Routes struct {
	network *gophercloud.ServiceClient
	os      *OpenStack
	// The ID of the node security group
	nodeSecurityGroupId string
	// router's private network IDs
	networkIDs []string
	// whether Neutron supports "extraroute-atomic" extension
	atomicRoutes bool
	// whether Neutron supports "allowed-address-pairs" extension
	allowedAddressPairs bool
	// The auto config node security group feature whether enabled
	autoConfigSecurityGroup bool
	// Neutron with no "extraroute-atomic" extension can modify only one route at
	// once
	sync.Mutex
}

var _ cloudprovider.Routes = &Routes{}

// NewRoutes creates a new instance of Routes
func NewRoutes(os *OpenStack, network *gophercloud.ServiceClient, atomicRoutes bool, allowedAddressPairs bool) (cloudprovider.Routes, error) {
	if os.routeOpts.RouterID == "" {
		return nil, errors.ErrNoRouterID
	}

	return &Routes{
		network:                 network,
		os:                      os,
		atomicRoutes:            atomicRoutes,
		allowedAddressPairs:     allowedAddressPairs,
		autoConfigSecurityGroup: os.routeOpts.AutoConfigSecurityGroup,
	}, nil
}

func (r *Routes) getOrCreateNodeSecurityGroup(ctx context.Context, clusterName string) error {
	r.Lock()
	defer r.Unlock()
	sgName := fmt.Sprintf("k8s-node-sg-for-cluster-%s", clusterName)
	sgId, err := secgroups.IDFromName(ctx, r.network, sgName)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		mc := metrics.NewMetricContext("security_group", "create")
		klog.V(2).InfoS("Create node security group", "name", sgName)
		group, err := groups.Create(ctx, r.network, groups.CreateOpts{Name: sgName}).Extract()
		if mc.ObserveRequest(err) != nil {
			return err
		}
		sgId = group.ID

	}
	r.nodeSecurityGroupId = sgId
	return nil
}

// checkSecurityGroupRulesExist check whether the ingress security group rules that related to the podCidr and the node (nodeName) are existing,
// these security group rules use to ensure other nodes permit the traffic from this node (nodeName) or this node's pods pass through.
func (r *Routes) checkSecurityGroupRulesExist(podCidr string, nodeName types.NodeName, existingRules []rules.SecGroupRule) (bool, bool, error) {
	nodes, err := r.os.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		return false, false, err
	}
	sgRuleForPodCidrFound := false
	sgRuleForNodeAddressFound := false
	podPrefix, _ := netip.ParsePrefix(podCidr)
	nodeAddr := getAddrByNodeName(nodeName, podPrefix.Addr().Is6(), nodes)
	nodeIp, _ := netip.ParseAddr(nodeAddr)

	for _, rule := range existingRules {
		sgPrefix, _ := netip.ParsePrefix(rule.RemoteIPPrefix)
		if sgPrefix.Overlaps(podPrefix) && podPrefix.Bits() >= sgPrefix.Bits() {
			sgRuleForPodCidrFound = true
		}
		if rule.RemoteGroupID == r.nodeSecurityGroupId || sgPrefix.Contains(nodeIp) {
			sgRuleForNodeAddressFound = true
		}
		if sgRuleForPodCidrFound && sgRuleForNodeAddressFound {
			break
		}
	}
	return sgRuleForPodCidrFound, sgRuleForNodeAddressFound, nil
}

// checkPortSecurityRules check the port's security rules (SecurityGroups and AllowAddressPairs) whether are valid
func (r *Routes) checkPortSecurityRules(port *PortWithPortSecurity, route routers.Route) bool {

	// check if the node security group bind to the port
	if r.autoConfigSecurityGroup && !slices.Contains(port.SecurityGroups, r.nodeSecurityGroupId) {
		return false
	}

	if r.allowedAddressPairs {
		// check whether the related AllowAddressPair is existing
		for _, addrPair := range port.AllowedAddressPairs {
			if addrPair.IPAddress == route.DestinationCIDR {
				return true
			}
		}
		return false
	}
	return true
}

// ListRoutes lists all managed routes that belong to the specified clusterName
func (r *Routes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	klog.V(4).Infof("ListRoutes(%v)", clusterName)

	if r.os.nodeInformerHasSynced == nil || !r.os.nodeInformerHasSynced() {
		return nil, errors.ErrNoNodeInformer
	}

	nodes, err := r.os.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		return nil, err
	}

	mc := metrics.NewMetricContext("router", "get")
	router, err := routers.Get(ctx, r.network, r.os.routeOpts.RouterID).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	routes := make([]*cloudprovider.Route, 0, len(router.Routes))

	// detect router's private network ID for further VM ports filtering
	r.networkIDs, err = getRouterNetworkIDs(ctx, r.network, r.os.routeOpts.RouterID)
	if err != nil {
		return nil, err
	}

	var existingRules []rules.SecGroupRule
	if r.autoConfigSecurityGroup {
		if r.nodeSecurityGroupId == "" {
			klog.V(5).Info("ListRoutes: Cannot found node security group")
			return routes, nil
		}
		existingRules, err = openstackutil.GetSecurityGroupRules(r.network, rules.ListOpts{SecGroupID: r.nodeSecurityGroupId, Direction: string(rules.DirIngress)})
		if err != nil {
			return nil, err
		}
	}

	for _, item := range router.Routes {
		nodeName, foundNode := getNodeNameByAddr(item.NextHop, nodes)
		// If the node found, check whether the necessary rules (SecurityGroup and AllowAddressPair) was set. If
		// not, the connectivity of different nodes can't be ensure. CreateRoutes need be to trigger to set these
		// rules.
		if foundNode {
			if r.autoConfigSecurityGroup {
				podCidrRuleExist, nodeAddrRuleExit, err := r.checkSecurityGroupRulesExist(item.DestinationCIDR, nodeName, existingRules)
				if err != nil {
					return nil, err
				}
				if !(podCidrRuleExist && nodeAddrRuleExit) {
					continue
				}
			}
			// get the node port that the route next hop addr belong to
			port, err := r.getPortByIP(ctx, item.NextHop)
			if err != nil {
				return nil, err
			}
			if port.PortSecurityEnabled && !r.checkPortSecurityRules(port, item) {
				continue
			}
		}
		route := cloudprovider.Route{
			Name:            item.DestinationCIDR,
			TargetNode:      nodeName, //contains the nexthop address if node name was not found
			Blackhole:       !foundNode,
			DestinationCIDR: item.DestinationCIDR,
		}
		routes = append(routes, &route)
	}

	// detect router's private network ID for further VM ports filtering
	r.networkIDs, err = getRouterNetworkIDs(ctx, r.network, r.os.routeOpts.RouterID)
	if err != nil {
		return nil, err
	}

	return routes, nil
}

func getRouterNetworkIDs(ctx context.Context, network *gophercloud.ServiceClient, routerID string) ([]string, error) {
	opts := ports.ListOpts{
		DeviceID: routerID,
	}
	mc := metrics.NewMetricContext("port", "list")
	pages, err := ports.List(network, opts).AllPages(ctx)
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	ports, err := ports.ExtractPorts(pages)
	if err != nil {
		return nil, err
	}

	var networkIDs []string
	for _, port := range ports {
		if port.NetworkID != "" {
			networkIDs = append(networkIDs, port.NetworkID)
		}
	}

	return networkIDs, nil
}

func getNodeNameByAddr(addr string, nodes []*v1.Node) (types.NodeName, bool) {
	for _, node := range nodes {
		for _, v := range node.Status.Addresses {
			if v.Address == addr {
				return types.NodeName(node.Name), true
			}
		}
	}

	klog.V(4).Infof("Unable to resolve node name by %s IP", addr)
	return types.NodeName(addr), false
}

func getAddrByNodeName(name types.NodeName, needIPv6 bool, nodes []*v1.Node) string {
	for _, node := range nodes {
		if node.Name == string(name) {
			for _, v := range node.Status.Addresses {
				if v.Type == v1.NodeInternalIP {
					ip := net.ParseIP(v.Address)
					if ip == nil {
						continue
					}
					isIPv6 := ip.To4() == nil
					if needIPv6 {
						if isIPv6 {
							return v.Address
						}
						continue
					}
					if !isIPv6 {
						return v.Address
					}
				}
			}
		}
	}

	klog.V(4).Infof("Unable to resolve IP by %s node name", name)
	return ""
}

func updateRoutes(ctx context.Context, network *gophercloud.ServiceClient, router *routers.Router, newRoutes []routers.Route) (func(), error) {
	origRoutes := router.Routes // shallow copy

	mc := metrics.NewMetricContext("router", "update")
	_, err := routers.Update(ctx, network, router.ID, routers.UpdateOpts{
		Routes: &newRoutes,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting routes change to router %v", router.ID)
		mc := metrics.NewMetricContext("router", "update")
		_, err := routers.Update(ctx, network, router.ID, routers.UpdateOpts{
			Routes: &origRoutes,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset routes during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

func addRoute(ctx context.Context, network *gophercloud.ServiceClient, routerID string, newRoute []routers.Route) (func(), error) {
	mc := metrics.NewMetricContext("router", "update")
	_, err := extraroutes.Add(ctx, network, routerID, extraroutes.Opts{
		Routes: &newRoute,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting routes change to router %v", routerID)
		mc := metrics.NewMetricContext("router", "update")
		_, err := extraroutes.Remove(ctx, network, routerID, extraroutes.Opts{
			Routes: &newRoute,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset routes during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

func removeRoute(ctx context.Context, network *gophercloud.ServiceClient, routerID string, oldRoute []routers.Route) (func(), error) {
	mc := metrics.NewMetricContext("router", "update")
	_, err := extraroutes.Remove(ctx, network, routerID, extraroutes.Opts{
		Routes: &oldRoute,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting routes change to router %v", routerID)
		mc := metrics.NewMetricContext("router", "update")
		_, err := extraroutes.Add(ctx, network, routerID, extraroutes.Opts{
			Routes: &oldRoute,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset routes during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

func updateAllowedAddressPairs(ctx context.Context, network *gophercloud.ServiceClient, port *PortWithPortSecurity, newPairs []ports.AddressPair) (func(), error) {
	origPairs := port.AllowedAddressPairs // shallow copy

	mc := metrics.NewMetricContext("port", "update")
	_, err := ports.Update(ctx, network, port.ID, ports.UpdateOpts{
		AllowedAddressPairs: &newPairs,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting allowed-address-pairs change to port %v", port.ID)
		mc := metrics.NewMetricContext("port", "update")
		_, err := ports.Update(ctx, network, port.ID, ports.UpdateOpts{
			AllowedAddressPairs: &origPairs,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset allowed-address-pairs during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

// updateSecurityGroup update the port's security groups
func updateSecurityGroup(ctx context.Context, network *gophercloud.ServiceClient, port *PortWithPortSecurity, sgs []string) (func(), error) {
	origSgs := port.SecurityGroups
	mc := metrics.NewMetricContext("port", "update")
	_, err := ports.Update(ctx, network, port.ID, ports.UpdateOpts{
		SecurityGroups: &sgs,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting security-groups change to port %v", port.ID)
		mc := metrics.NewMetricContext("port", "update")
		_, err := ports.Update(ctx, network, port.ID, ports.UpdateOpts{
			SecurityGroups: &origSgs,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset port's security-groups during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

func createSecurityGroupRule(ctx context.Context, network *gophercloud.ServiceClient, rule rules.CreateOpts) (func(), error) {
	mc := metrics.NewMetricContext("security_group_rule", "create")
	newRule, err := rules.Create(ctx, network, rule).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	unwinder := func() {
		klog.V(4).Infof("Reverting security-group-rule creation %v", newRule.ID)
		mc := metrics.NewMetricContext("security_group_rule", "delete")
		err := rules.Delete(ctx, network, newRule.ID).ExtractErr()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to revert security-group-rule creation during error unwind: %v", err)
		}
	}
	return unwinder, nil
}

func deleteSecurityGroupRule(ctx context.Context, network *gophercloud.ServiceClient, rule *rules.SecGroupRule) (func(), error) {
	mc := metrics.NewMetricContext("security-group-rule", "delete")
	err := rules.Delete(ctx, network, rule.ID).ExtractErr()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	unwinder := func() {
		klog.V(4).Infof("Reverting security_group_rule deletion %v", rule)
		mc := metrics.NewMetricContext("security-group-rule", "create")
		_, err := rules.Create(ctx, network, rules.CreateOpts{SecGroupID: rule.ID, RemoteIPPrefix: rule.RemoteIPPrefix}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to revert security_group_rule deletion error unwind: %v", err)
		}
	}
	return unwinder, nil
}

// CreateRoute creates the described managed route
func (r *Routes) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	ip, _, _ := net.ParseCIDR(route.DestinationCIDR)
	isCIDRv6 := ip.To4() == nil

	nodes, err := r.os.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		return err
	}
	addr := getAddrByNodeName(route.TargetNode, isCIDRv6, nodes)
	if addr == "" {
		return errors.ErrNoAddressFound
	}

	klog.V(4).Infof("CreateRoute(%v, %v, %v)", clusterName, nameHint, route)

	onFailure := newCaller()

	klog.V(4).Infof("Using nexthop %v for node %v", addr, route.TargetNode)

	mc := metrics.NewMetricContext("router", "get")
	router, err := routers.Get(ctx, r.network, r.os.routeOpts.RouterID).Extract()
	if mc.ObserveRequest(err) != nil {
		return err
	}

	routeFound := false
	for _, item := range router.Routes {
		if item.DestinationCIDR == route.DestinationCIDR && item.NextHop == addr {
			routeFound = true
			break
		}
	}
	if !routeFound {
		klog.V(5).Info("Router's route rule not found, try to create")
		if !r.atomicRoutes {
			// classical logic
			r.Lock()
			defer r.Unlock()

			routes := append(router.Routes, routers.Route{
				DestinationCIDR: route.DestinationCIDR,
				NextHop:         addr,
			})

			unwind, err := updateRoutes(ctx, r.network, router, routes)
			if err != nil {
				return err
			}

			defer onFailure.call(unwind)
		} else {
			// atomic route update
			route := []routers.Route{{
				DestinationCIDR: route.DestinationCIDR,
				NextHop:         addr,
			}}
			unwind, err := addRoute(ctx, r.network, r.os.routeOpts.RouterID, route)
			if err != nil {
				return err
			}

			defer onFailure.call(unwind)
		}
	}

	if r.autoConfigSecurityGroup {
		if r.nodeSecurityGroupId == "" {
			err = r.getOrCreateNodeSecurityGroup(ctx, clusterName)
			if err != nil {
				return err
			}
		}
		// update node security group rules, so that other nodes permit the traffic from this
		// node (route.TargetNode) or this node's pods pass through
		origRules, err := openstackutil.GetSecurityGroupRules(r.network, rules.ListOpts{SecGroupID: r.nodeSecurityGroupId, Direction: string(rules.DirIngress)})
		if err != nil {
			return err
		}
		sgRuleForPodCidrFound, sgRuleForNodeAddressFound, err := r.checkSecurityGroupRulesExist(route.DestinationCIDR, route.TargetNode, origRules)
		if err != nil {
			return err
		}
		if !sgRuleForPodCidrFound {
			// add security group rule for the pods of this node (route.TargetNode)
			klog.V(5).Infof("Security group rule related to podCidr %s not found, try to create", route.DestinationCIDR)
			etherType := rules.EtherType4
			if isCIDRv6 {
				etherType = rules.EtherType6
			}
			unwind, err := createSecurityGroupRule(
				ctx, r.network,
				rules.CreateOpts{
					SecGroupID:     r.nodeSecurityGroupId,
					RemoteIPPrefix: route.DestinationCIDR,
					Direction:      rules.DirIngress,
					EtherType:      etherType})
			if err != nil {
				return err
			}
			defer onFailure.call(unwind)
		}
		if !sgRuleForNodeAddressFound {
			// add security group rule for this node (route.TargetNode)
			klog.V(5).Infof("Security group rule related to node %s not found, try to create", route.TargetNode)
			etherType := rules.EtherType4
			if isCIDRv6 {
				etherType = rules.EtherType6
			}
			unwind, err := createSecurityGroupRule(
				ctx, r.network,
				rules.CreateOpts{
					SecGroupID:     r.nodeSecurityGroupId,
					RemoteIPPrefix: addr,
					Direction:      rules.DirIngress,
					EtherType:      etherType})
			if err != nil {
				return err
			}
			defer onFailure.call(unwind)
		}
	}

	// get the port of addr on target node.
	port, err := r.getPortByIP(ctx, addr)
	if err != nil {
		return err
	}
	if !port.PortSecurityEnabled {
		klog.Warningf("Skipping update of the port : %s (allowed_address_pairs and node security_group)", port.ID)
		onFailure.disarm()
		return nil
	}

	if !r.allowedAddressPairs {
		klog.V(4).Infof("Route created (skipping the allowed_address_pairs update): %v", route)
	} else {
		// update node port's AllowedAddressPairs, so that the packets from the pods can pass through
		// the node's port and leave the node.
		allowAddressPairFound := false
		for _, item := range port.AllowedAddressPairs {
			if item.IPAddress == route.DestinationCIDR {
				klog.V(4).Infof("Found existing allowed-address-pair: %v", item)
				allowAddressPairFound = true
				break
			}
		}
		var newPairs []ports.AddressPair
		if !allowAddressPairFound {
			klog.V(5).Infof("AllowedAddressPairs rule related to podCidr %s not found, try to create", route.DestinationCIDR)
			newPairs = append(port.AllowedAddressPairs, ports.AddressPair{
				IPAddress: route.DestinationCIDR,
			})
			unwind, err := updateAllowedAddressPairs(ctx, r.network, port, newPairs)
			if err != nil {
				return err
			}
			defer onFailure.call(unwind)
		}
	}

	if r.autoConfigSecurityGroup {
		// node port bind node security group, so that other nodes' traffic can enter
		// into this node (route.TargetNode). in other words, permitting the packets the
		// source addresses are other nodes or their pods enter ino this node (route.TargetNode).
		nodePortBindSecurityGroup := false
		for _, sg := range port.SecurityGroups {
			if sg == r.nodeSecurityGroupId {
				nodePortBindSecurityGroup = true
				break
			}
		}
		if !nodePortBindSecurityGroup {
			klog.V(5).Infof("Try to bind node security group %s to node %s", r.nodeSecurityGroupId, route.TargetNode)
			newSgs := append(port.SecurityGroups, r.nodeSecurityGroupId)
			unwind, err := updateSecurityGroup(ctx, r.network, port, newSgs)
			if err != nil {
				return err
			}
			defer onFailure.call(unwind)
		}
	}
	klog.V(4).Infof("Route created: %v", route)
	onFailure.disarm()
	return nil
}

// DeleteRoute deletes the specified managed route
func (r *Routes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	klog.V(4).Infof("DeleteRoute(%v, %v)", clusterName, route)

	onFailure := newCaller()

	ip, _, _ := net.ParseCIDR(route.DestinationCIDR)
	isCIDRv6 := ip.To4() == nil
	var addr string

	// Blackhole routes are orphaned and have no counterpart in OpenStack
	if !route.Blackhole {
		nodes, err := r.os.nodeInformer.Lister().List(labels.Everything())
		if err != nil {
			return err
		}
		addr = getAddrByNodeName(route.TargetNode, isCIDRv6, nodes)
		if addr == "" {
			return errors.ErrNoAddressFound
		}
	}

	if !r.atomicRoutes {
		// classical logic
		r.Lock()
		defer r.Unlock()

		mc := metrics.NewMetricContext("router", "get")
		router, err := routers.Get(ctx, r.network, r.os.routeOpts.RouterID).Extract()
		if mc.ObserveRequest(err) != nil {
			return err
		}

		routes := router.Routes
		index := -1
		for i, item := range routes {
			if item.DestinationCIDR == route.DestinationCIDR && (item.NextHop == addr || route.Blackhole && item.NextHop == string(route.TargetNode)) {
				index = i
				break
			}
		}

		if index == -1 {
			klog.V(4).Infof("Skipping non-existent route: %v", route)
			return nil
		}

		// Delete element `index`
		routes[index] = routes[len(routes)-1]
		routes = routes[:len(routes)-1]

		unwind, err := updateRoutes(ctx, r.network, router, routes)
		// If this was a blackhole route we are done, there are no ports to update
		if err != nil || route.Blackhole {
			return err
		}

		defer onFailure.call(unwind)
	} else {
		// atomic route update
		blackhole := route.Blackhole
		if blackhole {
			addr = string(route.TargetNode)
		}
		route := []routers.Route{{
			DestinationCIDR: route.DestinationCIDR,
			NextHop:         addr,
		}}
		unwind, err := removeRoute(ctx, r.network, r.os.routeOpts.RouterID, route)
		// If this was a blackhole route we are done, there are no ports to update
		if err != nil || blackhole {
			return err
		}

		defer onFailure.call(unwind)
	}

	origRules, err := openstackutil.GetSecurityGroupRules(r.network, rules.ListOpts{SecGroupID: r.nodeSecurityGroupId, Direction: string(rules.DirIngress)})
	if err != nil {
		return err
	}
	var staleRule *rules.SecGroupRule
	for _, rule := range origRules {
		if rule.RemoteIPPrefix == route.DestinationCIDR {
			staleRule = &rule
		}
	}
	if staleRule != nil {
		unwind, err := deleteSecurityGroupRule(ctx, r.network, staleRule)
		if err != nil {
			return err
		}
		defer onFailure.call(unwind)
	}

	// get the port of addr on target node.
	port, err := r.getPortByIP(ctx, addr)
	if err != nil {
		return err
	}
	if !port.PortSecurityEnabled {
		klog.Warningf("Skipping update of port : %s (allowed_address_pairs and node security_group)", port.ID)
		onFailure.disarm()
		return nil
	}

	if !r.allowedAddressPairs {
		klog.V(4).Infof("Route deleted (skipping the allowed_address_pairs update): %v", route)
	} else {
		addrPairs := port.AllowedAddressPairs
		addrPairIndex := -1
		for i, item := range addrPairs {
			if item.IPAddress == route.DestinationCIDR {
				addrPairIndex = i
				break
			}
		}
		if addrPairIndex != -1 {
			// Delete element `index`
			addrPairs[addrPairIndex] = addrPairs[len(addrPairs)-1]
			addrPairs = addrPairs[:len(addrPairs)-1]

			unwind, err := updateAllowedAddressPairs(ctx, r.network, port, addrPairs)
			if err != nil {
				return err
			}
			defer onFailure.call(unwind)
		}
	}

	if r.autoConfigSecurityGroup {
		sgs := port.SecurityGroups
		sgIndex := -1
		for i, item := range sgs {
			if item == r.nodeSecurityGroupId {
				sgIndex = i
			}
		}
		if sgIndex != -1 {
			// Delete element `index`
			sgs[sgIndex] = sgs[len(sgs)-1]
			sgs = sgs[:len(sgs)-1]
			unwind, err := updateSecurityGroup(ctx, r.network, port, sgs)
			if err != nil {
				return err
			}
			defer onFailure.call(unwind)
		}
	}

	klog.V(4).Infof("Route deleted: %v", route)
	onFailure.disarm()
	return nil
}

func (r *Routes) getPortByIP(ctx context.Context, addr string) (*PortWithPortSecurity, error) {
	for _, networkID := range r.networkIDs {
		opts := ports.ListOpts{
			FixedIPs: []ports.FixedIPOpts{
				{
					IPAddress: addr,
				},
			},
			NetworkID: networkID,
		}
		ports, err := openstackutil.GetPorts[PortWithPortSecurity](ctx, r.network, opts)
		if err != nil {
			return nil, err
		}
		if len(ports) != 1 {
			continue
		}
		return &ports[0], nil
	}

	return nil, errors.ErrNotFound
}
