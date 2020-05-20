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

package cloudprovider

import (
	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"

	"k8s.io/cloud-provider-openstack/pkg/autohealing/config"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/healthcheck"
)

var (
	providers = make(map[string]RegisterFunc)
)

// CloudProvider is an abstract, pluggable interface for cloud providers.
type CloudProvider interface {
	// GetName returns the cloud provider name.
	GetName() string

	// Update cluster health status.
	UpdateHealthStatus([]healthcheck.NodeInfo, []healthcheck.NodeInfo) error

	// Repair triggers the node repair process in the cloud.
	Repair([]healthcheck.NodeInfo) error

	// Enabled decides if the repair should be triggered.
	// It's recommended that the `Enabled()` function of the cloud provider doesn't allow to re-trigger when the repair
	// is in place, e.g. before the repair process is finished, `Enabled()` should return false so that we won't
	// re-trigger the repair process in the subsequent checks.
	// This function also provides the cluster admin the capability to disable the cluster auto healing on the fly.
	Enabled() bool
}

type RegisterFunc func(config config.Config, client kubernetes.Interface) (CloudProvider, error)

// RegisterCloudProvider registers a cloudprovider.Factory by name. This
// is expected to happen during app startup.
func RegisterCloudProvider(name string, register RegisterFunc) {
	if _, found := providers[name]; found {
		log.Fatalf("Cloud provider %s is already registered.", name)
	}

	log.Infof("Registered cloud provider %s.", name)
	providers[name] = register
}

// GetCloudProvider creates an instance of the named cloud provider
func GetCloudProvider(name string, config config.Config, client kubernetes.Interface) (CloudProvider, error) {
	f, found := providers[name]
	if !found {
		return nil, nil
	}
	return f(config, client)
}
