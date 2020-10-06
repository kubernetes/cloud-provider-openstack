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
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	clouderrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

type volumeCreator interface {
	create(req *csi.CreateVolumeRequest, shareName string, sizeInGiB int, manilaClient manilaclient.Interface, shareOpts *options.ControllerVolumeContext) (*shares.Share, error)
}

type blankVolume struct{}

func (blankVolume) create(req *csi.CreateVolumeRequest, shareName string, sizeInGiB int, manilaClient manilaclient.Interface, shareOpts *options.ControllerVolumeContext) (*shares.Share, error) {
	klog.V(4).Infof("creating a new share (%s) in AZ %s", shareName, coalesceValue(shareOpts.AvailabilityZone))

	createOpts := &shares.CreateOpts{
		AvailabilityZone: shareOpts.AvailabilityZone,
		ShareProto:       shareOpts.Protocol,
		ShareType:        shareOpts.Type,
		ShareNetworkID:   shareOpts.ShareNetworkID,
		Name:             shareName,
		Description:      shareDescription,
		Size:             sizeInGiB,
	}

	share, manilaErrCode, err := getOrCreateShare(shareName, createOpts, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for share %s to become available", share.ID)
		}

		if manilaErrCode != 0 {
			// An error has occurred, try to roll-back the share
			tryDeleteShare(share, manilaClient)
		}

		return nil, status.Errorf(manilaErrCode.toRpcErrorCode(), "failed to create a share (%s): %v", shareName, err)
	}

	return share, err
}

type volumeFromSnapshot struct{}

func (volumeFromSnapshot) create(req *csi.CreateVolumeRequest, shareName string, sizeInGiB int, manilaClient manilaclient.Interface, shareOpts *options.ControllerVolumeContext) (*shares.Share, error) {
	snapshotSource := req.GetVolumeContentSource().GetSnapshot()

	if shareOpts.Protocol == "CEPHFS" {
		// TODO: Restoring shares from CephFS snapshots needs special handling.
		return nil, status.Errorf(codes.InvalidArgument, "restoring CephFS snapshots is not supported yet")
	}

	if snapshotSource.GetSnapshotId() == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot ID cannot be empty")
	}

	klog.V(4).Infof("restoring snapshot %s into a share (%s) in AZ %s", snapshotSource.GetSnapshotId(), shareName, coalesceValue(shareOpts.AvailabilityZone))

	snapshot, err := manilaClient.GetSnapshotByID(snapshotSource.GetSnapshotId())
	if err != nil {
		if clouderrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "source snapshot %s not found: %v", snapshotSource.GetSnapshotId(), err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve snapshot %s: %v", snapshotSource.GetSnapshotId(), err)
	}

	if snapshot.Status != snapshotAvailable {
		if snapshot.Status == snapshotCreating {
			return nil, status.Errorf(codes.Unavailable, "snapshot %s is in transient creating state", snapshot.ID)
		}

		return nil, status.Errorf(codes.FailedPrecondition, "snapshot %s is in invalid state: expected 'available', got '%s'", snapshot.ID, snapshot.Status)
	}

	createOpts := &shares.CreateOpts{
		AvailabilityZone: shareOpts.AvailabilityZone,
		SnapshotID:       snapshot.ID,
		ShareProto:       shareOpts.Protocol,
		ShareType:        shareOpts.Type,
		ShareNetworkID:   shareOpts.ShareNetworkID,
		Name:             shareName,
		Description:      shareDescription,
		Size:             sizeInGiB,
	}

	share, manilaErrCode, err := getOrCreateShare(shareName, createOpts, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for share %s to become available", share.ID)
		}

		if manilaErrCode != 0 {
			// An error has occurred, try to roll-back the share
			tryDeleteShare(share, manilaClient)
		}

		return nil, status.Errorf(manilaErrCode.toRpcErrorCode(), "failed to restore snapshot %s into a share (%s): %v", snapshotSource.GetSnapshotId(), shareName, err)
	}

	return share, err
}
