/*
Copyright 2018 The Kubernetes Authors.

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
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	log "github.com/sirupsen/logrus"

	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/cloud-provider-openstack/pkg/ingress/config"
	"k8s.io/cloud-provider-openstack/pkg/version"
)

// OpenStack is an implementation of cloud provider Interface for OpenStack.
type OpenStack struct {
	octavia *gophercloud.ServiceClient
	nova    *gophercloud.ServiceClient
	neutron *gophercloud.ServiceClient
	config  config.Config
}

// NewOpenStack gets openstack struct
func NewOpenStack(cfg config.Config) (*OpenStack, error) {
	provider, err := openstack.NewClient(cfg.OpenStack.AuthURL)
	if err != nil {
		return nil, err
	}

	userAgent := gophercloud.UserAgent{}
	userAgent.Prepend(fmt.Sprintf("octavia-ingress-controller/%s", version.Version))
	provider.UserAgent = userAgent

	if cfg.OpenStack.CAFile != "" {
		roots, err := certutil.NewPool(cfg.OpenStack.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig := &tls.Config{}
		tlsConfig.RootCAs = roots
		provider.HTTPClient.Transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: tlsConfig})
	}

	if cfg.OpenStack.TrustID != "" {
		opts := cfg.ToV3AuthOptions()
		authOptsExt := trusts.AuthOptsExt{
			TrustID:            cfg.OpenStack.TrustID,
			AuthOptionsBuilder: &opts,
		}
		err = openstack.AuthenticateV3(provider, authOptsExt, gophercloud.EndpointOpts{})
	} else {
		err = openstack.Authenticate(provider, cfg.ToAuthOptions())
	}

	if err != nil {
		return nil, err
	}

	// get octavia service client
	var lb *gophercloud.ServiceClient
	lb, err = openstack.NewLoadBalancerV2(provider, gophercloud.EndpointOpts{
		Region: cfg.OpenStack.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find octavia endpoint for region %s: %v", cfg.OpenStack.Region, err)
	}

	// get neutron service client
	var network *gophercloud.ServiceClient
	network, err = openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: cfg.OpenStack.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find neutron endpoint for region %s: %v", cfg.OpenStack.Region, err)
	}

	// get nova service client
	var compute *gophercloud.ServiceClient
	compute, err = openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: cfg.OpenStack.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find compute v2 endpoint for region %s: %v", cfg.OpenStack.Region, err)
	}

	os := OpenStack{
		octavia: lb,
		nova:    compute,
		neutron: network,
		config:  cfg,
	}

	log.Debug("openstack client initialized")

	return &os, nil
}
