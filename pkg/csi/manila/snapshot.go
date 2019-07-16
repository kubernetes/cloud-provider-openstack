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
	"fmt"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/snapshots"
	"k8s.io/klog"
)

const (
	snapshotCreating      = "creating"
	snapshotDeleting      = "deleting"
	snapshotError         = "error"
	snapshotErrorDeleting = "error_deleting"
	snapshotAvailable     = "available"

	snapshotDescription = "snapshotted-by=manila.csi.openstack.org"
)

// getOrCreateSnapshot retrieves an existing snapshot with name=snapName, or creates a new one if it doesn't exist yet.
// Instead of waiting for the snapshot to become available (as getOrCreateShare does), CSI's ready_to_use flag is used to signal readiness
func getOrCreateSnapshot(snapName, sourceShareID string, manilaClient *gophercloud.ServiceClient) (*snapshots.Snapshot, error) {
	var (
		snapshot *snapshots.Snapshot
		err      error
	)

	// First, check if the snapshot already exists or needs to be created

	if snapshot, err = getSnapshotByName(snapName, manilaClient); err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
			// It doesn't exist, create it

			req := snapshots.CreateOpts{
				ShareID:     sourceShareID,
				Name:        snapName,
				Description: snapshotDescription,
			}

			var createErr error
			if snapshot, createErr = snapshots.Create(manilaClient, req).Extract(); createErr != nil {
				return nil, createErr
			}

		} else {
			// Something else is wrong
			return nil, fmt.Errorf("failed to probe for snapshot: %v", err)
		}
	} else {
		klog.V(4).Infof("a snapshot named %s already exists", snapName)
	}

	return snapshot, nil
}

func deleteSnapshot(snapID string, manilaClient *gophercloud.ServiceClient) error {
	if err := snapshots.Delete(manilaClient, snapID).ExtractErr(); err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
			klog.V(4).Infof("snapshot %s not found, assuming it to be already deleted", snapID)
		} else {
			return err
		}
	}

	return nil
}

func getSnapshotByName(snapshotName string, manilaClient *gophercloud.ServiceClient) (*snapshots.Snapshot, error) {
	snapID, err := snapshots.IDFromName(manilaClient, snapshotName)
	if err != nil {
		return nil, err
	}

	return getSnapshotByID(snapID, manilaClient)
}

func getSnapshotByID(snapshotID string, manilaClient *gophercloud.ServiceClient) (*snapshots.Snapshot, error) {
	return snapshots.Get(manilaClient, snapshotID).Extract()
}
