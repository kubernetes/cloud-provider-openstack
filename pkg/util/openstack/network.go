/*
Copyright 2019 The Kubernetes Authors.

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
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/external"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/networks"
	neutronports "github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/pagination"

	"k8s.io/cloud-provider-openstack/pkg/metrics"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
)

// GetNetworkExtensions returns an extension map.
func GetNetworkExtensions(client *gophercloud.ServiceClient) (map[string]bool, error) {
	seen := make(map[string]bool)

	mc := metrics.NewMetricContext("network_extension", "list")
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

	return seen, mc.ObserveRequest(err)
}

// GetFloatingIPs returns all the filtered floating IPs
func GetFloatingIPs(client *gophercloud.ServiceClient, opts floatingips.ListOpts) ([]floatingips.FloatingIP, error) {
	var floatingIPList []floatingips.FloatingIP

	mc := metrics.NewMetricContext("floating_ip", "list")
	allPages, err := floatingips.List(client, opts).AllPages()
	if mc.ObserveRequest(err) != nil {
		return floatingIPList, err
	}
	floatingIPList, err = floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return floatingIPList, err
	}

	return floatingIPList, nil
}

// GetFloatingIPByPortID get the floating IP of the given port.
func GetFloatingIPByPortID(client *gophercloud.ServiceClient, portID string) (*floatingips.FloatingIP, error) {
	opt := floatingips.ListOpts{
		PortID: portID,
	}
	ips, err := GetFloatingIPs(client, opt)
	if err != nil {
		return nil, err
	}

	if len(ips) == 0 {
		return nil, nil
	}

	return &ips[0], nil
}

// GetFloatingNetworkID returns a floating network ID.
func GetFloatingNetworkID(client *gophercloud.ServiceClient) (string, error) {
	var floatingNetworkIds []string

	type NetworkWithExternalExt struct {
		networks.Network
		external.NetworkExternalExt
	}

	mc := metrics.NewMetricContext("network", "list")
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
			return false, cpoerrors.ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return "", mc.ObserveRequest(cpoerrors.ErrNotFound)
		}

		if err == cpoerrors.ErrMultipleResults {
			return floatingNetworkIds[0], mc.ObserveRequest(nil)
		}
		return "", mc.ObserveRequest(err)
	}

	if len(floatingNetworkIds) == 0 {
		return "", mc.ObserveRequest(cpoerrors.ErrNotFound)
	}

	return floatingNetworkIds[0], mc.ObserveRequest(nil)
}

// GetPorts gets all the filtered ports.
func GetPorts(client *gophercloud.ServiceClient, listOpts neutronports.ListOpts) ([]neutronports.Port, error) {
	mc := metrics.NewMetricContext("port", "list")
	allPages, err := neutronports.List(client, listOpts).AllPages()
	if mc.ObserveRequest(err) != nil {
		return []neutronports.Port{}, err
	}
	allPorts, err := neutronports.ExtractPorts(allPages)
	if err != nil {
		return []neutronports.Port{}, err
	}

	return allPorts, nil
}
