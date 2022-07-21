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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/mitchellh/mapstructure"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/cloud-provider-openstack/pkg/util"
	"k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
)

// Instances encapsulates an implementation of Instances for OpenStack.
type Instances struct {
	compute        *gophercloud.ServiceClient
	opts           metadata.MetadataOpts
	networkingOpts NetworkingOpts
}

const (
	instanceShutoff = "SHUTOFF"
	noSortPriority  = 0
)

var _ cloudprovider.Instances = &Instances{}

// buildAddressSortOrderList builds a list containing only valid CIDRs based on the content of addressSortOrder.
//
// It will ignore and warn about invalid sort order items.
func buildAddressSortOrderList(addressSortOrder string) []*net.IPNet {
	var list []*net.IPNet
	for _, item := range strings.Split(addressSortOrder, ",") {
		item = strings.TrimSpace(item)

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
			fmt.Println(i, cidr, len(list)-i)
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

// Instances returns an implementation of Instances for OpenStack.
func (os *OpenStack) Instances() (cloudprovider.Instances, bool) {
	return os.instances()
}

// InstancesV2 returns an implementation of InstancesV2 for OpenStack.
// TODO: Support InstancesV2 in the future.
func (os *OpenStack) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return nil, false
}

func (os *OpenStack) instances() (*Instances, bool) {
	klog.V(4).Info("openstack.Instances() called")

	compute, err := client.NewComputeV2(os.provider, os.epOpts)
	if err != nil {
		klog.Errorf("unable to access compute v2 API : %v", err)
		return nil, false
	}

	return &Instances{
		compute:        compute,
		opts:           os.metadataOpts,
		networkingOpts: os.networkingOpts,
	}, true
}

// InstanceID returns the kubelet's cloud provider ID.
func (os *OpenStack) InstanceID() (string, error) {
	if len(os.localInstanceID) == 0 {
		id, err := readInstanceID(os.metadataOpts.SearchOrder)
		if err != nil {
			return "", err
		}
		os.localInstanceID = id
	}
	return os.localInstanceID, nil
}

// CurrentNodeName implements Instances.CurrentNodeName
// Note this is *not* necessarily the same as hostname.
func (i *Instances) CurrentNodeName(ctx context.Context, hostname string) (types.NodeName, error) {
	md, err := metadata.Get(i.opts.SearchOrder)
	if err != nil {
		return "", err
	}
	return types.NodeName(md.Name), nil
}

// AddSSHKeyToAllInstances is not implemented for OpenStack
func (i *Instances) AddSSHKeyToAllInstances(ctx context.Context, user string, keyData []byte) error {
	return cloudprovider.NotImplemented
}

// NodeAddresses implements Instances.NodeAddresses
func (i *Instances) NodeAddresses(ctx context.Context, name types.NodeName) ([]v1.NodeAddress, error) {
	klog.V(4).Infof("NodeAddresses(%v) called", name)

	addrs, err := getAddressesByName(i.compute, name, i.networkingOpts)
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("NodeAddresses(%v) => %v", name, addrs)
	return addrs, nil
}

// NodeAddressesByProviderID returns the node addresses of an instances with the specified unique providerID
// This method will not be called from the node that is requesting this ID. i.e. metadata service
// and other local methods cannot be used here
func (i *Instances) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]v1.NodeAddress, error) {
	klog.V(4).Infof("NodeAddressesByProviderID (%v) called", providerID)

	instanceID, err := instanceIDFromProviderID(providerID)

	if err != nil {
		return []v1.NodeAddress{}, err
	}

	mc := metrics.NewMetricContext("server", "get")
	server, err := servers.Get(i.compute, instanceID).Extract()

	if mc.ObserveRequest(err) != nil {
		return []v1.NodeAddress{}, err
	}

	interfaces, err := getAttachedInterfacesByID(i.compute, server.ID)
	if err != nil {
		return []v1.NodeAddress{}, err
	}

	addresses, err := nodeAddresses(server, interfaces, i.networkingOpts)
	if err != nil {
		return []v1.NodeAddress{}, err
	}

	klog.V(4).Infof("NodeAddressesByProviderID(%v) => %v", providerID, addresses)
	return addresses, nil
}

