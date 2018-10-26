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

package cinder

import (
	"errors"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/golang/glog"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	"github.com/pborman/uuid"
	"golang.org/x/net/context"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	volumeutil "k8s.io/kubernetes/pkg/volume/util"
)

type controllerServer struct {
	*csicommon.DefaultControllerServer
}

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	// Volume Name
	volName := req.GetName()
	if len(volName) == 0 {
		volName = uuid.NewUUID().String()
	}

	// Volume Size - Default is 1 GiB
	volSizeBytes := int64(1 * 1024 * 1024 * 1024)
	if req.GetCapacityRange() != nil {
		volSizeBytes = int64(req.GetCapacityRange().GetRequiredBytes())
	}
	volSizeGB := int(volumeutil.RoundUpSize(volSizeBytes, 1024*1024*1024))

	// Volume Type
	volType := req.GetParameters()["type"]

	// Volume Availability - Default is nova
	volAvailability := req.GetParameters()["availability"]

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		glog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Verify a volume with the provided name doesn't already exist for this tenant
	volumes, err := cloud.GetVolumesByName(volName)
	if err != nil {
		glog.V(3).Infof("Failed to query for existing Volume during CreateVolume: %v", err)
	}

	resID := ""
	resAvailability := ""
	resSize := 0

	if len(volumes) == 1 {
		resID = volumes[0].ID
		resAvailability = volumes[0].AZ
	} else if len(volumes) > 1 {
		glog.V(3).Infof("found multiple existing volumes with selected name (%s) during create", volName)
		return nil, errors.New("multiple volumes reported by Cinder with same name")
	} else {
		// Volume Create
		resID, resAvailability, resSize, err = cloud.CreateVolume(volName, volSizeGB, volType, volAvailability, nil)
		if err != nil {
			glog.V(3).Infof("Failed to CreateVolume: %v", err)
			return nil, err
		}

		glog.V(4).Infof("Create volume %s in Availability Zone: %s of size %d GiB", resID, resAvailability, resSize)

	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			Id:            resID,
			CapacityBytes: int64(resSize * 1024 * 1024 * 1024),
			Attributes: map[string]string{
				"availability": resAvailability,
			},
		},
	}, nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		glog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Volume Delete
	volID := req.GetVolumeId()
	err = cloud.DeleteVolume(volID)
	if err != nil {
		glog.V(3).Infof("Failed to DeleteVolume: %v", err)
		return nil, err
	}

	glog.V(4).Infof("Delete volume %s", volID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		glog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Volume Attach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	_, err = cloud.AttachVolume(instanceID, volumeID)
	if err != nil {
		glog.V(3).Infof("Failed to AttachVolume: %v", err)
		return nil, err
	}

	err = cloud.WaitDiskAttached(instanceID, volumeID)
	if err != nil {
		glog.V(3).Infof("Failed to WaitDiskAttached: %v", err)
		return nil, err
	}

	devicePath, err := cloud.GetAttachmentDiskPath(instanceID, volumeID)
	if err != nil {
		glog.V(3).Infof("Failed to GetAttachmentDiskPath: %v", err)
		return nil, err
	}

	glog.V(4).Infof("ControllerPublishVolume %s on %s", volumeID, instanceID)

	// Publish Volume Info
	pvInfo := map[string]string{}
	pvInfo["DevicePath"] = devicePath

	return &csi.ControllerPublishVolumeResponse{
		PublishInfo: pvInfo,
	}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		glog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Volume Detach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	err = cloud.DetachVolume(instanceID, volumeID)
	if err != nil {
		glog.V(3).Infof("Failed to DetachVolume: %v", err)
		return nil, err
	}

	err = cloud.WaitDiskDetached(instanceID, volumeID)
	if err != nil {
		glog.V(3).Infof("Failed to WaitDiskDetached: %v", err)
		return nil, err
	}

	glog.V(4).Infof("ControllerUnpublishVolume %s on %s", volumeID, instanceID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		glog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	vlist, err := cloud.ListVolumes()
	if err != nil {
		glog.V(3).Infof("Failed to ListVolumes: %v", err)
		return nil, err
	}

	var ventries []*csi.ListVolumesResponse_Entry
	for _, v := range vlist {
		ventry := csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				Id:            v.ID,
				CapacityBytes: int64(v.Size * 1024 * 1024 * 1024),
			},
		}
		ventries = append(ventries, &ventry)
	}
	return &csi.ListVolumesResponse{
		Entries: ventries,
	}, nil
}

func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		glog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	name := req.Name
	volumeId := req.SourceVolumeId
	// No description from csi.CreateSnapshotRequest now
	description := ""

	// Delegate the check to openstack itself
	snap, err := cloud.CreateSnapshot(name, volumeId, description, &req.Parameters)
	if err != nil {
		glog.V(3).Infof("Faled to Create snapshot: %v", err)
		return nil, err
	}

	glog.V(4).Infof("CreateSnapshot %s on %s", name, volumeId)
	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			Id:             snap.ID,
			SizeBytes:      int64(snap.Size * 1024 * 1024 * 1024),
			SourceVolumeId: snap.VolumeID,
			CreatedAt:      int64(snap.CreatedAt.UnixNano() / int64(time.Millisecond)),
		},
	}, nil

}
