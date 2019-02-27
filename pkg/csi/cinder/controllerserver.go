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
	"fmt"

	"github.com/golang/protobuf/ptypes"

	"github.com/container-storage-interface/spec/lib/go/csi"
	ossnapshots "github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/pborman/uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/klog"
	volumeutil "k8s.io/kubernetes/pkg/volume/util"
)

type controllerServer struct {
	Driver *CinderDriver
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

	var volAvailability string
	if req.GetAccessibilityRequirements() != nil {
		volAvailability = getAZFromTopology(req.GetAccessibilityRequirements())
	}

	if len(volAvailability) == 0 {
		// Volume Availability - Default is nova
		volAvailability = req.GetParameters()["availability"]
	}

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Verify a volume with the provided name doesn't already exist for this tenant
	volumes, err := cloud.GetVolumesByName(volName)
	if err != nil {
		klog.V(3).Infof("Failed to query for existing Volume during CreateVolume: %v", err)
	}

	resID := ""
	resAvailability := ""
	resSize := 0
	snapshotID := ""

	if len(volumes) == 1 {
		resID = volumes[0].ID
		resAvailability = volumes[0].AZ
		resSize = volumes[0].Size

		klog.V(4).Infof("Volume %s already exists in Availability Zone: %s of size %d GiB", resID, resAvailability, resSize)
	} else if len(volumes) > 1 {
		klog.V(3).Infof("found multiple existing volumes with selected name (%s) during create", volName)
		return nil, errors.New("multiple volumes reported by Cinder with same name")
	} else {
		// Volume Create
		properties := map[string]string{"cinder.csi.openstack.org/cluster": cs.Driver.cluster}
		content := req.GetVolumeContentSource()

		if content != nil && content.GetSnapshot() != nil {
			snapshotID = content.GetSnapshot().GetSnapshotId()
		}

		resID, resAvailability, resSize, err = cloud.CreateVolume(volName, volSizeGB, volType, volAvailability, snapshotID, &properties)
		if err != nil {
			klog.V(3).Infof("Failed to CreateVolume: %v", err)
			return nil, err
		}

		klog.V(4).Infof("Create volume %s in Availability Zone: %s of size %d GiB", resID, resAvailability, resSize)

	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      resID,
			CapacityBytes: int64(resSize * 1024 * 1024 * 1024),
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{topologyKey: resAvailability},
				},
			},
		},
	}

	if snapshotID != "" {
		src := &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: snapshotID,
				},
			},
		}
		resp.Volume.ContentSource = src
	}
	return resp, nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Volume Delete
	volID := req.GetVolumeId()
	err = cloud.DeleteVolume(volID)
	if err != nil {
		klog.V(3).Infof("Failed to DeleteVolume: %v", err)
		return nil, err
	}

	klog.V(4).Infof("Delete volume %s", volID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Volume Attach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	_, err = cloud.AttachVolume(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to AttachVolume: %v", err)
		return nil, err
	}

	err = cloud.WaitDiskAttached(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitDiskAttached: %v", err)
		return nil, err
	}

	devicePath, err := cloud.GetAttachmentDiskPath(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to GetAttachmentDiskPath: %v", err)
		return nil, err
	}

	klog.V(4).Infof("ControllerPublishVolume %s on %s", volumeID, instanceID)

	// Publish Volume Info
	pvInfo := map[string]string{}
	pvInfo["DevicePath"] = devicePath

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: pvInfo,
	}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {

	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	// Volume Detach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	err = cloud.DetachVolume(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to DetachVolume: %v", err)
		return nil, err
	}

	err = cloud.WaitDiskDetached(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitDiskDetached: %v", err)
		return nil, err
	}

	klog.V(4).Infof("ControllerUnpublishVolume %s on %s", volumeID, instanceID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	vlist, err := cloud.ListVolumes()
	if err != nil {
		klog.V(3).Infof("Failed to ListVolumes: %v", err)
		return nil, err
	}

	var ventries []*csi.ListVolumesResponse_Entry
	for _, v := range vlist {
		ventry := csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      v.ID,
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
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	name := req.Name
	volumeId := req.SourceVolumeId
	// No description from csi.CreateSnapshotRequest now
	description := ""

	// Verify a snapshot with the provided name doesn't already exist for this tenant
	snapshots, err := cloud.GetSnapshotByNameAndVolumeID(name, volumeId)
	if err != nil {
		klog.V(3).Infof("Failed to query for existing Snapshot during CreateSnapshot: %v", err)
	}
	var snap *ossnapshots.Snapshot

	if len(snapshots) == 1 {
		snap = &snapshots[0]

		klog.V(3).Infof("Found existing snapshot %s on %s", name, volumeId)
	} else if len(snapshots) > 1 {
		return nil, status.Errorf(codes.FailedPrecondition, "multiple snapshots reported by Cinder with same name: %v", err)
	} else {
		// TODO: Delegate the check to openstack itself and ignore the conflict
		snap, err = cloud.CreateSnapshot(name, volumeId, description, &req.Parameters)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to create snapshot %v", err)
		}

		klog.V(3).Infof("CreateSnapshot %s on %s", name, volumeId)
	}

	ctime, err := ptypes.TimestampProto(snap.CreatedAt)
	if err != nil {
		klog.Errorf("Error to convert time to timestamp: %v", err)
	}

	err = cloud.WaitSnapshotReady(snap.ID)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "Failed to WaitSnapshotReady: %v", err)
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     snap.ID,
			SizeBytes:      int64(snap.Size * 1024 * 1024 * 1024),
			SourceVolumeId: snap.VolumeID,
			CreationTime:   ctime,
			ReadyToUse:     true,
		},
	}, nil
}

func (cs *controllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	id := req.SnapshotId

	// Delegate the check to openstack itself
	err = cloud.DeleteSnapshot(id)
	if err != nil {
		klog.V(3).Infof("Faled to Delete snapshot: %v", err)
		return nil, err
	}
	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	// Get OpenStack Provider
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.V(3).Infof("Failed to GetOpenStackProvider: %v", err)
		return nil, err
	}

	filters := map[string]string{}
	// FIXME: honor the limit, offset and filters later
	vlist, err := cloud.ListSnapshots(int(req.MaxEntries), 0, filters)
	if err != nil {
		klog.V(3).Infof("Failed to ListSnapshots: %v", err)
		return nil, err
	}

	var ventries []*csi.ListSnapshotsResponse_Entry
	for _, v := range vlist {
		ctime, err := ptypes.TimestampProto(v.CreatedAt)
		if err != nil {
			klog.Errorf("Error to convert time to timestamp: %v", err)
		}
		ventry := csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      int64(v.Size * 1024 * 1024 * 1024),
				SnapshotId:     v.ID,
				SourceVolumeId: v.VolumeID,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}
		ventries = append(ventries, &ventry)
	}
	return &csi.ListSnapshotsResponse{
		Entries: ventries,
	}, nil

}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *controllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(5).Infof("Using default ControllerGetCapabilities")

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.Driver.cscap,
	}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func getAZFromTopology(requirement *csi.TopologyRequirement) string {
	for _, topology := range requirement.GetPreferred() {
		zone, exists := topology.GetSegments()[topologyKey]
		if exists {
			return zone
		}
	}

	for _, topology := range requirement.GetRequisite() {
		zone, exists := topology.GetSegments()[topologyKey]
		if exists {
			return zone
		}
	}
	return ""
}
