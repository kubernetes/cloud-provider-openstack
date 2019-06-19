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
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/klog"
)

type volumeCreator interface {
	create(req *csi.CreateVolumeRequest, volID volumeID, sizeInGiB int, manilaClient *gophercloud.ServiceClient, shareOpts *options.ControllerVolumeContext) (*shares.Share, error)
}

type blankVolume struct{}

func (blankVolume) create(req *csi.CreateVolumeRequest, volID volumeID, sizeInGiB int, manilaClient *gophercloud.ServiceClient, shareOpts *options.ControllerVolumeContext) (*shares.Share, error) {
	klog.V(4).Infof("creating a share for volume %s", volID)

	createOpts := &shares.CreateOpts{
		ShareProto:     shareOpts.Protocol,
		ShareType:      shareOpts.Type,
		ShareNetworkID: shareOpts.ShareNetworkID,
		Name:           string(volID),
		Description:    shareDescription,
		Size:           sizeInGiB,
	}

	share, err := getOrCreateShare(volID, createOpts, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for volume %s to become available", volID)
		}

		return nil, status.Errorf(codes.Internal, "failed to create volume %s: %v", volID, err)
	}

	return share, err
}

type volumeFromSnapshot struct{}

func (volumeFromSnapshot) create(req *csi.CreateVolumeRequest, volID volumeID, sizeInGiB int, manilaClient *gophercloud.ServiceClient, shareOpts *options.ControllerVolumeContext) (*shares.Share, error) {
	snapshotSource := req.GetVolumeContentSource().GetSnapshot()

	if shareOpts.Protocol == "CEPHFS" {
		// TODO: Restoring shares from CephFS snapshots needs special handling.
		return nil, status.Errorf(codes.InvalidArgument, "restoring CephFS snapshots is not supported yet")
	}

	if snapshotSource.GetSnapshotId() == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot ID cannot be empty")
	}

	klog.V(4).Infof("restoring snapshot %s to volume %s", snapshotSource.GetSnapshotId(), volID)

	snapshot, err := getSnapshotByName(snapshotSource.GetSnapshotId(), manilaClient)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve snapshot %s: %v", snapshotSource.GetSnapshotId(), err)
	}

	createOpts := &shares.CreateOpts{
		SnapshotID:     snapshot.ID,
		ShareProto:     shareOpts.Protocol,
		ShareType:      shareOpts.Type,
		ShareNetworkID: shareOpts.ShareNetworkID,
		Name:           string(volID),
		Description:    shareDescription,
		Size:           sizeInGiB,
	}

	share, err := getOrCreateShare(volID, createOpts, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for volume %s to become available", volID)
		}

		return nil, status.Errorf(codes.Internal, "failed to restore snapshot %s to volume %s: %v", snapshotSource.GetSnapshotId(), volID, err)
	}

	return share, err
}
