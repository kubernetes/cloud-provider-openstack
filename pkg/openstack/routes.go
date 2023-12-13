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
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
	"net"
	"sync"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/extraroutes"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"

	"k8s.io/api/core/v1"
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
	// router's private network IDs
	networkIDs []string
	// whether Neutron supports "extraroute-atomic" extension
	atomicRoutes bool
	// whether Neutron supports "allowed-address-pairs" extension
	allowedAddressPairs bool
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
		network:             network,
		os:                  os,
		atomicRoutes:        atomicRoutes,
		allowedAddressPairs: allowedAddressPairs,
	}, nil
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
	router, err := routers.Get(r.network, r.os.routeOpts.RouterID).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	routes := make([]*cloudprovider.Route, 0, len(router.Routes))
	for _, item := range router.Routes {
		nodeName, foundNode := getNodeNameByAddr(item.NextHop, nodes)
		route := cloudprovider.Route{
			Name:            item.DestinationCIDR,
			TargetNode:      nodeName, //contains the nexthop address if node name was not found
			Blackhole:       !foundNode,
			DestinationCIDR: item.DestinationCIDR,
		}
		routes = append(routes, &route)
	}

	// detect router's private network ID for further VM ports filtering
	r.networkIDs, err = getRouterNetworkIDs(r.network, r.os.routeOpts.RouterID)
	if err != nil {
		return nil, err
	}

	return routes, nil
}

