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
	"context"
	"fmt"
	sysos "os"
	"regexp"
	"slices"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/pagination"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/cloud-provider-openstack/pkg/util"
	"k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

const (
	RegionalProviderIDEnv = "OS_CCM_REGIONAL"
	instanceShutoff       = "SHUTOFF"
)

// InstancesV2 encapsulates an implementation of InstancesV2 for OpenStack.
type InstancesV2 struct {
	compute          map[string]*gophercloud.ServiceClient
	network          map[string]*gophercloud.ServiceClient
	regions          []string
	regionProviderID bool
	networkingOpts   NetworkingOpts
}

// InstancesV2 returns an implementation of InstancesV2 for OpenStack.
func (os *OpenStack) InstancesV2() (cloudprovider.InstancesV2, bool) {
	klog.V(4).Info("openstack.Instancesv2() called")

	var err error
	compute := make(map[string]*gophercloud.ServiceClient, len(os.regions))
	network := make(map[string]*gophercloud.ServiceClient, len(os.regions))

	for _, region := range os.regions {
		opt := os.epOpts
		opt.Region = region

		compute[region], err = client.NewComputeV2(os.provider, opt)
		if err != nil {
			klog.Errorf("unable to access compute v2 API : %v", err)
			return nil, false
		}

		network[region], err = client.NewNetworkV2(os.provider, opt)
		if err != nil {
			klog.Errorf("unable to access network v2 API : %v", err)
			return nil, false
		}
	}

	regionalProviderID := false
	if isRegionalProviderID := sysos.Getenv(RegionalProviderIDEnv); isRegionalProviderID == "true" {
		regionalProviderID = true
	}

	return &InstancesV2{
		compute:          compute,
		network:          network,
		regions:          os.regions,
		regionProviderID: regionalProviderID,
		networkingOpts:   os.networkingOpts,
	}, true
}

// InstanceExists indicates whether a given node exists according to the cloud provider
func (i *InstancesV2) InstanceExists(ctx context.Context, node *v1.Node) (bool, error) {
	klog.V(4).InfoS("openstack.InstanceExists() called", "node", klog.KObj(node),
		"providerID", node.Spec.ProviderID,
		"region", node.Labels[v1.LabelTopologyRegion])

	_, _, err := i.getInstance(ctx, node)
	if err == cloudprovider.InstanceNotFound {
		klog.V(6).InfoS("Node is not found in cloud provider", "node", klog.KObj(node),
			"providerID", node.Spec.ProviderID,
			"region", node.Labels[v1.LabelTopologyRegion])
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// InstanceShutdown returns true if the instance is shutdown according to the cloud provider.
func (i *InstancesV2) InstanceShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	klog.V(4).InfoS("openstack.InstanceShutdown() called", "node", klog.KObj(node),
		"providerID", node.Spec.ProviderID,
		"region", node.Labels[v1.LabelTopologyRegion])

	server, _, err := i.getInstance(ctx, node)
	if err != nil {
		return false, err
	}

	// SHUTOFF is the only state where we can detach volumes immediately
	if server.Status == instanceShutoff {
		return true, nil
	}

	return false, nil
}

// InstanceMetadata returns the instance's metadata.
func (i *InstancesV2) InstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	klog.V(4).InfoS("openstack.InstanceMetadata() called", "node", klog.KObj(node),
		"providerID", node.Spec.ProviderID,
		"region", node.Labels[v1.LabelTopologyRegion])

	srv, region, err := i.getInstance(ctx, node)
	if err != nil {
		return nil, err
	}
	var server servers.Server
	if srv != nil {
		server = *srv
	}

	instanceType, err := srvInstanceType(ctx, i.compute[region], &server)
	if err != nil {
		return nil, err
	}

	ports, err := getAttachedPorts(ctx, i.network[region], server.ID)
	if err != nil {
		return nil, err
	}

	addresses, err := nodeAddresses(ctx, &server, ports, i.network[region], i.networkingOpts)
	if err != nil {
		return nil, err
	}

	availabilityZone := util.SanitizeLabel(server.AvailabilityZone)

	return &cloudprovider.InstanceMetadata{
		ProviderID:    i.makeInstanceID(&server, region),
		InstanceType:  instanceType,
		NodeAddresses: addresses,
		Zone:          availabilityZone,
		Region:        region,
	}, nil
}

func (i *InstancesV2) makeInstanceID(srv *servers.Server, region string) string {
	if i.regionProviderID {
		return fmt.Sprintf("%s://%s/%s", ProviderName, region, srv.ID)
	}
	return fmt.Sprintf("%s:///%s", ProviderName, srv.ID)
}

