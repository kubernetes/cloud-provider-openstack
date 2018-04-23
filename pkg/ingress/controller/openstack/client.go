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
	"fmt"
	"net/http"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	log "github.com/sirupsen/logrus"
	"k8s.io/cloud-provider-openstack/pkg/ingress/config"
)

// OpenStack is an implementation of cloud provider Interface for OpenStack.
type OpenStack struct {
	octavia *gophercloud.ServiceClient
	nova    *gophercloud.ServiceClient
	neutron *gophercloud.ServiceClient
	config  config.Config
}

func isNotFound(err error) bool {
	if _, ok := err.(gophercloud.ErrDefault404); ok {
		return true
	}

	if errCode, ok := err.(gophercloud.ErrUnexpectedResponseCode); ok {
		if errCode.Actual == http.StatusNotFound {
			return true
		}
	}

	return false
}

// NewOpenStack gets openstack struct
func NewOpenStack(cfg config.Config) (*OpenStack, error) {
	provider, err := openstack.NewClient(cfg.OpenStack.AuthURL)
	if err != nil {
		return nil, err
	}

	if err = openstack.Authenticate(provider, cfg.ToAuthOptions()); err != nil {
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

	log.Info("openstack client initialized")

	return &os, nil
}
