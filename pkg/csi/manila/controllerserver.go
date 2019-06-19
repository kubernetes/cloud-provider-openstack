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

package manila

import (
	"context"
	"fmt"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/snapshots"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/responsebroker"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
	"k8s.io/klog"
)

type controllerServer struct {
	d *Driver
}

var (
	rbVolumeID   = responsebroker.New()
	rbSnapshotID = responsebroker.New()
)

func getVolumeCreator(source *csi.VolumeContentSource) (volumeCreator, error) {
	if source == nil {
		return &blankVolume{}, nil
	}

	if source.GetVolume() != nil {
		return nil, status.Error(codes.Unimplemented, "volume cloning is not supported yet")
	}

	if source.GetSnapshot() != nil {
		return &volumeFromSnapshot{}, nil
	}

	return nil, status.Error(codes.InvalidArgument, "invalid volume content source")
}

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if err := validateCreateVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Configuration

	params := req.GetParameters()
	if params == nil {
		params = make(map[string]string)
	}

	params["protocol"] = cs.d.shareProto

	shareOpts, err := options.NewControllerVolumeContext(params)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid volume parameters: %v", err)
	}

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid OpenStack secrets: %v", err)
	}

	volID := newVolumeID(req.GetName())

	// Acquire a lock for this volume
	handle, isNewRequest := rbVolumeID.Acquire(string(volID))
	if !isNewRequest {
		if respData := readResponse(handle); respData != nil {
			return respData.(*csi.CreateVolumeResponse), nil
		}
	}

	var (
		res = &requestResult{}

		manilaClient *gophercloud.ServiceClient
		volCreator   volumeCreator
		share        *shares.Share
		accessRight  *shares.AccessRight
	)

	defer writeResponse(handle, rbVolumeID, string(volID), res)

	manilaClient, res.err = newManilaV2Client(osOpts)
	if res.err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", res.err)
	}

	requestedSize := req.GetCapacityRange().GetRequiredBytes()
	if requestedSize == 0 {
		// At least 1GiB
		requestedSize = 1 * bytesInGiB
	}

	sizeInGiB := bytesToGiB(requestedSize)

	// Create or get the share

	if volCreator, res.err = getVolumeCreator(req.GetVolumeContentSource()); res.err != nil {
		return nil, res.err
	}

	if share, res.err = volCreator.create(req, volID, sizeInGiB, manilaClient, shareOpts); res.err != nil {
		return nil, res.err
	}

	if res.err = verifyVolumeCompatibility(sizeInGiB, share, shareOpts); res.err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "volume %s already exists, but is incompatible with the request: %v", volID, res.err)
	}

	// Grant access to the share

	ad := getShareAdapter(shareOpts.Protocol)

	klog.V(4).Infof("creating an access rule for volume %s (share ID %s)", volID, share.ID)

	accessRight, res.err = ad.GetOrGrantAccess(&shareadapters.GrantAccessArgs{Share: share, ManilaClient: manilaClient, Options: shareOpts})
	if res.err != nil {
		if res.err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for access rights for volume %s to become available", volID)
		}

		return nil, status.Errorf(codes.Internal, "failed to grant access for volume %s (share ID %s): %v", volID, share.ID, res.err)
	}

	klog.V(4).Infof("successfully created volume %s (share ID %s)", volID, share.ID)

	res.dataPtr = &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      string(volID),
			ContentSource: req.GetVolumeContentSource(),
			CapacityBytes: int64(sizeInGiB) * bytesInGiB,
			VolumeContext: map[string]string{
				"shareID":        share.ID,
				"shareAccessID":  accessRight.ID,
				"cephfs-mounter": shareOpts.CephfsMounter,
			},
		},
	}

	return res.dataPtr.(*csi.CreateVolumeResponse), nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if err := validateDeleteVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid OpenStack secrets: %v", err)
	}

	manilaClient, err := newManilaV2Client(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	volID := volumeID(req.VolumeId)

	if err := deleteShare(volID, manilaClient); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete volume %s: %v", volID, err)
	}

	klog.V(4).Infof("successfully deleted volume %s", volID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	if cs.d.shareProto == "CEPHFS" {
		// Restoring shares from CephFS snapshots needs special handling that's not implemented yet.
		// TODO: Creating CephFS snapshots is forbidden until CephFS restoration is in place.
		return nil, status.Errorf(codes.InvalidArgument, "the driver doesn't support snapshotting CephFS shares yet")
	}

	if err := validateCreateSnapshotRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Configuration

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid OpenStack secrets: %v", err)
	}

	snapID := newSnapshotID(req.GetName())

	// Acquire a lock for this snapshot
	handle, isNewRequest := rbSnapshotID.Acquire(string(snapID))
	if !isNewRequest {
		if respData := readResponse(handle); respData != nil {
			return respData.(*csi.CreateSnapshotResponse), nil
		}
	}

	var (
		res = &requestResult{}

		manilaClient *gophercloud.ServiceClient
		sourceShare  *shares.Share
		snapshot     *snapshots.Snapshot
		ctime        *timestamp.Timestamp
	)

	defer writeResponse(handle, rbSnapshotID, string(snapID), res)

	manilaClient, res.err = newManilaV2Client(osOpts)
	if res.err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", res.err)
	}

	// Retrieve the source share

	if sourceShare, res.err = getShareByName(req.GetSourceVolumeId(), manilaClient); res.err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve source volume %s: %v", req.GetSourceVolumeId(), res.err)
	}

	if strings.ToUpper(sourceShare.ShareProto) != cs.d.shareProto {
		return nil, status.Errorf(codes.InvalidArgument, "share protocol mismatch: requested a snapshot of %s volume %s (share ID %s), but share protocol selector is set to %s",
			sourceShare.ShareProto, req.GetSourceVolumeId(), sourceShare.ID, cs.d.shareProto)
	}

	// Create or get the snapshot

	klog.V(4).Infof("creating snapshot %s of volume %s", snapID, req.GetSourceVolumeId())

	if snapshot, res.err = getOrCreateSnapshot(snapID, sourceShare.ID, manilaClient); res.err != nil {
		if res.err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for snapshot %s to become available", snapID)
		}

		return nil, status.Errorf(codes.Internal, "failed to create snapshot %s for volume %s: %v", snapID, req.GetSourceVolumeId(), res.err)
	}

	// Check for snapshot status, determine whether it's ready

	var readyToUse bool

	switch snapshot.Status {
	case snapshotCreating:
		readyToUse = false
	case snapshotAvailable:
		readyToUse = true
	default:
		res.err = fmt.Errorf("snapshot %s is in %s state", snapshot.ID, snapshot.Status)
		return nil, status.Errorf(codes.Internal, "an error occurred while creating snapshot %s: %v", snapID, res.err)
	}

	// Parse CreatedAt timestamp
	if ctime, err = ptypes.TimestampProto(snapshot.CreatedAt); err != nil {
		klog.Warningf("couldn't parse timestamp %v from snapshot %s (snapshot ID %s): %v", snapshot.CreatedAt, snapID, snapshot.ID, err)
	}

	res.dataPtr = &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     string(snapID),
			SourceVolumeId: req.GetSourceVolumeId(),
			SizeBytes:      int64(sourceShare.Size) * bytesInGiB,
			CreationTime:   ctime,
			ReadyToUse:     readyToUse,
		},
	}

	return res.dataPtr.(*csi.CreateSnapshotResponse), nil
}

func (cs *controllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	if cs.d.shareProto == "CEPHFS" {
		// Restoring shares from CephFS snapshots needs special handling that's not implemented yet.
		// TODO: Deleting CephFS snapshots is forbidden until CephFS restoration is in place.
		return nil, status.Errorf(codes.InvalidArgument, "the driver doesn't support CephFS snapshots yet")
	}

	if err := validateDeleteSnapshotRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid OpenStack secrets: %v", err)
	}

	manilaClient, err := newManilaV2Client(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	snapID := snapshotID(req.GetSnapshotId())

	if err := deleteSnapshot(snapID, manilaClient); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete snapshot %s: %v", snapID, err)
	}

	klog.V(4).Infof("successfully deleted snapshot %s", snapID)

	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.d.cscaps,
	}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ListVolumes(context.Context, *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) GetCapacity(context.Context, *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
