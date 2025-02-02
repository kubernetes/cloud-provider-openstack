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

	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	clouderrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
)

type volumeCreator interface {
	create(ctx context.Context, manilaClient manilaclient.Interface, shareName string, sizeInGiB int, shareOpts *options.ControllerVolumeContext, shareMetadata map[string]string) (*shares.Share, error)
}

func create(ctx context.Context, manilaClient manilaclient.Interface, shareName string, sizeInGiB int, shareOpts *options.ControllerVolumeContext, shareMetadata map[string]string, snapshotID string) (*shares.Share, error) {
	createOpts := &shares.CreateOpts{
		AvailabilityZone: shareOpts.AvailabilityZone,
		ShareProto:       shareOpts.Protocol,
		ShareType:        shareOpts.Type,
		ShareNetworkID:   shareOpts.ShareNetworkID,
		ShareGroupID:     shareOpts.GroupID,
		Name:             shareName,
		Description:      shareDescription,
		Size:             sizeInGiB,
		Metadata:         shareMetadata,
		SnapshotID:       snapshotID,
	}

	// Set scheduler hints if affinity or anti-affinity is set in PVC annotations
	if shareOpts.Affinity != "" || shareOpts.AntiAffinity != "" {
		// Set microversion to 2.65 to use scheduler hints
		v := manilaClient.GetMicroversion()
		manilaClient.SetMicroversion("2.65")
		defer manilaClient.SetMicroversion(v)
		createOpts.SchedulerHints = &shares.SchedulerHints{
			DifferentHost: shareOpts.AntiAffinity,
			SameHost:      shareOpts.Affinity,
		}
	}

	share, manilaErrCode, err := getOrCreateShare(ctx, manilaClient, shareName, createOpts)
	if err != nil {
		if wait.Interrupted(err) {
			return nil, status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for volume %s to become available", shareName)
		}

		if manilaErrCode != 0 {
			// An error has occurred, try to roll-back the share
			tryDeleteShare(ctx, manilaClient, share)
		}

		if snapshotID != "" {
			return nil, status.Errorf(manilaErrCode.toRPCErrorCode(), "failed to restore snapshot %s into volume %s: %v", snapshotID, shareName, err)
		}
		return nil, status.Errorf(manilaErrCode.toRPCErrorCode(), "failed to create volume %s: %v", shareName, err)
	}

	return share, err
}

type blankVolume struct{}

func (blankVolume) create(ctx context.Context, manilaClient manilaclient.Interface, shareName string, sizeInGiB int, shareOpts *options.ControllerVolumeContext, shareMetadata map[string]string) (*shares.Share, error) {
	return create(ctx, manilaClient, shareName, sizeInGiB, shareOpts, shareMetadata, "")
}

type volumeFromSnapshot struct {
	snapshotID string
}

func (v volumeFromSnapshot) create(ctx context.Context, manilaClient manilaclient.Interface, shareName string, sizeInGiB int, shareOpts *options.ControllerVolumeContext, shareMetadata map[string]string) (*shares.Share, error) {
	if v.snapshotID == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot ID cannot be empty")
	}

	snapshot, err := manilaClient.GetSnapshotByID(ctx, v.snapshotID)
	if err != nil {
		if clouderrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "source snapshot %s not found: %v", v.snapshotID, err)
		}

		return nil, status.Errorf(codes.Internal, "failed to retrieve snapshot %s: %v", v.snapshotID, err)
	}

	if snapshot.Status != snapshotAvailable {
		if snapshot.Status == snapshotCreating {
			return nil, status.Errorf(codes.Unavailable, "snapshot %s is in transient creating state", snapshot.ID)
		}

		return nil, status.Errorf(codes.FailedPrecondition, "snapshot %s is in invalid state: expected 'available', got '%s'", snapshot.ID, snapshot.Status)
	}

	return create(ctx, manilaClient, shareName, sizeInGiB, shareOpts, shareMetadata, snapshot.ID)
}
