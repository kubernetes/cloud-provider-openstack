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
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/responsebroker"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
)

type controllerServer struct {
	d *Driver
}

var (
	rbVolumeID = responsebroker.New()
)

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
		return nil, status.Errorf(codes.InvalidArgument, "invalid volume secrets: %v", err)
	}

	volID := newVolumeID(req.GetName())

	// Acquire a lock for this volume
	handle, isNewRequest := rbVolumeID.Acquire(string(volID))
	if !isNewRequest {
		// Check if the master was successful

		if respData, respErr := handle.Read(); respErr == nil {
			// Yup
			defer handle.Release()
			return respData.(*csi.CreateVolumeResponse), nil
		}

		// The previous master failed, this request now becomes the master
	}

	var (
		createErr    error
		createResp   *csi.CreateVolumeResponse
		manilaClient *gophercloud.ServiceClient
		share        *shares.Share
		accessRight  *shares.AccessRight
	)

	defer func() {
		// Let other in-flight calls read our result
		handle.Write(createResp, createErr)

		if createErr == nil {
			// This request was a success, wait for any other in-flight calls to read our result
			rbVolumeID.Done(string(volID))
		}
	}()

	manilaClient, createErr = newManilaV2Client(osOpts)
	if createErr != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", createErr)
	}

	// Create or get the share

	requestedSize := req.GetCapacityRange().GetRequiredBytes()
	if requestedSize == 0 {
		// At least 1GiB
		requestedSize = 1 * bytesInGiB
	}

	sizeInGiB := bytesToGiB(requestedSize)

	klog.V(4).Infof("creating a share for volume %s", volID)

	share, createErr = cs.getOrCreateShare(volID, sizeInGiB, shareOpts, manilaClient)
	if createErr != nil {
		if createErr == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for volume %s to become available", volID)
		}

		return nil, status.Errorf(codes.Internal, "failed to create volume %s: %v", volID, createErr)
	}

	if share.Size != sizeInGiB {
		return nil, status.Errorf(codes.AlreadyExists, "volume %s already exists, but is incompatible with the request", volID)
	}

	// Grant access to the share

	ad := getShareAdapter(shareOpts.Protocol)

	klog.V(4).Infof("creating an access rule for volume %s (share ID %s)", volID, share.ID)

	accessRight, createErr = ad.GetOrGrantAccess(&shareadapters.GrantAccessArgs{Share: share, ManilaClient: manilaClient, Options: shareOpts})
	if createErr != nil {
		if createErr == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for access rights for volume %s to become available", volID)
		}

		return nil, status.Errorf(codes.Internal, "failed to grant access for volume %s (share ID %s): %v", volID, share.ID, createErr)
	}

	klog.V(4).Infof("successfully created volume %s", volID)

	createResp = &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      string(volID),
			CapacityBytes: int64(sizeInGiB) * bytesInGiB,
			VolumeContext: map[string]string{
				"shareID":        share.ID,
				"shareAccessID":  accessRight.ID,
				"cephfs-mounter": shareOpts.CephfsMounter,
			},
		},
	}

	return createResp, nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if err := validateDeleteVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	volID := volumeID(req.VolumeId)

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid volume secrets: %v", err)
	}

	manilaClient, err := newManilaV2Client(osOpts)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	if err := deleteShare(volID, manilaClient); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete volume %s: %v", volID, err)
	}

	klog.V(4).Infof("successfully deleted volume %s", volID)

	return &csi.DeleteVolumeResponse{}, nil
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

func (cs *controllerServer) CreateSnapshot(context.Context, *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) DeleteSnapshot(context.Context, *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
