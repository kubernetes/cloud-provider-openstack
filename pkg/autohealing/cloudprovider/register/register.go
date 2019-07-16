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
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/gophercloud/gophercloud"
	gopenstack "github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	netutil "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"

	"k8s.io/cloud-provider-openstack/pkg/autohealing/cloudprovider"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/cloudprovider/openstack"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/config"
)

func registerOpenStack(cfg config.Config, kubeClient kubernetes.Interface) (cloudprovider.CloudProvider, error) {
	client, err := gopenstack.NewClient(cfg.OpenStack.AuthURL)
	if err != nil {
		return nil, err
	}

	if cfg.OpenStack.CAFile != "" {
		roots, err := certutil.NewPool(cfg.OpenStack.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig := &tls.Config{}
		tlsConfig.RootCAs = roots
		client.HTTPClient.Transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: tlsConfig})
	}

	if cfg.OpenStack.TrustID != "" {
		opts := cfg.ToV3AuthOptions()
		authOptsExt := trusts.AuthOptsExt{
			TrustID:            cfg.OpenStack.TrustID,
			AuthOptionsBuilder: &opts,
		}
		err = gopenstack.AuthenticateV3(client, authOptsExt, gophercloud.EndpointOpts{})
	} else {
		err = gopenstack.Authenticate(client, cfg.ToAuthOptions())
	}

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