func getRouterNetworkIDs(network *gophercloud.ServiceClient, routerID string) ([]string, error) {
	opts := ports.ListOpts{
		DeviceID: routerID,
	}
	mc := metrics.NewMetricContext("port", "list")
	pages, err := ports.List(network, opts).AllPages()
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

func updateRoutes(network *gophercloud.ServiceClient, router *routers.Router, newRoutes []routers.Route) (func(), error) {
	origRoutes := router.Routes // shallow copy

	mc := metrics.NewMetricContext("router", "update")
	_, err := routers.Update(network, router.ID, routers.UpdateOpts{
		Routes: &newRoutes,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting routes change to router %v", router.ID)
		mc := metrics.NewMetricContext("router", "update")
		_, err := routers.Update(network, router.ID, routers.UpdateOpts{
			Routes: &origRoutes,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset routes during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

func addRoute(network *gophercloud.ServiceClient, routerID string, newRoute []routers.Route) (func(), error) {
	mc := metrics.NewMetricContext("router", "update")
	_, err := extraroutes.Add(network, routerID, extraroutes.Opts{
		Routes: &newRoute,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting routes change to router %v", routerID)
		mc := metrics.NewMetricContext("router", "update")
		_, err := extraroutes.Remove(network, routerID, extraroutes.Opts{
			Routes: &newRoute,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset routes during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

func removeRoute(network *gophercloud.ServiceClient, routerID string, oldRoute []routers.Route) (func(), error) {
	mc := metrics.NewMetricContext("router", "update")
	_, err := extraroutes.Remove(network, routerID, extraroutes.Opts{
		Routes: &oldRoute,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting routes change to router %v", routerID)
		mc := metrics.NewMetricContext("router", "update")
		_, err := extraroutes.Add(network, routerID, extraroutes.Opts{
			Routes: &oldRoute,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset routes during error unwind: %v", err)
		}
	}

	return unwinder, nil
}

func updateAllowedAddressPairs(network *gophercloud.ServiceClient, port *PortWithPortSecurity, newPairs []ports.AddressPair) (func(), error) {
	origPairs := port.AllowedAddressPairs // shallow copy

	mc := metrics.NewMetricContext("port", "update")
	_, err := ports.Update(network, port.ID, ports.UpdateOpts{
		AllowedAddressPairs: &newPairs,
	}).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	unwinder := func() {
		klog.V(4).Infof("Reverting allowed-address-pairs change to port %v", port.ID)
		mc := metrics.NewMetricContext("port", "update")
		_, err := ports.Update(network, port.ID, ports.UpdateOpts{
			AllowedAddressPairs: &origPairs,
		}).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Unable to reset allowed-address-pairs during error unwind: %v", err)
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

	if !r.atomicRoutes {
		// classical logic
		r.Lock()
		defer r.Unlock()

		mc := metrics.NewMetricContext("router", "get")
		router, err := routers.Get(r.network, r.os.routeOpts.RouterID).Extract()
		if mc.ObserveRequest(err) != nil {
			return err
		}

		routes := router.Routes

		for _, item := range routes {
			if item.DestinationCIDR == route.DestinationCIDR && item.NextHop == addr {
				klog.V(4).Infof("Skipping existing route: %v", route)
				return nil
			}
		}

		routes = append(routes, routers.Route{
			DestinationCIDR: route.DestinationCIDR,
			NextHop:         addr,
		})

		unwind, err := updateRoutes(r.network, router, routes)
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
		unwind, err := addRoute(r.network, r.os.routeOpts.RouterID, route)
		if err != nil {
			return err
		}

		defer onFailure.call(unwind)
	}

	if !r.allowedAddressPairs {
		klog.V(4).Infof("Route created (skipping the allowed_address_pairs update): %v", route)
		onFailure.disarm()
		return nil
	}

	// get the port of addr on target node.
	port, err := getPortByIP(r.network, addr, r.networkIDs)
	if err != nil {
		return err
	}
	if !port.PortSecurityEnabled {
		klog.Warningf("Skipping allowed_address_pair for port: %s", port.ID)
		onFailure.disarm()
		return nil
	}

	found := false
	for _, item := range port.AllowedAddressPairs {
		if item.IPAddress == route.DestinationCIDR {
			klog.V(4).Infof("Found existing allowed-address-pair: %v", item)
			found = true
			break
		}
	}

	if !found {
		newPairs := append(port.AllowedAddressPairs, ports.AddressPair{
			IPAddress: route.DestinationCIDR,
		})
		unwind, err := updateAllowedAddressPairs(r.network, port, newPairs)
		if err != nil {
			return err
		}
		defer onFailure.call(unwind)
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
		router, err := routers.Get(r.network, r.os.routeOpts.RouterID).Extract()
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

		unwind, err := updateRoutes(r.network, router, routes)
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
		unwind, err := removeRoute(r.network, r.os.routeOpts.RouterID, route)
		// If this was a blackhole route we are done, there are no ports to update
		if err != nil || blackhole {
			return err
		}

		defer onFailure.call(unwind)
	}

	if !r.allowedAddressPairs {
		klog.V(4).Infof("Route deleted (skipping the allowed_address_pairs update): %v", route)
		onFailure.disarm()
		return nil
	}

	// get the port of addr on target node.
	port, err := getPortByIP(r.network, addr, r.networkIDs)
	if err != nil {
		return err
	}
	if !port.PortSecurityEnabled {
		klog.Warningf("Skipping allowed_address_pair for port: %s", port.ID)
		onFailure.disarm()
		return nil
	}

	addrPairs := port.AllowedAddressPairs
	index := -1
	for i, item := range addrPairs {
		if item.IPAddress == route.DestinationCIDR {
			index = i
			break
		}
	}

	if index != -1 {
		// Delete element `index`
		addrPairs[index] = addrPairs[len(addrPairs)-1]
		addrPairs = addrPairs[:len(addrPairs)-1]

		unwind, err := updateAllowedAddressPairs(r.network, port, addrPairs)
		if err != nil {
			return err
		}
		defer onFailure.call(unwind)
	}

	klog.V(4).Infof("Route deleted: %v", route)
	onFailure.disarm()
	return nil
}

func getPortByIP(network *gophercloud.ServiceClient, addr string, networkIDs []string) (*PortWithPortSecurity, error) {
	for _, networkID := range networkIDs {
		opts := ports.ListOpts{
			FixedIPs: []ports.FixedIPOpts{
				{
					IPAddress: addr,
				},
			},
			NetworkID: networkID,
		}
		ports, err := openstackutil.GetPorts[PortWithPortSecurity](network, opts)
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
