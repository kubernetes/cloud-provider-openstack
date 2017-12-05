/*
Copyright 2017 The Kubernetes Authors.

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
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
)

type OpenStack struct {
	compute      *gophercloud.ServiceClient
	blockstorage *gophercloud.ServiceClient
}

var osinstance *OpenStack = nil

func GetOpenStackProvider() (*OpenStack, error) {

	if osinstance == nil {
		// Get config from env
		opts, err := openstack.AuthOptionsFromEnv()
		if err != nil {
			return nil, err
		}

		// Authenticate Client
		provider, err := openstack.AuthenticatedClient(opts)
		if err != nil {
			return nil, err
		}

		region := os.Getenv("OS_REGION_NAME")

		// Init Nova ServiceClient
		computeclient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
			Region: region,
		})
		if err != nil {
			return nil, err
		}

		// Init Cinder ServiceClient
		blockstorageclient, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
			Region: region,
		})
		if err != nil {
			return nil, err
		}

		// Init OpenStack
		osinstance = &OpenStack{
			compute:      computeclient,
			blockstorage: blockstorageclient,
		}
	}

	return osinstance, nil
}
