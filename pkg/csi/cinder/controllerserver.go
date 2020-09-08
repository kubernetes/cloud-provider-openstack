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
	"fmt"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	ossnapshots "github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

type controllerServer struct {
	Driver *CinderDriver
	Cloud  openstack.IOpenStack
}

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(4).Infof("CreateVolume: called with args %+v", *req)

	// Volume Name
	volName := req.GetName()
	volCapabilities := req.GetVolumeCapabilities()

	if len(volName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[CreateVolume] missing Volume Name")
	}

	if volCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "[CreateVolume] missing Volume capability")
	}

	// Volume Size - Default is 1 GiB
	volSizeBytes := int64(1 * 1024 * 1024 * 1024)
	if req.GetCapacityRange() != nil {
		volSizeBytes = int64(req.GetCapacityRange().GetRequiredBytes())
	}
	volSizeGB := int(util.RoundUpSize(volSizeBytes, 1024*1024*1024))

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

	cloud := cs.Cloud

	// Verify a volume with the provided name doesn't already exist for this tenant
	volumes, err := cloud.GetVolumesByName(volName)
	if err != nil {
		klog.V(3).Infof("Failed to query for existing Volume during CreateVolume: %v", err)
	}

	if len(volumes) == 1 {
		if volSizeGB != volumes[0].Size {
			return nil, status.Error(codes.AlreadyExists, "Volume Already exists with same name and different capacity")
		}

		klog.V(4).Infof("Volume %s already exists in Availability Zone: %s of size %d GiB", volumes[0].ID, volumes[0].AvailabilityZone, volumes[0].Size)
		return getCreateVolumeResponse(&volumes[0]), nil
	} else if len(volumes) > 1 {
		klog.V(3).Infof("found multiple existing volumes with selected name (%s) during create", volName)
		return nil, status.Error(codes.Internal, "Multiple volumes reported by Cinder with same name")

	}

	// Volume Create
	properties := map[string]string{"cinder.csi.openstack.org/cluster": cs.Driver.cluster}
	content := req.GetVolumeContentSource()
	var snapshotID string
	var sourcevolID string

	if content != nil && content.GetSnapshot() != nil {
		snapshotID = content.GetSnapshot().GetSnapshotId()
		_, err := cloud.GetSnapshotByID(snapshotID)
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				return nil, status.Errorf(codes.NotFound, "VolumeContentSource Snapshot %s not found", snapshotID)
			}
			return nil, status.Errorf(codes.Internal, "Failed to retrieve the snapshot %s: %v", snapshotID, err)
		}
	}

	if content != nil && content.GetVolume() != nil {
		sourcevolID = content.GetVolume().GetVolumeId()
		_, err := cloud.GetVolume(sourcevolID)
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				return nil, status.Errorf(codes.NotFound, "Source Volume %s not found", sourcevolID)
			}
			return nil, status.Errorf(codes.Internal, "Failed to retrieve the source volume %s: %v", sourcevolID, err)
		}
	}

	vol, err := cloud.CreateVolume(volName, volSizeGB, volType, volAvailability, snapshotID, sourcevolID, &properties)

	if err != nil {
		klog.Errorf("Failed to CreateVolume: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("CreateVolume failed with error %v", err))

	}

	klog.V(4).Infof("CreateVolume: Successfully created volume %s in Availability Zone: %s of size %d GiB", vol.ID, vol.AvailabilityZone, vol.Size)

	return getCreateVolumeResponse(vol), nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(4).Infof("DeleteVolume: called with args %+v", *req)

	// Volume Delete
	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "DeleteVolume Volume ID must be provided")
	}
	err := cs.Cloud.DeleteVolume(volID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("Volume %s is already deleted.", volID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		klog.Errorf("Failed to DeleteVolume: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("DeleteVolume failed with error %v", err))
	}

	klog.V(4).Infof("DeleteVolume: Successfully deleted volume %s", volID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {

	// Volume Attach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()
	volumeCapability := req.GetVolumeCapability()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Volume ID must be provided")
	}
	if len(instanceID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Instance ID must be provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Volume capability must be provided")
	}

	_, err := cs.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] Volume %s not found", volumeID)
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("[ControllerPublishVolume] get volume failed with error %v", err))
	}

	_, err = cs.Cloud.GetInstanceByID(instanceID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] Instance %s not found", instanceID)
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("[ControllerPublishVolume] GetInstanceByID failed with error %v", err))
	}

	_, err = cs.Cloud.AttachVolume(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to AttachVolume: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("[ControllerPublishVolume] Attach Volume failed with error %v", err))

	}

	err = cs.Cloud.WaitDiskAttached(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitDiskAttached: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("[ControllerPublishVolume] failed to attach volume: %v", err))
	}

	devicePath, err := cs.Cloud.GetAttachmentDiskPath(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to GetAttachmentDiskPath: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("[ControllerPublishVolume] failed to get device path of attached volume : %v", err))
	}

	klog.V(4).Infof("ControllerPublishVolume %s on %s is successful", volumeID, instanceID)

	// Publish Volume Info
	pvInfo := map[string]string{}
	pvInfo["DevicePath"] = devicePath

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: pvInfo,
	}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {

	// Volume Detach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerUnpublishVolume] Volume ID must be provided")
	}
	_, err := cs.Cloud.GetInstanceByID(instanceID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because node %s does not exist", volumeID, instanceID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("[ControllerUnpublishVolume] GetInstanceByID failed with error %v", err))
	}

	err = cs.Cloud.DetachVolume(instanceID, volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because it does not exist", volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		klog.V(3).Infof("Failed to DetachVolume: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("ControllerUnpublishVolume Detach Volume failed with error %v", err))
	}

	err = cs.Cloud.WaitDiskDetached(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitDiskDetached: %v", err)
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because it was deleted in the meanwhile", volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("ControllerUnpublishVolume failed with error %v", err))
	}

	klog.V(4).Infof("ControllerUnpublishVolume %s on %s", volumeID, instanceID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {

	if req.MaxEntries < 0 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf(
			"[ListVolumes] Invalid max entries request %v, must not be negative ", req.MaxEntries))
	}
	maxEntries := int(req.MaxEntries)

	vlist, nextPageToken, err := cs.Cloud.ListVolumes(maxEntries, req.StartingToken)
	if err != nil {
		klog.V(3).Infof("Failed to ListVolumes: %v", err)
		if cpoerrors.IsInvalidError(err) {
			return nil, status.Errorf(codes.Aborted, "[ListVolumes] Invalid request: %v", err)
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("ListVolumes failed with error %v", err))
	}

	var ventries []*csi.ListVolumesResponse_Entry
	for _, v := range vlist {
		ventry := csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      v.ID,
				CapacityBytes: int64(v.Size * 1024 * 1024 * 1024),
			},
		}

		status := &csi.ListVolumesResponse_VolumeStatus{}
		for _, attachment := range v.Attachments {
			status.PublishedNodeIds = append(status.PublishedNodeIds, attachment.ServerID)
		}
		ventry.Status = status

		ventries = append(ventries, &ventry)
	}
	return &csi.ListVolumesResponse{
		Entries:   ventries,
		NextToken: nextPageToken,
	}, nil
}

