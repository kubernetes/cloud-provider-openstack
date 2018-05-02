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
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
)

type Server struct {
	// Unique identifier for the volume.
	ID string
	// Human-readable display name for the volume.
	Name string
	// Current status of the volume.
	Status string
}

func (os *OpenStack) GetInstance(instanceID string) (Server, error) {
	inst, err := servers.Get(os.compute, instanceID).Extract()
	if err != nil {
		return Server{}, err
	}

	server := Server{
		ID:     inst.ID,
		Name:   inst.Name,
		Status: inst.Status,
	}

	return server, nil
}