// InstanceExists returns true if the instance for the given node exists.
func (i *Instances) InstanceExists(ctx context.Context, node *v1.Node) (bool, error) {
	return i.InstanceExistsByProviderID(ctx, node.Spec.ProviderID)
}

// InstanceExistsByProviderID returns true if the instance with the given provider id still exists.
// If false is returned with no error, the instance will be immediately deleted by the cloud controller manager.
func (i *Instances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	instanceID, err := instanceIDFromProviderID(providerID)
	if err != nil {
		return false, err
	}

	mc := metrics.NewMetricContext("server", "get")
	_, err = servers.Get(i.compute, instanceID).Extract()
	if mc.ObserveRequest(err) != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// InstanceShutdown returns true if the instances is in safe state to detach volumes.
// It is the only state, where volumes can be detached immediately.
func (i *Instances) InstanceShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	return i.InstanceShutdownByProviderID(ctx, node.Spec.ProviderID)
}

// InstanceShutdownByProviderID returns true if the instances is in safe state to detach volumes.
// It is the only state, where volumes can be detached immediately.
func (i *Instances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	instanceID, err := instanceIDFromProviderID(providerID)
	if err != nil {
		return false, err
	}

	mc := metrics.NewMetricContext("server", "get")
	server, err := servers.Get(i.compute, instanceID).Extract()
	if mc.ObserveRequest(err) != nil {
		return false, err
	}

	// SHUTOFF is the only state where we can detach volumes immediately
	if server.Status == instanceShutoff {
		return true, nil
	}
	return false, nil
}

// InstanceMetadata returns metadata of the specified instance.
func (i *Instances) InstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	instanceID, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return nil, err
	}

	srv, err := servers.Get(i.compute, instanceID).Extract()
	if err != nil {
		return nil, err
	}

	instanceType, err := srvInstanceType(i.compute, srv)
	if err != nil {
		return nil, err
	}

	interfaces, err := getAttachedInterfacesByID(i.compute, srv.ID)
	if err != nil {
		return nil, err
	}
	addresses, err := nodeAddresses(srv, interfaces, i.networkingOpts)
	if err != nil {
		return nil, err
	}

	return &cloudprovider.InstanceMetadata{
		ProviderID:    node.Spec.ProviderID,
		InstanceType:  instanceType,
		NodeAddresses: addresses,
	}, nil
}

// InstanceID returns the cloud provider ID of the specified instance.
func (i *Instances) InstanceID(ctx context.Context, name types.NodeName) (string, error) {
	srv, err := getServerByName(i.compute, name)
	if err != nil {
		if err == errors.ErrNotFound {
			return "", cloudprovider.InstanceNotFound
		}
		return "", err
	}
	// In the future it is possible to also return an endpoint as:
	// <endpoint>/<instanceid>
	return "/" + srv.ID, nil
}

// InstanceTypeByProviderID returns the cloudprovider instance type of the node with the specified unique providerID
// This method will not be called from the node that is requesting this ID. i.e. metadata service
// and other local methods cannot be used here
func (i *Instances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	instanceID, err := instanceIDFromProviderID(providerID)

	if err != nil {
		return "", err
	}

	mc := metrics.NewMetricContext("server", "get")
	server, err := servers.Get(i.compute, instanceID).Extract()

	if mc.ObserveRequest(err) != nil {
		return "", err
	}

	return srvInstanceType(i.compute, server)
}

// InstanceType returns the type of the specified instance.
func (i *Instances) InstanceType(ctx context.Context, name types.NodeName) (string, error) {
	srv, err := getServerByName(i.compute, name)

	if err != nil {
		return "", err
	}

	return srvInstanceType(i.compute, &srv.Server)
}

func srvInstanceType(client *gophercloud.ServiceClient, srv *servers.Server) (string, error) {
	keys := []string{"original_name", "id"}
	for _, key := range keys {
		val, found := srv.Flavor[key]
		if !found {
			continue
		}

		flavor, ok := val.(string)
		if !ok {
			continue
		}

		if key == "original_name" && isValidLabelValue(flavor) {
			return flavor, nil
		}

		// get flavor name by id
		mc := metrics.NewMetricContext("flavor", "get")
		f, err := flavors.Get(client, flavor).Extract()
		if mc.ObserveRequest(err) == nil {
			if isValidLabelValue(f.Name) {
				return f.Name, nil
			}
			// fallback on flavor id
			return f.ID, nil
		}
	}
	return "", fmt.Errorf("flavor original_name/id not found")
}

