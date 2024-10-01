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
	"context"
	"fmt"
	sysos "os"
	"slices"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/cloud-provider-openstack/pkg/util"
	"k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
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
	if !os.useV1Instances {
		return os.instancesv2()
	}
	return nil, false
}

func (os *OpenStack) instancesv2() (*InstancesV2, bool) {
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
	srv, region, err := i.getInstance(ctx, node)
	if err != nil {
		return nil, err
	}
	var server servers.Server
	if srv != nil {
		server = *srv
	}

	instanceType, err := srvInstanceType(i.compute[region], &server)
	if err != nil {
		return nil, err
	}

	ports, err := getAttachedPorts(i.network[region], server.ID)
	if err != nil {
		return nil, err
	}

	addresses, err := nodeAddresses(&server, ports, i.network[region], i.networkingOpts)
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

func (i *InstancesV2) getInstance(_ context.Context, node *v1.Node) (*servers.Server, string, error) {
	klog.V(4).InfoS("openstack.getInstance() called", "node", klog.KObj(node),
		"providerID", node.Spec.ProviderID,
		"region", node.Labels[v1.LabelTopologyRegion])

	instanceID, instanceRegion, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err == nil && instanceRegion != "" {
		if slices.Contains(i.regions, instanceRegion) {
			return i.getInstanceByID(instanceID, []string{instanceRegion})
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

	klog.V(6).InfoS("openstack.getInstance() trying to find the instance in regions", "node", klog.KObj(node),
		"instanceID", instanceID,
		"regions", strings.Join(searchRegions, ","))

	if instanceID == "" {
		return i.getInstanceByName(node, searchRegions)
	}

	return i.getInstanceByID(instanceID, searchRegions)
}

func (i *InstancesV2) getInstanceByID(instanceID string, searchRegions []string) (*servers.Server, string, error) {
	server := servers.Server{}

	mc := metrics.NewMetricContext("server", "get")
	for _, r := range searchRegions {
		err := servers.Get(context.TODO(), i.compute[r], instanceID).ExtractInto(&server)
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

func (i *InstancesV2) getInstanceByName(node *v1.Node, searchRegions []string) (*servers.Server, string, error) {
	opt := servers.ListOpts{
		Name: fmt.Sprintf("^%s$", node.Name),
	}

	serverList := make([]servers.Server, 0, 1)
	mc := metrics.NewMetricContext("server", "list")

	for _, r := range searchRegions {
		allPages, err := servers.List(i.compute[r], opt).AllPages(context.TODO())
		if mc.ObserveRequest(err) != nil {
			return nil, "", fmt.Errorf("error listing servers %v: %v", opt, err)
		}

		err = servers.ExtractServersInto(allPages, &serverList)
		if err != nil {
			return nil, "", fmt.Errorf("error extracting servers from pages: %v", err)
		}
		if len(serverList) == 0 {
			continue
		}
		if len(serverList) > 1 {
			return nil, "", fmt.Errorf("getInstanceByName: multiple instances found")
		}

		return &serverList[0], r, nil
	}

	return nil, "", cloudprovider.InstanceNotFound
}
