/*
Copyright 2024 The Kubernetes Authors.

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
	"bytes"
	"context"
	"net"
	"slices"
	"sort"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	neutronports "github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/mitchellh/mapstructure"

	v1 "k8s.io/api/core/v1"
	"k8s.io/cloud-provider-openstack/pkg/util"
	"k8s.io/klog/v2"
)

const (
	noSortPriority = 0
)

// buildAddressSortOrderList builds a list containing only valid CIDRs based on the content of addressSortOrder.
//
// It will ignore and warn about invalid sort order items.
func buildAddressSortOrderList(addressSortOrder string) []*net.IPNet {
	var list []*net.IPNet
	for _, item := range util.SplitTrim(addressSortOrder, ',') {
		_, cidr, err := net.ParseCIDR(item)
		if err != nil {
			klog.Warningf("Ignoring invalid sort order item '%s': %v.", item, err)
			continue
		}

		list = append(list, cidr)
	}

	return list
}

// getSortPriority returns the priority as int of an address.
//
// The priority depends on the index of the CIDR in the list the address is matching,
// where the first item of the list has higher priority than the last.
//
// If the address does not match any CIDR or is not an IP address the function returns noSortPriority.
func getSortPriority(list []*net.IPNet, address string) int {
	parsedAddress := net.ParseIP(address)
	if parsedAddress == nil {
		return noSortPriority
	}

	for i, cidr := range list {
		if cidr.Contains(parsedAddress) {
			return len(list) - i
		}
	}

	return noSortPriority
}

// sortNodeAddresses sorts node addresses based on comma separated list of CIDRs represented by addressSortOrder.
//
// The function only sorts addresses which match the CIDR and leaves the other addresses in the same order they are in.
// Essentially, it will also group the addresses matching a CIDR together and sort them ascending in this group,
// whereas the inter-group sorting depends on the priority.
//
// The priority depends on the order of the item in addressSortOrder, where the first item has higher priority than the last.
func sortNodeAddresses(addresses []v1.NodeAddress, addressSortOrder string) {
	list := buildAddressSortOrderList(addressSortOrder)

	sort.SliceStable(addresses, func(i int, j int) bool {
		addressLeft := addresses[i]
		addressRight := addresses[j]

		priorityLeft := getSortPriority(list, addressLeft.Address)
		priorityRight := getSortPriority(list, addressRight.Address)

		// ignore priorities of value 0 since this means the address has noSortPriority and we need to sort by priority
		if priorityLeft > noSortPriority && priorityLeft == priorityRight {
			return bytes.Compare(net.ParseIP(addressLeft.Address), net.ParseIP(addressRight.Address)) < 0
		}

		return priorityLeft > priorityRight
	})
}

// addToNodeAddresses appends the NodeAddresses to the passed-by-pointer slice,
// only if they do not already exist
func addToNodeAddresses(addresses *[]v1.NodeAddress, addAddresses ...v1.NodeAddress) {
	for _, add := range addAddresses {
		exists := false
		for _, existing := range *addresses {
			if existing.Address == add.Address && existing.Type == add.Type {
				exists = true
				break
			}
		}
		if !exists {
			*addresses = append(*addresses, add)
		}
	}
}

// removeFromNodeAddresses removes the NodeAddresses from the passed-by-pointer
// slice if they already exist.
func removeFromNodeAddresses(addresses *[]v1.NodeAddress, removeAddresses ...v1.NodeAddress) {
	var indexesToRemove []int
	for _, remove := range removeAddresses {
		for i := len(*addresses) - 1; i >= 0; i-- {
			existing := (*addresses)[i]
			if existing.Address == remove.Address && (existing.Type == remove.Type || remove.Type == "") {
				indexesToRemove = append(indexesToRemove, i)
			}
		}
	}
	for _, i := range indexesToRemove {
		if i < len(*addresses) {
			*addresses = append((*addresses)[:i], (*addresses)[i+1:]...)
		}
	}
}

// IP addresses order:
// * interfaces private IPs
// * access IPs
// * metadata hostname
// * server object Addresses (floating type)
func nodeAddresses(ctx context.Context, srv *servers.Server, ports []PortWithTrunkDetails, client *gophercloud.ServiceClient, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	addrs := []v1.NodeAddress{}

	// parse private IP addresses first in an ordered manner
	for _, port := range ports {
		for _, fixedIP := range port.FixedIPs {
			if port.Status != "ACTIVE" {
				continue
			}
			isIPv6 := net.ParseIP(fixedIP.IPAddress).To4() == nil
			if !isIPv6 || !networkingOpts.IPv6SupportDisabled {
				addToNodeAddresses(&addrs,
					v1.NodeAddress{
						Type:    v1.NodeInternalIP,
						Address: fixedIP.IPAddress,
					},
				)
			}
		}
	}

	// process public IP addresses
	if srv.AccessIPv4 != "" {
		addToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv4,
			},
		)
	}

	if srv.AccessIPv6 != "" && !networkingOpts.IPv6SupportDisabled {
		addToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv6,
			},
		)
	}

	if srv.Metadata[TypeHostName] != "" {
		addToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeHostName,
				Address: srv.Metadata[TypeHostName],
			},
		)
	}

	// process the rest
	type Address struct {
		IPType string `mapstructure:"OS-EXT-IPS:type"`
		Addr   string
	}

	var addresses map[string][]Address
	err := mapstructure.Decode(srv.Addresses, &addresses)
	if err != nil {
		return nil, err
	}

	// Add the addresses assigned on subports via trunk
	// This exposes the vlan networks to which subports are attached
	for _, port := range ports {
		for _, subport := range port.SubPorts {
			p, err := neutronports.Get(ctx, client, subport.PortID).Extract()
			if err != nil {
				klog.Errorf("Failed to get subport %s details: %v", subport.PortID, err)
				continue
			}
			n, err := networks.Get(ctx, client, p.NetworkID).Extract()
			if err != nil {
				klog.Errorf("Failed to get subport %s network details: %v", subport.PortID, err)
				continue
			}
			for _, fixedIP := range p.FixedIPs {
				klog.V(5).Infof("Node '%s' is found subport '%s' address '%s/%s'", srv.Name, p.Name, n.Name, fixedIP.IPAddress)
				isIPv6 := net.ParseIP(fixedIP.IPAddress).To4() == nil
				if !isIPv6 || !networkingOpts.IPv6SupportDisabled {
					addr := Address{IPType: "fixed", Addr: fixedIP.IPAddress}
					subportAddresses := map[string][]Address{n.Name: {addr}}
					srvAddresses, ok := addresses[n.Name]
					if !ok {
						addresses[n.Name] = subportAddresses[n.Name]
					} else {
						// this is to take care the corner case
						// where the same network is attached to the node both directly and via trunk
						addresses[n.Name] = append(srvAddresses, subportAddresses[n.Name]...)
					}
				}
			}
		}
	}

	networks := make([]string, 0, len(addresses))
	for k := range addresses {
		networks = append(networks, k)
	}
	sort.Strings(networks)

	for _, network := range networks {
		for _, props := range addresses[network] {
			var addressType v1.NodeAddressType
			if props.IPType == "floating" {
				addressType = v1.NodeExternalIP
			} else if slices.Contains(networkingOpts.PublicNetworkName, network) {
				addressType = v1.NodeExternalIP
				// removing already added address to avoid listing it as both ExternalIP and InternalIP
				// may happen due to listing "private" network as "public" in CCM's config
				removeFromNodeAddresses(&addrs,
					v1.NodeAddress{
						Address: props.Addr,
					},
				)
			} else {
				if len(networkingOpts.InternalNetworkName) == 0 || slices.Contains(networkingOpts.InternalNetworkName, network) {
					addressType = v1.NodeInternalIP
				} else {
					klog.V(5).Infof("Node '%s' address '%s' ignored due to 'internal-network-name' option", srv.Name, props.Addr)
					removeFromNodeAddresses(&addrs,
						v1.NodeAddress{
							Address: props.Addr,
						},
					)
					continue
				}
			}

			isIPv6 := net.ParseIP(props.Addr).To4() == nil
			if !isIPv6 || !networkingOpts.IPv6SupportDisabled {
				addToNodeAddresses(&addrs,
					v1.NodeAddress{
						Type:    addressType,
						Address: props.Addr,
					},
				)
			}
		}
	}

	if networkingOpts.AddressSortOrder != "" {
		sortNodeAddresses(addrs, networkingOpts.AddressSortOrder)
	}

	klog.V(5).Infof("Node '%s' returns addresses '%v'", srv.Name, addrs)
	return addrs, nil
}
