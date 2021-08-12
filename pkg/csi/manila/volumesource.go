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
)

type volumeCreator interface {
	create(req *csi.CreateVolumeRequest, shareName string, sizeInGiB int, manilaClient manilaclient.Interface, shareOpts *options.ControllerVolumeContext, shareMetadata map[string]string) (*shares.Share, error)
}

type blankVolume struct{}

func (blankVolume) create(req *csi.CreateVolumeRequest, shareName string, sizeInGiB int, manilaClient manilaclient.Interface, shareOpts *options.ControllerVolumeContext, shareMetadata map[string]string) (*shares.Share, error) {
	createOpts := &shares.CreateOpts{
		AvailabilityZone: shareOpts.AvailabilityZone,
		ShareProto:       shareOpts.Protocol,
		ShareType:        shareOpts.Type,
		ShareNetworkID:   shareOpts.ShareNetworkID,
		Name:             shareName,
		Description:      shareDescription,
		Size:             sizeInGiB,
		Metadata:         shareMetadata,
	}

	share, manilaErrCode, err := getOrCreateShare(shareName, createOpts, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for volume %s to become available", shareName)
		}

		if manilaErrCode != 0 {
			// An error has occurred, try to roll-back the share
			tryDeleteShare(share, manilaClient)
		}

		return nil, status.Errorf(manilaErrCode.toRpcErrorCode(), "failed to create volume %s: %v", shareName, err)
	}

	return share, err
}

type volumeFromSnapshot struct{}

func (volumeFromSnapshot) create(req *csi.CreateVolumeRequest, shareName string, sizeInGiB int, manilaClient manilaclient.Interface, shareOpts *options.ControllerVolumeContext, shareMetadata map[string]string) (*shares.Share, error) {
	snapshotSource := req.GetVolumeContentSource().GetSnapshot()

	if snapshotSource.GetSnapshotId() == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot ID cannot be empty")
	}

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
		Metadata:         shareMetadata,
	}

	share, manilaErrCode, err := getOrCreateShare(shareName, createOpts, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for volume %s to become available", share.Name)
		}

		if manilaErrCode != 0 {
			// An error has occurred, try to roll-back the share
			tryDeleteShare(share, manilaClient)
		}

		return nil, status.Errorf(manilaErrCode.toRpcErrorCode(), "failed to restore snapshot %s into volume %s: %v", snapshotSource.GetSnapshotId(), shareName, err)
	}

	return share, err
}
