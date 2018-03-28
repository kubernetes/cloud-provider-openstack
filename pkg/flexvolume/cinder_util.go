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

package flexvolume

import (
	"fmt"

	utilnode "k8s.io/cloud-provider-openstack/pkg/flexvolume/node"
)

// FlexManager is a wrapper of CinderBaremetalUtil
type FlexManager struct {
	cinderBaremetalUtil *CinderBaremetalUtil
}

func NewFlexManager(cinderConfigFile string) (*FlexManager, error) {
	result := &FlexManager{}

	cinderClient, err := newCinderClient(cinderConfigFile)
	if err != nil {
		return nil, fmt.Errorf("Init cinder client failed: %v", err)
	} else {
		result.cinderBaremetalUtil = &CinderBaremetalUtil{
			client:   cinderClient,
			hostname: utilnode.GetHostname(""),
		}
	}

	return result, nil
}

func (m *FlexManager) AttachDisk(d *FlexVolumeDriver, targetMountDir string) error {
	return m.cinderBaremetalUtil.AttachDiskBaremetal(d, targetMountDir)
}

func (m *FlexManager) DetachDisk(d *FlexVolumeDriver) error {
	return m.cinderBaremetalUtil.DetachDiskBaremetal(d)
}
