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

package provisioner

import (
	"github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/controller"
	"k8s.io/api/core/v1"
	"k8s.io/cloud-provider-openstack/pkg/volume/cinder/volumeservice"
)

const fcType = "fc"

type fcMapper struct {
	volumeMapper
}

func (m *fcMapper) BuildPVSource(conn volumeservice.VolumeConnection, options controller.VolumeOptions) (*v1.PersistentVolumeSource, error) {
	ret := &v1.PersistentVolumeSource{
		FC: &v1.FCVolumeSource{
			TargetWWNs: conn.Data.TargetWWNs,
			Lun:        &(conn.Data.TargetLun),
		},
	}
	return ret, nil
}

func (m *fcMapper) AuthSetup(p *cinderProvisioner, options controller.VolumeOptions, conn volumeservice.VolumeConnection) error {
	return nil
}

func (m *fcMapper) AuthTeardown(p *cinderProvisioner, pv *v1.PersistentVolume) error {
	return nil
}