func isValidLabelValue(v string) bool {
	if errs := validation.IsValidLabelValue(v); len(errs) != 0 {
		return false
	}
	return true
}

// If Instances.InstanceID or cloudprovider.GetInstanceProviderID is changed, the regexp should be changed too.
var providerIDRegexp = regexp.MustCompile(`^` + ProviderName + `:///([^/]+)$`)

// instanceIDFromProviderID splits a provider's id and return instanceID.
// A providerID is build out of '${ProviderName}:///${instance-id}'which contains ':///'.
// See cloudprovider.GetInstanceProviderID and Instances.InstanceID.
func instanceIDFromProviderID(providerID string) (instanceID string, err error) {

	// https://github.com/kubernetes/kubernetes/issues/85731
	if providerID != "" && !strings.Contains(providerID, "://") {
		providerID = ProviderName + "://" + providerID
	}

	matches := providerIDRegexp.FindStringSubmatch(providerID)
	if len(matches) != 2 {
		return "", fmt.Errorf("ProviderID \"%s\" didn't match expected format \"openstack:///InstanceID\"", providerID)
	}
	return matches[1], nil
}

// AddToNodeAddresses appends the NodeAddresses to the passed-by-pointer slice,
// only if they do not already exist
func AddToNodeAddresses(addresses *[]v1.NodeAddress, addAddresses ...v1.NodeAddress) {
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

// RemoveFromNodeAddresses removes the NodeAddresses from the passed-by-pointer
// slice if they already exist.
func RemoveFromNodeAddresses(addresses *[]v1.NodeAddress, removeAddresses ...v1.NodeAddress) {
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

// mapNodeNameToServerName maps a k8s NodeName to an OpenStack Server Name
// This is a simple string cast.
func mapNodeNameToServerName(nodeName types.NodeName) string {
	return string(nodeName)
}

// mapServerToNodeName maps an OpenStack Server to a k8s NodeName
func mapServerToNodeName(server *servers.Server) types.NodeName {
	// Node names are always lowercase, and (at least)
	// routecontroller does case-sensitive string comparisons
	// assuming this
	return types.NodeName(strings.ToLower(server.Name))
}

func readInstanceID(searchOrder string) (string, error) {
	// First, try to get data from metadata service because local
	// data might be changed by accident
	md, err := metadata.Get(searchOrder)
	if err == nil {
		return md.UUID, nil
	}

	// Try to find instance ID on the local filesystem (created by cloud-init)
	const instanceIDFile = "/var/lib/cloud/data/instance-id"
	idBytes, err := ioutil.ReadFile(instanceIDFile)
	if err == nil {
		instanceID := string(idBytes)
		instanceID = strings.TrimSpace(instanceID)
		klog.V(3).Infof("Got instance id from %s: %s", instanceIDFile, instanceID)
		if instanceID != "" && instanceID != "iid-datasource-none" {
			return instanceID, nil
		}
	}

	return "", err
}

func getServerByName(client *gophercloud.ServiceClient, name types.NodeName) (*ServerAttributesExt, error) {
	opts := servers.ListOpts{
		Name: fmt.Sprintf("^%s$", regexp.QuoteMeta(mapNodeNameToServerName(name))),
	}

	var s []ServerAttributesExt
	serverList := make([]ServerAttributesExt, 0, 1)

	mc := metrics.NewMetricContext("server", "list")
	pager := servers.List(client, opts)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		if err := servers.ExtractServersInto(page, &s); err != nil {
			return false, err
		}
		serverList = append(serverList, s...)
		if len(serverList) > 1 {
			return false, errors.ErrMultipleResults
		}
		return true, nil
	})
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	if len(serverList) == 0 {
		return nil, errors.ErrNotFound
	}

	return &serverList[0], nil
}

