/*
Copyright 2023 The Kubernetes Authors.

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
	"context"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/klog/v2"
)

// PVLabeler encapsulates an implementation of PVLabeler for OpenStack.
type PVLabeler struct {
	blockStorage     *gophercloud.ServiceClient
	sharedFileSystem *gophercloud.ServiceClient
}

// PVLabeler returns an implementation of PVLabeler for OpenStack.
func (os *OpenStack) PVLabeler() (cloudprovider.PVLabeler, bool) {
	return os.pvlabeler()
}

func (os *OpenStack) pvlabeler() (*PVLabeler, bool) {
	klog.V(4).Info("openstack.PVLabeler() called")

	blockStorage, err := client.NewBlockStorageV3(os.provider, os.epOpts)
	if err != nil {
		klog.Errorf("unable to access block storage v3 API : %v", err)
		return nil, false
	}

	sharedFileSystem, err := client.NewSharedFileSystemV2(os.provider, os.epOpts)
	if err != nil {
		klog.Errorf("unable to access share v2 API : %v", err)
		return nil, false
	}

	return &PVLabeler{
		blockStorage:     blockStorage,
		sharedFileSystem: sharedFileSystem,
	}, true
}

func getVolumeAZ(client *gophercloud.ServiceClient, volumeID string) (string, error) {
	volume, err := volumes.Get(client, volumeID).Extract()
	if err != nil {
		return "", err
	}
	return volume.AvailabilityZone, nil
}

func getShareAZ(client *gophercloud.ServiceClient, shareID string) (string, error) {
	share, err := shares.Get(client, shareID).Extract()
	if err != nil {
		return "", err
	}
	return share.AvailabilityZone, nil
}

func (p *PVLabeler) GetLabelsForVolume(ctx context.Context, pv *v1.PersistentVolume) (map[string]string, error) {
	labels := map[string]string{}

	switch pv.Spec.CSI.Driver {
	case "cinder.csi.openstack.org":
		volumeAZ, err := getVolumeAZ(p.blockStorage, pv.Spec.CSI.VolumeHandle)
		if err != nil {
			return nil, err
		}
		labels["topology.kubernetes.io/zone"] = volumeAZ
	case "manila.csi.openstack.org":
		shareAZ, err := getShareAZ(p.sharedFileSystem, pv.Spec.CSI.VolumeHandle)
		if err != nil {
			return nil, err
		}
		labels["topology.kubernetes.io/zone"] = shareAZ
	default:
		return labels, nil
	}

	return labels, nil
}
