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
	"strings"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/capabilities"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
	clouderrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

type controllerServer struct {
	d *Driver
}

var (
	pendingVolumes   = sync.Map{}
	pendingSnapshots = sync.Map{}
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

	// Check for pending CreateVolume for this volume name
	if _, isPending := pendingVolumes.LoadOrStore(req.GetName(), true); isPending {
		return nil, status.Errorf(codes.Aborted, "a volume named %s is already being created", req.GetName())
	}
	defer pendingVolumes.Delete(req.GetName())

	manilaClient, err := cs.d.manilaClientBuilder.New(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	shareTypeCaps, err := capabilities.GetManilaCapabilities(shareOpts.Type, manilaClient)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get Manila capabilities for share type %s: %v", shareOpts.Type, err)
	}

	requestedSize := req.GetCapacityRange().GetRequiredBytes()
	if requestedSize == 0 {
		// At least 1GiB
		requestedSize = 1 * bytesInGiB
	}

	sizeInGiB := bytesToGiB(requestedSize)

	// Retrieve an existing share or create a new one

	volCreator, err := getVolumeCreator(req.GetVolumeContentSource(), shareOpts, cs.d.compatOpts, shareTypeCaps)
	if err != nil {
		return nil, err
	}

	share, err := volCreator.create(req, req.GetName(), sizeInGiB, manilaClient, shareOpts)
	if err != nil {
		return nil, err
	}

	if err = verifyVolumeCompatibility(sizeInGiB, req, share, shareOpts, cs.d.compatOpts, shareTypeCaps); err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "a share named %s already exists, but is incompatible with the request: %v", req.GetName(), err)
	}

	// Grant access to the share

	ad := getShareAdapter(shareOpts.Protocol)

	klog.V(4).Infof("creating an access rule for share %s", share.ID)

	accessRight, err := ad.GetOrGrantAccess(&shareadapters.GrantAccessArgs{Share: share, ManilaClient: manilaClient, Options: shareOpts})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for access rights %s for share %s to become available", accessRight.ID, share.ID)
		}

		return nil, status.Errorf(codes.Internal, "failed to grant access for share %s: %v", share.ID, err)
	}

	// Check if compatibility layer is needed and can be used
	if compatLayer := tryCompatForVolumeSource(cs.d.compatOpts, shareOpts, req.GetVolumeContentSource(), shareTypeCaps); compatLayer != nil {
		if err = compatLayer.SupplementCapability(cs.d.compatOpts, share, accessRight, req, cs.d.fwdEndpoint, manilaClient, cs.d.csiClientBuilder); err != nil {
			// An error occurred, the user must clean the share manually
			// TODO needs proper monitoring
			return nil, err
		}
	}

	var accessibleTopology []*csi.Topology
	if cs.d.withTopology {
		// All requisite/preferred topologies are considered valid. Nodes from those zones are required to be able to reach the storage.
		// The operator is responsible for making sure that provided topology keys are valid and present on the nodes of the cluster.
		accessibleTopology = req.GetAccessibilityRequirements().GetPreferred()
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           share.ID,
			ContentSource:      req.GetVolumeContentSource(),
			AccessibleTopology: accessibleTopology,
			CapacityBytes:      int64(sizeInGiB) * bytesInGiB,
			VolumeContext: map[string]string{
				"shareID":        share.ID,
				"shareAccessID":  accessRight.ID,
				"cephfs-mounter": shareOpts.CephfsMounter,
			},
		},
	}, nil
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

	// Check for pending CreateSnapshots for this snapshot name
	if _, isPending := pendingSnapshots.LoadOrStore(req.GetName(), true); isPending {
		return nil, status.Errorf(codes.Aborted, "a snapshot named %s is already being created", req.GetName())
	}
	defer pendingSnapshots.Delete(req.GetName())

	manilaClient, err := cs.d.manilaClientBuilder.New(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	// Retrieve the source share

	sourceShare, err := manilaClient.GetShareByID(req.GetSourceVolumeId())
	if err != nil {
		if clouderrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "failed to create a snapshot (%s) for share %s because the share doesn't exist: %v", req.GetName(), req.GetSourceVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve source share %s when creating a snapshot (%s): %v", req.GetSourceVolumeId(), req.GetName(), err)
	}

	if strings.ToUpper(sourceShare.ShareProto) != cs.d.shareProto {
		return nil, status.Errorf(codes.InvalidArgument, "share protocol mismatch: requested a snapshot of %s share %s, but share protocol selector is set to %s",
			sourceShare.ShareProto, req.GetSourceVolumeId(), cs.d.shareProto)
	}

	// Retrieve an existing snapshot or create a new one

	klog.V(4).Infof("creating a snapshot (%s) of share %s", req.GetName(), req.GetSourceVolumeId())

	snapshot, err := getOrCreateSnapshot(req.GetName(), sourceShare.ID, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for snapshot %s of share %s to become available", snapshot.ID, req.GetSourceVolumeId())
		}

		if clouderrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "failed to create a snapshot (%s) for share %s because the share doesn't exist: %v", req.GetName(), req.GetSourceVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to create a snapshot (%s) of share %s: %v", req.GetName(), req.GetSourceVolumeId(), err)
	}

	if err = verifySnapshotCompatibility(snapshot, req); err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "a snapshot named %s already exists, but is incompatible with the request: %v", req.GetName(), err)
	}

	// Check for snapshot status, determine whether it's ready

	var readyToUse bool

	switch snapshot.Status {
	case snapshotCreating:
		readyToUse = false
	case snapshotAvailable:
		readyToUse = true
	case snapshotError:
		// An error occurred, try to roll-back the snapshot
		tryDeleteSnapshot(snapshot, manilaClient)

		manilaErrMsg, err := lastResourceError(snapshot.ID, manilaClient)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "snapshot %s of share %s is in error state, error description could not be retrieved: %v", snapshot.ID, req.GetSourceVolumeId(), err.Error())
		}

		return nil, status.Errorf(manilaErrMsg.errCode.toRpcErrorCode(), "snapshot %s of share %s is in error state: %s", snapshot.ID, req.GetSourceVolumeId(), manilaErrMsg.message)
	default:
		return nil, status.Errorf(codes.Internal, "an error occurred while creating a snapshot (%s) of share %s: snapshot is in an unexpected state: wanted creating/available, got %s",
			req.GetName(), req.GetSourceVolumeId(), snapshot.Status)
	}

	// Parse CreatedAt timestamp
	ctime, err := ptypes.TimestampProto(snapshot.CreatedAt)
	if err != nil {
		klog.Warningf("couldn't parse timestamp %v from snapshot %s: %v", snapshot.CreatedAt, snapshot.ID, err)
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     snapshot.ID,
			SourceVolumeId: req.GetSourceVolumeId(),
			SizeBytes:      int64(sourceShare.Size) * bytesInGiB,
			CreationTime:   ctime,
			ReadyToUse:     readyToUse,
		},
	}, nil
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
