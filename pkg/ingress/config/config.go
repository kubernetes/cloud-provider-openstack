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
	Kubernetes kubeConfig
	OpenStack  osConfig
	Octavia    octaviaConfig
}

type kubeConfig struct {
	ApiserverHost string
	KubeConfig    string
}

type osConfig struct {
	Username  string
	Password  string
	ProjectID string
	AuthURL   string
	Region    string
}

type octaviaConfig struct {
	SubnetID           string
	NodeSubnetID       string
	AllocateFloatingIP bool
	FloatingIPNetwork  string
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
