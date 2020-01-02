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

// register package is introduced in order to avoid circle imports between openstack and cloudprovider packages.
package register

import (
	"fmt"

	"github.com/gophercloud/gophercloud"
	gopenstack "github.com/gophercloud/gophercloud/openstack"
	"k8s.io/client-go/kubernetes"

	"k8s.io/cloud-provider-openstack/pkg/autohealing/cloudprovider"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/cloudprovider/openstack"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/config"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
)

func registerOpenStack(cfg config.Config, kubeClient kubernetes.Interface) (cloudprovider.CloudProvider, error) {
	client, err := openstack_provider.NewOpenStackClient(&cfg.OpenStack, "magnum-auto-healer")
	if err != nil {
		return nil, err
	}

	// get nova service client
	var novaClient *gophercloud.ServiceClient
	novaClient, err = gopenstack.NewComputeV2(client, gophercloud.EndpointOpts{
		Region: cfg.OpenStack.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find Nova service endpoint in the region %s: %v", cfg.OpenStack.Region, err)
	}

	// get heat service client
	var heatClient *gophercloud.ServiceClient
	heatClient, err = gopenstack.NewOrchestrationV1(client, gophercloud.EndpointOpts{
		Region: cfg.OpenStack.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find Heat service endpoint in the region %s: %v", cfg.OpenStack.Region, err)
	}

	// get magnum service client
	var magnumClient *gophercloud.ServiceClient
	magnumClient, err = gopenstack.NewContainerInfraV1(client, gophercloud.EndpointOpts{
		Region: cfg.OpenStack.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find Magnum service endpoint in the region %s: %v", cfg.OpenStack.Region, err)
	}
	magnumClient.Microversion = "latest"

	var p cloudprovider.CloudProvider
	p = openstack.OpenStackCloudProvider{
		KubeClient: kubeClient,
		Nova:       novaClient,
		Heat:       heatClient,
		Magnum:     magnumClient,
		Config:     cfg,
	}

	return p, nil
}

func init() {
	cloudprovider.RegisterCloudProvider(openstack.ProviderName, registerOpenStack)
}
