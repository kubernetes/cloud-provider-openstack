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

package shareoptions

import (
	"k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions/validator"
)

var (
	osOptionsValidator = validator.New(&openstack_provider.AuthOpts{})
)

// NewOpenStackOptionsFromSecret reads k8s secrets, validates and populates OpenStackOptions
func NewOpenStackOptionsFromSecret(c clientset.Interface, secretRef *v1.SecretReference) (*openstack_provider.AuthOpts, error) {
	params, err := readSecrets(c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewOpenStackOptionsFromMap(params)
}

// NewOpenStackOptionsFromMap validates and populates OpenStackOptions
func NewOpenStackOptionsFromMap(params map[string]string) (*openstack_provider.AuthOpts, error) {
	opts := &openstack_provider.AuthOpts{}
	return opts, osOptionsValidator.Populate(params, opts)
}