func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	name := req.Name
	volumeId := req.GetSourceVolumeId()

	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot name must be provided in CreateSnapshot request")
	}

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeID must be provided in CreateSnapshot request")
	}

	// Verify a snapshot with the provided name doesn't already exist for this tenant
	filters := map[string]string{}
	filters["Name"] = name
	snapshots, _, err := cs.Cloud.ListSnapshots(filters)
	if err != nil {
		klog.V(3).Infof("Failed to query for existing Snapshot during CreateSnapshot: %v", err)
	}
	var snap *ossnapshots.Snapshot

	if len(snapshots) == 1 {
		snap = &snapshots[0]

		if snap.VolumeID != volumeId {
			return nil, status.Error(codes.AlreadyExists, "Snapshot with given name already exists, with different source volume ID")
		}

		klog.V(3).Infof("Found existing snapshot %s on %s", name, volumeId)

	} else if len(snapshots) > 1 {
		klog.V(3).Infof("found multiple existing snapshots with selected name (%s) during create", name)
		return nil, status.Error(codes.Internal, "Multiple snapshots reported by Cinder with same name")

	} else {
		// TODO: Delegate the check to openstack itself and ignore the conflict
		snap, err = cs.Cloud.CreateSnapshot(name, volumeId, &req.Parameters)
		if err != nil {
			klog.V(3).Infof("Failed to Create snapshot: %v", err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("CreateSnapshot failed with error %v", err))
		}

		klog.V(3).Infof("CreateSnapshot %s on %s", name, volumeId)
	}

	ctime, err := ptypes.TimestampProto(snap.CreatedAt)
	if err != nil {
		klog.Errorf("Error to convert time to timestamp: %v", err)
	}

	err = cs.Cloud.WaitSnapshotReady(snap.ID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitSnapshotReady: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("CreateSnapshot failed with error %v", err))
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

	id := req.GetSnapshotId()

	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot ID must be provided in DeleteSnapshot request")
	}

	// Delegate the check to openstack itself
	err := cs.Cloud.DeleteSnapshot(id)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("Snapshot %s is already deleted.", id)
			return &csi.DeleteSnapshotResponse{}, nil
		}
		klog.V(3).Infof("Failed to Delete snapshot: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("DeleteSnapshot failed with error %v", err))
	}
	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {

	snapshotID := req.GetSnapshotId()
	if len(snapshotID) != 0 {
		snap, err := cs.Cloud.GetSnapshotByID(snapshotID)
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				klog.V(3).Infof("Snapshot %s not found", snapshotID)
				return &csi.ListSnapshotsResponse{}, nil
			}
			return nil, status.Errorf(codes.Internal, "Failed to GetSnapshot %s : %v", snapshotID, err)
		}

		ctime, err := ptypes.TimestampProto(snap.CreatedAt)

		entry := &csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      int64(snap.Size * 1024 * 1024 * 1024),
				SnapshotId:     snap.ID,
				SourceVolumeId: snap.VolumeID,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}

		entries := []*csi.ListSnapshotsResponse_Entry{entry}
		return &csi.ListSnapshotsResponse{
			Entries: entries,
		}, err

	}

	filters := map[string]string{}

	var slist []snapshots.Snapshot
	var err error
	var nextPageToken string

	// Add the filters
	if len(req.GetSourceVolumeId()) != 0 {
		filters["VolumeID"] = req.GetSourceVolumeId()
	} else {
		filters["Limit"] = strconv.Itoa(int(req.MaxEntries))
		filters["Marker"] = req.StartingToken
	}

	// Only retrieve snapshots that are available
	filters["Status"] = "available"
	slist, nextPageToken, err = cs.Cloud.ListSnapshots(filters)
	if err != nil {
		klog.V(3).Infof("Failed to ListSnapshots: %v", err)
		return nil, status.Errorf(codes.Internal, "ListSnapshots failed with error %v", err)
	}

	var sentries []*csi.ListSnapshotsResponse_Entry
	for _, v := range slist {
		ctime, err := ptypes.TimestampProto(v.CreatedAt)
		if err != nil {
			klog.Errorf("Error to convert time to timestamp: %v", err)
		}
		sentry := csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      int64(v.Size * 1024 * 1024 * 1024),
				SnapshotId:     v.ID,
				SourceVolumeId: v.VolumeID,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}
		sentries = append(sentries, &sentry)
	}
	return &csi.ListSnapshotsResponse{
		Entries:   sentries,
		NextToken: nextPageToken,
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

	reqVolCap := req.GetVolumeCapabilities()

	if reqVolCap == nil || len(reqVolCap) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume Capabilities must be provided")
	}
	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume ID must be provided")
	}

	_, err := cs.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, fmt.Sprintf("ValidateVolumeCapabiltites Volume %s not found", volumeID))
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("ValidateVolumeCapabiltites %v", err))
	}

	for _, cap := range reqVolCap {
		if cap.GetAccessMode().GetMode() != cs.Driver.vcap[0].Mode {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "Requested Volume Capabilty not supported"}, nil
		}
	}

	// Cinder CSI driver currently supports one mode only
	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: cs.Driver.vcap[0],
				},
			},
		},
	}

	return resp, nil
}

