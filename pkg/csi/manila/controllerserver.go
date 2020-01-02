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
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/snapshots"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/capabilities"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/responsebroker"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
	clouderrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog"
)

type controllerServer struct {
	d *Driver
}

var (
	rbVolumeID   = responsebroker.New()
	rbSnapshotID = responsebroker.New()
)

func getVolumeCreator(source *csi.VolumeContentSource, shareOpts *options.ControllerVolumeContext, compatOpts *options.CompatibilityOptions, shareTypeCaps capabilities.ManilaCapabilities) (volumeCreator, error) {
	if source == nil {
		return &blankVolume{}, nil
	}

	if source.GetVolume() != nil {
		return nil, status.Error(codes.Unimplemented, "volume cloning is not supported yet")
	}

	if source.GetSnapshot() != nil {
		if tryCompatForVolumeSource(compatOpts, shareOpts, source, shareTypeCaps) != nil {
			klog.Infof("share type %s does not advertise create_share_from_snapshot_support capability, compatibility mode is available", shareOpts.Type)
			return &blankVolume{}, nil
		}

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

	// Acquire a lock for this volume
	handle, isNewRequest := rbVolumeID.Acquire(req.GetName())
	if !isNewRequest {
		if respData := readResponse(handle); respData != nil {
			return respData.(*csi.CreateVolumeResponse), nil
		}
	}

	var (
		res = &requestResult{}

		manilaClient  manilaclient.Interface
		volCreator    volumeCreator
		share         *shares.Share
		accessRight   *shares.AccessRight
		shareTypeCaps capabilities.ManilaCapabilities
	)

	defer writeResponse(handle, rbVolumeID, req.GetName(), res)

	manilaClient, res.err = cs.d.manilaClientBuilder.New(osOpts)
	if res.err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", res.err)
	}

	if shareTypeCaps, res.err = capabilities.GetManilaCapabilities(shareOpts.Type, manilaClient); res.err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get Manila capabilities for share type %s: %v", shareOpts.Type, res.err)
	}

	requestedSize := req.GetCapacityRange().GetRequiredBytes()
	if requestedSize == 0 {
		// At least 1GiB
		requestedSize = 1 * bytesInGiB
	}

	sizeInGiB := bytesToGiB(requestedSize)

	// Retrieve an existing share or create a new one

	if volCreator, res.err = getVolumeCreator(req.GetVolumeContentSource(), shareOpts, cs.d.compatOpts, shareTypeCaps); res.err != nil {
		return nil, res.err
	}

	if share, res.err = volCreator.create(req, req.GetName(), sizeInGiB, manilaClient, shareOpts); res.err != nil {
		return nil, res.err
	}

	if res.err = verifyVolumeCompatibility(sizeInGiB, req, share, shareOpts, cs.d.compatOpts, shareTypeCaps); res.err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "a share named %s already exists, but is incompatible with the request: %v", req.GetName(), res.err)
	}

	// Grant access to the share

	ad := getShareAdapter(shareOpts.Protocol)

	klog.V(4).Infof("creating an access rule for share %s", share.ID)

	accessRight, res.err = ad.GetOrGrantAccess(&shareadapters.GrantAccessArgs{Share: share, ManilaClient: manilaClient, Options: shareOpts})
	if res.err != nil {
		if res.err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for access rights %s for share %s to become available", accessRight.ID, share.ID)
		}

		return nil, status.Errorf(codes.Internal, "failed to grant access for share %s: %v", share.ID, res.err)
	}

	// Check if compatibility layer is needed and can be used
	if compatLayer := tryCompatForVolumeSource(cs.d.compatOpts, shareOpts, req.GetVolumeContentSource(), shareTypeCaps); compatLayer != nil {
		if res.err = compatLayer.SupplementCapability(cs.d.compatOpts, share, accessRight, req, cs.d.fwdEndpoint, manilaClient, cs.d.csiClientBuilder); res.err != nil {
			// An error occurred, the user must clean the share manually
			// TODO needs proper monitoring
			return nil, res.err
		}
	}

	res.dataPtr = &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      share.ID,
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

	manilaClient, err := cs.d.manilaClientBuilder.New(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	if err := deleteShare(req.GetVolumeId(), manilaClient); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete share %s: %v", req.GetVolumeId(), err)
	}

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

	// Acquire a lock for this snapshot
	handle, isNewRequest := rbSnapshotID.Acquire(req.GetName())
	if !isNewRequest {
		if respData := readResponse(handle); respData != nil {
			return respData.(*csi.CreateSnapshotResponse), nil
		}
	}

	var (
		res = &requestResult{}

		manilaClient manilaclient.Interface
		sourceShare  *shares.Share
		snapshot     *snapshots.Snapshot
		ctime        *timestamp.Timestamp
	)

	defer writeResponse(handle, rbSnapshotID, req.GetName(), res)

	manilaClient, res.err = cs.d.manilaClientBuilder.New(osOpts)
	if res.err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", res.err)
	}

	// Retrieve the source share

	if sourceShare, res.err = manilaClient.GetShareByID(req.GetSourceVolumeId()); res.err != nil {
		if clouderrors.IsNotFound(res.err) {
			return nil, status.Errorf(codes.NotFound, "failed to create a snapshot (%s) for share %s because the share doesn't exist: %v", req.GetName(), req.GetSourceVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve source share %s when creating a snapshot (%s): %v", req.GetSourceVolumeId(), req.GetName(), res.err)
	}

	if strings.ToUpper(sourceShare.ShareProto) != cs.d.shareProto {
		return nil, status.Errorf(codes.InvalidArgument, "share protocol mismatch: requested a snapshot of %s share %s, but share protocol selector is set to %s",
			sourceShare.ShareProto, req.GetSourceVolumeId(), cs.d.shareProto)
	}

	// Retrieve an existing snapshot or create a new one

	klog.V(4).Infof("creating a snapshot (%s) of share %s", req.GetName(), req.GetSourceVolumeId())

	if snapshot, res.err = getOrCreateSnapshot(req.GetName(), sourceShare.ID, manilaClient); res.err != nil {
		if res.err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for snapshot %s of share %s to become available", snapshot.ID, req.GetSourceVolumeId())
		}

		if clouderrors.IsNotFound(res.err) {
			return nil, status.Errorf(codes.NotFound, "failed to create a snapshot (%s) for share %s because the share doesn't exist: %v", req.GetName(), req.GetSourceVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to create a snapshot (%s) of share %s: %v", req.GetName(), req.GetSourceVolumeId(), res.err)
	}

	if res.err = verifySnapshotCompatibility(snapshot, req); res.err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "a snapshot named %s already exists, but is incompatible with the request: %v", req.GetName(), res.err)
	}

	// Check for snapshot status, determine whether it's ready

	var readyToUse bool

	switch snapshot.Status {
	case snapshotCreating:
		readyToUse = false
	case snapshotAvailable:
		readyToUse = true
	case snapshotError:
		manilaErrMsg, err := lastResourceError(snapshot.ID, manilaClient)
		if err != nil {
			res.err = fmt.Errorf("snapshot %s of share %s is in error state, error description could not be retrieved: %v", snapshot.ID, req.GetSourceVolumeId(), err)
			return nil, status.Error(codes.Internal, res.err.Error())
		}

		// An error occurred, try to roll-back the snapshot
		tryDeleteSnapshot(snapshot, manilaClient)

		res.err = fmt.Errorf("snapshot %s of share %s is in error state: %s", snapshot.ID, req.GetSourceVolumeId(), manilaErrMsg.message)
		return nil, status.Error(manilaErrMsg.errCode.toRpcErrorCode(), res.err.Error())
	default:
		res.err = fmt.Errorf("snapshot %s is in an unexpected state: wanted creating/available, got %s", snapshot.ID, snapshot.Status)
		return nil, status.Errorf(codes.Internal, "an error occurred while creating a snapshot (%s) of share %s: %v", req.GetName(), req.GetSourceVolumeId(), res.err)
	}

	// Parse CreatedAt timestamp
	if ctime, err = ptypes.TimestampProto(snapshot.CreatedAt); err != nil {
		klog.Warningf("couldn't parse timestamp %v from snapshot %s: %v", snapshot.CreatedAt, snapshot.ID, err)
	}

	res.dataPtr = &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     snapshot.ID,
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

	manilaClient, err := cs.d.manilaClientBuilder.New(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	if err := deleteSnapshot(req.GetSnapshotId(), manilaClient); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete snapshot %s: %v", req.GetSnapshotId(), err)
	}

	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.d.cscaps,
	}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if err := validateValidateVolumeCapabilitiesRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid OpenStack secrets: %v", err)
	}

	for _, volCap := range req.GetVolumeCapabilities() {
		if volCap.GetBlock() != nil {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "block access type is not allowed"}, nil
		}

		if volCap.GetMount() == nil {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "volume must be accessible via filesystem API"}, nil
		}

		if volCap.GetAccessMode().GetMode() == csi.VolumeCapability_AccessMode_UNKNOWN {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "unknown volume access mode"}, nil
		}
	}

	manilaClient, err := cs.d.manilaClientBuilder.New(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	share, err := manilaClient.GetShareByID(req.GetVolumeId())
	if err != nil {
		if clouderrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "share %s not found: %v", req.GetVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve share %s: %v", req.GetVolumeId(), err)
	}

	if share.Status != shareAvailable {
		if share.Status == shareCreating {
			return nil, status.Errorf(codes.Unavailable, "share %s is in transient creating state", share.ID)
		}

		return nil, status.Errorf(codes.FailedPrecondition, "share %s is in an unexpected state: wanted %s, got %s", share.ID, shareAvailable, share.Status)
	}

	if !compareProtocol(share.ShareProto, cs.d.shareProto) {
		return nil, status.Errorf(codes.InvalidArgument, "share protocol mismatch: wanted %s, got %s", cs.d.shareProto, share.ShareProto)
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.GetVolumeContext(),
			VolumeCapabilities: req.GetVolumeCapabilities(),
			Parameters:         req.GetParameters(),
		},
	}, nil
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
