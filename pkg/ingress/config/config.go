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
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
)

// Config struct contains ingress controller configuration
type Config struct {
	ClusterName string        `mapstructure:"cluster-name"`
	Kubernetes  kubeConfig    `mapstructure:"kubernetes"`
	OpenStack   osConfig      `mapstructure:"openstack"`
	Octavia     octaviaConfig `mapstructure:"octavia"`
}

// Configuration for connecting to Kubernetes API server, either api_host or kubeconfig should be configured.
type kubeConfig struct {
	// (Optional)Kubernetes API server host address.
	ApiserverHost string `mapstructure:"api-host"`

	// (Optional)Kubeconfig file used to connect to Kubernetes cluster.
	KubeConfig string `mapstructure:"kubeconfig"`
}

// OpenStack credentials configuration, the section is required.
type osConfig struct {
	Username   string
	UserID     string `mapstructure:"user-id"`
	TrustID    string `mapstructure:"trust-id"`
	Password   string
	ProjectID  string `mapstructure:"project-id"`
	AuthURL    string `mapstructure:"auth-url"`
	Region     string
	DomainID   string `mapstructure:"domain-id"`
	DomainName string `mapstructure:"domain-name"`
	CAFile     string `mapstructure:"ca-file"`
}

// Octavia service related configuration
type octaviaConfig struct {
	// (Optional) Provider name for the load balancer. Default: octavia
	// For more information: https://docs.openstack.org/octavia/latest/admin/providers.html
	Provider string `mapstructure:"provider"`

	// (Required) Subnet ID to create the load balancer.
	SubnetID string `mapstructure:"subnet-id"`

	// (Optional) Public network ID to create floating IP.
	// If empty, no floating IP will be allocated to the load balancer vip.
	FloatingIPNetwork string `mapstructure:"floating-network-id"`

	// (Optional) If the ingress controller should manage the security groups attached to the cluster nodes.
	// Default is false.
	ManageSecurityGroups bool `mapstructure:"manage-security-groups"`
}

// ToAuthOptions gets openstack auth options
func (cfg Config) ToAuthOptions() gophercloud.AuthOptions {
	return gophercloud.AuthOptions{
		IdentityEndpoint: cfg.OpenStack.AuthURL,
		Username:         cfg.OpenStack.Username,
		UserID:           cfg.OpenStack.UserID,
		Password:         cfg.OpenStack.Password,
		TenantID:         cfg.OpenStack.ProjectID,
		DomainID:         cfg.OpenStack.DomainID,
		DomainName:       cfg.OpenStack.DomainName,
		AllowReauth:      true,
	}
}

func (cfg Config) ToV3AuthOptions() tokens.AuthOptions {
	return tokens.AuthOptions{
		IdentityEndpoint: cfg.OpenStack.AuthURL,
		Username:         cfg.OpenStack.Username,
		UserID:           cfg.OpenStack.UserID,
		Password:         cfg.OpenStack.Password,
		DomainID:         cfg.OpenStack.DomainID,
		DomainName:       cfg.OpenStack.DomainName,
		AllowReauth:      true,
	}
}
