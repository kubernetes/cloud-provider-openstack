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
	"encoding/json"
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

const clusterMetadataKey = "manila.csi.openstack.org/cluster"

type controllerServer struct {
	d *Driver
}

var (
	pendingVolumes   = sync.Map{}
	pendingSnapshots = sync.Map{}
)

func getVolumeCreator(source *csi.VolumeContentSource, shareOpts *options.ControllerVolumeContext, compatOpts *options.CompatibilityOptions) (volumeCreator, error) {
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

func filterParametersForVolumeContext(params map[string]string, recognizedFields []string) map[string]string {
	volCtx := make(map[string]string)

	for _, fieldName := range recognizedFields {
		if val, ok := params[fieldName]; ok {
			volCtx[fieldName] = val
		}
	}

	return volCtx
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

	shareMetadata, err := prepareShareMetadata(shareOpts.AppendShareMetadata, cs.d.clusterID)
	if err != nil {
		return nil, err
	}

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid OpenStack secrets: %v", err)
	}

	// Check for pending CreateVolume for this volume name
	if _, isPending := pendingVolumes.LoadOrStore(req.GetName(), true); isPending {
		return nil, status.Errorf(codes.Aborted, "volume %s is already being processed", req.GetName())
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

	volCreator, err := getVolumeCreator(req.GetVolumeContentSource(), shareOpts, cs.d.compatOpts)
	if err != nil {
		return nil, err
	}

	share, err := volCreator.create(req, req.GetName(), sizeInGiB, manilaClient, shareOpts, shareMetadata)
	if err != nil {
		return nil, err
	}

	if err = verifyVolumeCompatibility(sizeInGiB, req, share, shareOpts, cs.d.compatOpts, shareTypeCaps); err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "volume %s already exists, but is incompatible with the request: %v", req.GetName(), err)
	}

	// Grant access to the share

	ad := getShareAdapter(shareOpts.Protocol)

	accessRight, err := ad.GetOrGrantAccess(&shareadapters.GrantAccessArgs{Share: share, ManilaClient: manilaClient, Options: shareOpts})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for access rule %s for volume %s to become available", accessRight.ID, share.Name)
		}

		return nil, status.Errorf(codes.Internal, "failed to grant access to volume %s: %v", share.Name, err)
	}

	var accessibleTopology []*csi.Topology
	if cs.d.withTopology {
		// All requisite/preferred topologies are considered valid. Nodes from those zones are required to be able to reach the storage.
		// The operator is responsible for making sure that provided topology keys are valid and present on the nodes of the cluster.
		accessibleTopology = req.GetAccessibilityRequirements().GetPreferred()
	}

	volCtx := filterParametersForVolumeContext(params, options.NodeVolumeContextFields())
	volCtx["shareID"] = share.ID
	volCtx["shareAccessID"] = accessRight.ID

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           share.ID,
			ContentSource:      req.GetVolumeContentSource(),
			AccessibleTopology: accessibleTopology,
			CapacityBytes:      int64(sizeInGiB) * bytesInGiB,
			VolumeContext:      volCtx,
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
		return nil, status.Errorf(codes.Internal, "failed to delete volume %s: %v", req.GetVolumeId(), err)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
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
		return nil, status.Errorf(codes.Aborted, "snapshot %s is already being processed", req.GetName())
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
			return nil, status.Errorf(codes.NotFound, "failed to create snapshot %s for volume %s because the volume doesn't exist: %v", req.GetName(), req.GetSourceVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve source volume %s when creating snapshot %s: %v", req.GetSourceVolumeId(), req.GetName(), err)
	}

	if strings.ToUpper(sourceShare.ShareProto) != cs.d.shareProto {
		return nil, status.Errorf(codes.InvalidArgument, "share protocol mismatch: requested snapshot of %s volume %s, but share protocol selector is set to %s",
			sourceShare.ShareProto, req.GetSourceVolumeId(), cs.d.shareProto)
	}

	// In order to satisfy CSI spec requirements around CREATE_DELETE_SNAPSHOT
	// and the ability to populate volumes with snapshot contents, parent share
	// must advertise snapshot_support and create_share_from_snapshot_support
	// capabilities.

	if !sourceShare.SnapshotSupport || !sourceShare.CreateShareFromSnapshotSupport {
		return nil, status.Errorf(codes.InvalidArgument,
			"cannot create snapshot %s for volume %s: parent share must advertise snapshot_support and create_share_from_snapshot_support capabilities",
			req.GetName(), req.GetSourceVolumeId())
	}

	// Retrieve an existing snapshot or create a new one

	snapshot, err := getOrCreateSnapshot(req.GetName(), sourceShare.ID, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for snapshot %s of volume %s to become available", snapshot.ID, req.GetSourceVolumeId())
		}

		if clouderrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "failed to create snapshot %s for volume %s because the volume doesn't exist: %v", req.GetName(), req.GetSourceVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to create snapshot %s of volume  %s: %v", req.GetName(), req.GetSourceVolumeId(), err)
	}

	if err = verifySnapshotCompatibility(snapshot, req); err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "snapshot %s already exists, but is incompatible with the request: %v", req.GetName(), err)
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
			return nil, status.Errorf(codes.Internal, "snapshot %s of volume %s is in error state, error description could not be retrieved: %v", snapshot.ID, req.GetSourceVolumeId(), err.Error())
		}

		return nil, status.Errorf(manilaErrMsg.errCode.toRpcErrorCode(), "snapshot %s of volume %s is in error state: %s", snapshot.ID, req.GetSourceVolumeId(), manilaErrMsg.message)
	default:
		return nil, status.Errorf(codes.Internal, "an error occurred while creating snapshot %s of volume %s: snapshot is in an unexpected state: wanted creating/available, got %s",
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
			return nil, status.Errorf(codes.NotFound, "volume %s not found: %v", req.GetVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve volume %s: %v", req.GetVolumeId(), err)
	}

	if share.Status != shareAvailable {
		if share.Status == shareCreating {
			return nil, status.Errorf(codes.Unavailable, "volume %s is in transient creating state", req.GetVolumeId())
		}

		return nil, status.Errorf(codes.FailedPrecondition, "volume %s is in an unexpected state: wanted %s, got %s", req.GetVolumeId(), shareAvailable, share.Status)
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
	if err := validateControllerExpandVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Configuration

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid OpenStack secrets: %v", err)
	}

	manilaClient, err := cs.d.manilaClientBuilder.New(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	// Retrieve the share by its ID

	share, err := manilaClient.GetShareByID(req.GetVolumeId())
	if err != nil {
		if clouderrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "volume %s not found: %v", req.GetVolumeId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve volume %s: %v", req.GetVolumeId(), err)
	}

	// Check for pending operations on this volume
	if _, isPending := pendingVolumes.LoadOrStore(share.Name, true); isPending {
		return nil, status.Errorf(codes.Aborted, "volume %s is already being processed", share.Name)
	}
	defer pendingVolumes.Delete(share.Name)

	// Try to expand the share

	currentSizeInBytes := int64(share.Size * bytesInGiB)

	if currentSizeInBytes >= req.GetCapacityRange().GetRequiredBytes() {
		// Share is already larger than requested size

		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes: currentSizeInBytes,
		}, nil
	}

	desiredSizeInGiB := int(req.GetCapacityRange().GetRequiredBytes() / bytesInGiB)

	if err = extendShare(share, desiredSizeInGiB, manilaClient); err != nil {
		return nil, err
	}

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes: int64(desiredSizeInGiB * bytesInGiB),
	}, nil
}

func (cs *controllerServer) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func parseStringMapFromJson(data string) (m map[string]string, err error) {
	if data == "" {
		return
	}

	err = json.Unmarshal([]byte(data), &m)
	return
}

func prepareShareMetadata(appendShareMetadata, clusterID string) (map[string]string, error) {
	shareMetadata, err := parseStringMapFromJson(appendShareMetadata)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to parse appendShareMetadata field: %v", err)
	}

	if clusterID != "" {
		if shareMetadata == nil {
			shareMetadata = make(map[string]string)
		}
		if val, ok := shareMetadata[clusterMetadataKey]; ok && val != clusterID {
			klog.Warningf("skip adding cluster ID %v to share metadata because appended metadata already defines it as %v", clusterID, val)
		} else {
			shareMetadata[clusterMetadataKey] = clusterID
		}
	}

	return shareMetadata, nil
}
