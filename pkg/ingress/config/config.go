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

package config

import (
	"github.com/gophercloud/gophercloud"
)

// Config struct contains ingress controller configuration
type Config struct {
	ClusterName string
	Kubernetes  kubeConfig    `mapstructure:"kubernetes"`
	OpenStack   osConfig      `mapstructure:"openstack"`
	Octavia     octaviaConfig `mapstructure:"octavia"`
}

// Configuration for connecting to Kubernetes API server, either api_host or kubeconfig should be configured.
type kubeConfig struct {
	// (Optional)Kubernetes API server host address.
	ApiserverHost string `mapstructure:"api_host"`

	// (Optional)Kubeconfig file used to connect to Kubernetes cluster.
	KubeConfig string `mapstructure:"kubeconfig"`
}

// OpenStack credentials configuration, the section is required.
type osConfig struct {
	Username  string
	Password  string
	ProjectID string `mapstructure:"project_id"`
	AuthURL   string `mapstructure:"auth_url"`
	Region    string
}

// Octavia service related configuration
type octaviaConfig struct {
	// (Required)Subnet ID to create the load balancer.
	SubnetID string `mapstructure:"subnet_id"`

	// (Optional)Public network to create floating IP.
	// If empty, no floating IP will be allocated to the load balancer vip.
	FloatingIPNetwork string `mapstructure:"fip_network"`
}

// ToAuthOptions gets openstack auth options
func (cfg Config) ToAuthOptions() gophercloud.AuthOptions {
	return gophercloud.AuthOptions{
		IdentityEndpoint: cfg.OpenStack.AuthURL,
		Username:         cfg.OpenStack.Username,
		Password:         cfg.OpenStack.Password,
		TenantID:         cfg.OpenStack.ProjectID,
		DomainName:       "default",
		AllowReauth:      true,
	}
}