func (cs *controllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("GetCapacity is not yet implemented"))
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(4).Infof("ControllerExpandVolume: called with args %+v", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	cap := req.GetCapacityRange()
	if cap == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range not provided")
	}

	volSizeBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	volSizeGB := int(util.RoundUpSize(volSizeBytes, 1024*1024*1024))
	maxVolSize := cap.GetLimitBytes()

	if maxVolSize > 0 && maxVolSize < volSizeBytes {
		return nil, status.Error(codes.OutOfRange, "After round-up, volume size exceeds the limit specified")
	}

	_, err := cs.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("GetVolume failed with error %v", err))
	}

	err = cs.Cloud.ExpandVolume(volumeID, volSizeGB)
	if err != nil {
		return nil, status.Errorf(codes.Internal, fmt.Sprintf("Could not resize volume %q to size %v: %v", volumeID, volSizeGB, err))
	}

	klog.V(4).Infof("ControllerExpandVolume resized volume %v to size %v", volumeID, volSizeGB)

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         volSizeBytes,
		NodeExpansionRequired: true,
	}, nil
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

func getCreateVolumeResponse(vol *volumes.Volume) *csi.CreateVolumeResponse {

	var volsrc *csi.VolumeContentSource

	if vol.SnapshotID != "" {
		volsrc = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: vol.SnapshotID,
				},
			},
		}
	}

	if vol.SourceVolID != "" {
		volsrc = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Volume{
				Volume: &csi.VolumeContentSource_VolumeSource{
					VolumeId: vol.SourceVolID,
				},
			},
		}
	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      vol.ID,
			CapacityBytes: int64(vol.Size * 1024 * 1024 * 1024),
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{topologyKey: vol.AvailabilityZone},
				},
			},
			ContentSource: volsrc,
		},
	}

	return resp

}