func (i *InstancesV2) getInstance(ctx context.Context, node *v1.Node) (*servers.Server, string, error) {
	var instanceID, instanceRegion string

	if node.Spec.ProviderID != "" {
		var err error

		instanceID, instanceRegion, err = instanceIDFromProviderID(node.Spec.ProviderID)
		if err != nil {
			return nil, "", err
		}
	}

	if instanceRegion != "" {
		if slices.Contains(i.regions, instanceRegion) {
			return i.getInstanceByID(ctx, instanceID, []string{instanceRegion})
		}

		return nil, "", fmt.Errorf("getInstance: ProviderID \"%s\" didn't match supported regions \"%s\"", node.Spec.ProviderID, strings.Join(i.regions, ","))
	}

	// At this point we know that ProviderID is not properly set or it doesn't contain region information
	// We need to search for the instance in all regions
	var searchRegions []string

	// We cannot trust the region label, so we need to check the region
	instanceRegion = node.Labels[v1.LabelTopologyRegion]
	if slices.Contains(i.regions, instanceRegion) {
		searchRegions = []string{instanceRegion}
	}

	for _, r := range i.regions {
		if r != instanceRegion {
			searchRegions = append(searchRegions, r)
		}
	}

	klog.V(4).InfoS("openstack.getInstance() trying to find the instance in regions", "node", klog.KObj(node),
		"instanceID", instanceID,
		"regions", strings.Join(searchRegions, ","))

	if instanceID == "" {
		return i.getInstanceByName(ctx, node.Name, searchRegions)
	}

	return i.getInstanceByID(ctx, instanceID, searchRegions)
}

func (i *InstancesV2) getInstanceByID(ctx context.Context, instanceID string, searchRegions []string) (*servers.Server, string, error) {
	server := servers.Server{}

	mc := metrics.NewMetricContext("server", "get")
	for _, r := range searchRegions {
		err := servers.Get(ctx, i.compute[r], instanceID).ExtractInto(&server)
		if mc.ObserveRequest(err) != nil {
			if errors.IsNotFound(err) {
				continue
			}

			return nil, "", err
		}

		return &server, r, nil
	}

	return nil, "", cloudprovider.InstanceNotFound
}

func (i *InstancesV2) getInstanceByName(ctx context.Context, name string, searchRegions []string) (*servers.Server, string, error) {
	opts := servers.ListOpts{
		Name: fmt.Sprintf("^%s$", regexp.QuoteMeta(name)),
	}

	serverList := make([]servers.Server, 0, 1)
	mc := metrics.NewMetricContext("server", "list")

	for _, r := range searchRegions {
		pager := servers.List(i.compute[r], opts)

		err := pager.EachPage(ctx, func(_ context.Context, page pagination.Page) (bool, error) {
			s, err := servers.ExtractServers(page)
			if err != nil {
				return false, err
			}
			serverList = append(serverList, s...)
			if len(serverList) > 1 {
				return false, errors.ErrMultipleResults
			}
			return true, nil
		})
		if mc.ObserveRequest(err) != nil {
			return nil, "", err
		}

		if len(serverList) == 0 {
			continue
		}

		return &serverList[0], r, nil
	}

	return nil, "", cloudprovider.InstanceNotFound
}

// If Instances.InstanceID or cloudprovider.GetInstanceProviderID is changed, the regexp should be changed too.
var providerIDRegexp = regexp.MustCompile(`^` + ProviderName + `://([^/]*)/([^/]+)$`)

// instanceIDFromProviderID splits a provider's id and return instanceID.
// A providerID is build out of '${ProviderName}:///${instance-id}' which contains ':///'.
// or '${ProviderName}://${region}/${instance-id}' which contains '://'.
// See cloudprovider.GetInstanceProviderID and Instances.InstanceID.
func instanceIDFromProviderID(providerID string) (instanceID string, region string, err error) {
	// https://github.com/kubernetes/kubernetes/issues/85731
	if providerID != "" && !strings.Contains(providerID, "://") {
		providerID = ProviderName + "://" + providerID
	}

	matches := providerIDRegexp.FindStringSubmatch(providerID)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("ProviderID \"%s\" didn't match expected format \"openstack://region/InstanceID\"", providerID)
	}
	return matches[2], matches[1], nil
}

// getAttachedPorts returns a list of ports attached to a server.
func getAttachedPorts(ctx context.Context, client *gophercloud.ServiceClient, serverID string) ([]PortWithTrunkDetails, error) {
	var allPorts []PortWithTrunkDetails

	listOpts := ports.ListOpts{
		DeviceID: serverID,
	}
	allPages, err := ports.List(client, listOpts).AllPages(ctx)
	if err != nil {
		return allPorts, err
	}

	err = ports.ExtractPortsInto(allPages, &allPorts)
	if err != nil {
		return allPorts, err
	}

	return allPorts, nil
}

func srvInstanceType(ctx context.Context, client *gophercloud.ServiceClient, srv *servers.Server) (string, error) {
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
		f, err := flavors.Get(ctx, client, flavor).Extract()
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