// IP addresses order:
// * interfaces private IPs
// * access IPs
// * metadata hostname
// * server object Addresses (floating type)
func nodeAddresses(srv *servers.Server, interfaces []attachinterfaces.Interface, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	addrs := []v1.NodeAddress{}

	// parse private IP addresses first in an ordered manner
	for _, iface := range interfaces {
		for _, fixedIP := range iface.FixedIPs {
			if iface.PortState == "ACTIVE" {
				isIPv6 := net.ParseIP(fixedIP.IPAddress).To4() == nil
				if !(isIPv6 && networkingOpts.IPv6SupportDisabled) {
					AddToNodeAddresses(&addrs,
						v1.NodeAddress{
							Type:    v1.NodeInternalIP,
							Address: fixedIP.IPAddress,
						},
					)
				}
			}
		}
	}

	// process public IP addresses
	if srv.AccessIPv4 != "" {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv4,
			},
		)
	}

	if srv.AccessIPv6 != "" && !networkingOpts.IPv6SupportDisabled {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv6,
			},
		)
	}

	if srv.Metadata[TypeHostName] != "" {
		AddToNodeAddresses(&addrs,
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

	var networks []string
	for k := range addresses {
		networks = append(networks, k)
	}
	sort.Strings(networks)

	for _, network := range networks {
		for _, props := range addresses[network] {
			var addressType v1.NodeAddressType
			if props.IPType == "floating" {
				addressType = v1.NodeExternalIP
			} else if util.Contains(networkingOpts.PublicNetworkName, network) {
				addressType = v1.NodeExternalIP
				// removing already added address to avoid listing it as both ExternalIP and InternalIP
				// may happen due to listing "private" network as "public" in CCM's config
				RemoveFromNodeAddresses(&addrs,
					v1.NodeAddress{
						Address: props.Addr,
					},
				)
			} else {
				if len(networkingOpts.InternalNetworkName) == 0 || util.Contains(networkingOpts.InternalNetworkName, network) {
					addressType = v1.NodeInternalIP
				} else {
					klog.V(5).Infof("Node '%s' address '%s' ignored due to 'internal-network-name' option", srv.Name, props.Addr)
					RemoveFromNodeAddresses(&addrs,
						v1.NodeAddress{
							Address: props.Addr,
						},
					)
					continue
				}
			}

			isIPv6 := net.ParseIP(props.Addr).To4() == nil
			if !(isIPv6 && networkingOpts.IPv6SupportDisabled) {
				AddToNodeAddresses(&addrs,
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

	return addrs, nil
}

func getAddressesByName(client *gophercloud.ServiceClient, name types.NodeName, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	srv, err := getServerByName(client, name)
	if err != nil {
		return nil, err
	}

	interfaces, err := getAttachedInterfacesByID(client, srv.ID)
	if err != nil {
		return nil, err
	}

	return nodeAddresses(&srv.Server, interfaces, networkingOpts)
}

func getAddressByName(client *gophercloud.ServiceClient, name types.NodeName, needIPv6 bool, networkingOpts NetworkingOpts) (string, error) {
	if needIPv6 && networkingOpts.IPv6SupportDisabled {
		return "", errors.ErrIPv6SupportDisabled
	}

	addrs, err := getAddressesByName(client, name, networkingOpts)
	if err != nil {
		return "", err
	} else if len(addrs) == 0 {
		return "", errors.ErrNoAddressFound
	}

	for _, addr := range addrs {
		isIPv6 := net.ParseIP(addr.Address).To4() == nil
		if (addr.Type == v1.NodeInternalIP) && (isIPv6 == needIPv6) {
			return addr.Address, nil
		}
	}

	for _, addr := range addrs {
		isIPv6 := net.ParseIP(addr.Address).To4() == nil
		if (addr.Type == v1.NodeExternalIP) && (isIPv6 == needIPv6) {
			return addr.Address, nil
		}
	}
	// It should never return an address from a different IP Address family than the one needed
	return "", errors.ErrNoAddressFound
}

// getAttachedInterfacesByID returns the node interfaces of the specified instance.
func getAttachedInterfacesByID(client *gophercloud.ServiceClient, serviceID string) ([]attachinterfaces.Interface, error) {
	var interfaces []attachinterfaces.Interface

	mc := metrics.NewMetricContext("server_os_interface", "list")
	pager := attachinterfaces.List(client, serviceID)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		s, err := attachinterfaces.ExtractInterfaces(page)
		if err != nil {
			return false, err
		}
		interfaces = append(interfaces, s...)
		return true, nil
	})
	if mc.ObserveRequest(err) != nil {
		return interfaces, err
	}

	return interfaces, nil
}
