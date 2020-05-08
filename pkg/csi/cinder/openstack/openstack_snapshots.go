/*
Copyright 2018 The Kubernetes Authors.

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

// Package openstack snapshots provides an implementation of Cinder Snapshot features
// cinder functions using Gophercloud.
package openstack

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

const (
	snapshotReadyStatus = "available"
	snapReadyDuration   = 1 * time.Second
	snapReadyFactor     = 1.2
	snapReadySteps      = 10

	snapshotDescription = "Created by OpenStack Cinder CSI driver"
	snapshotForceCreate = "force-create"
)

// CreateSnapshot issues a request to take a Snapshot of the specified Volume with the corresponding ID and
// returns the resultant gophercloud Snapshot Item upon success
func (os *OpenStack) CreateSnapshot(name, volID string, tags *map[string]string) (*snapshots.Snapshot, error) {

	force := false
	// if no flag given, then force will be false by default
	// if flag it given , check it
	if item, ok := (*tags)[snapshotForceCreate]; ok {
		var err error
		force, err = strconv.ParseBool(item)
		if err != nil {
			klog.V(5).Infof("Make force create flag to false due to: %v", err)
		}

		delete(*tags, snapshotForceCreate)
	}
	// Force the creation of snapshot even the Volume is in in-use state
	opts := &snapshots.CreateOpts{
		VolumeID:    volID,
		Name:        name,
		Description: snapshotDescription,
		Force:       force,
	}
	if tags != nil {
		opts.Metadata = *tags
	}
	// TODO: Do some check before really call openstack API on the input

	snap, err := snapshots.Create(os.blockstorage, opts).Extract()
	if err != nil {
		return &snapshots.Snapshot{}, err
	}
	// There's little value in rewrapping these gophercloud types into yet another abstraction/type, instead just
	// return the gophercloud item
	return snap, nil
}

// ListSnapshots retrieves a list of active snapshots from Cinder for the corresponding Tenant.  We also
// provide the ability to provide limit and offset to enable the consumer to provide accurate pagination.
// In addition the filters argument provides a mechanism for passing in valid filter strings to the list
// operation.  Valid filter keys are:  Name, Status, VolumeID, Limit, Marker (TenantID has no effect)
func (os *OpenStack) ListSnapshots(filters map[string]string) ([]snapshots.Snapshot, string, error) {
	// Build the Opts
	opts := snapshots.ListOpts{}
	nextPageToken := ""

	for key, val := range filters {
		switch key {
		case "Status":
			opts.Status = val
		case "Name":
			opts.Name = val
		case "VolumeID":
			opts.VolumeID = val
		case "Marker":
			opts.Marker = val
		case "Limit":
			opts.Limit, _ = strconv.Atoi(val)
		default:
			klog.V(3).Infof("Not a valid filter key %s", key)
		}
	}

	pages, err := snapshots.List(os.blockstorage, opts).AllPages()
	if err != nil {
		klog.V(3).Infof("Failed to retrieve snapshots: %v", err)
		return nil, nextPageToken, err
	}
	snaps, err := snapshots.ExtractSnapshots(pages)
	if err != nil {
		klog.V(3).Infof("Failed to extract snapshot pages: %v", err)
		return nil, nextPageToken, err
	}

	nextPageURL, err := pages.NextPageURL()
	if err != nil && nextPageURL != "" {
		if queryParams, nerr := url.ParseQuery(nextPageURL); nerr != nil {
			nextPageToken = queryParams.Get("marker")
		}
	}
	return snaps, nextPageToken, nil

}

// DeleteSnapshot issues a request to delete the Snapshot with the specified ID from the Cinder backend
func (os *OpenStack) DeleteSnapshot(snapID string) error {
	err := snapshots.Delete(os.blockstorage, snapID).ExtractErr()
	if err != nil {
		klog.V(3).Infof("Failed to delete snapshot: %v", err)
	}
	return err
}

//GetSnapshotByID returns snapshot details by id
func (os *OpenStack) GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error) {
	s, err := snapshots.Get(os.blockstorage, snapshotID).Extract()
	if err != nil {
		klog.V(3).Infof("Failed to get snapshot: %v", err)
		return nil, err
	}
	return s, nil
}

// WaitSnapshotReady waits till snapshot is ready
func (os *OpenStack) WaitSnapshotReady(snapshotID string) error {
	backoff := wait.Backoff{
		Duration: snapReadyDuration,
		Factor:   snapReadyFactor,
		Steps:    snapReadySteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		ready, err := os.snapshotIsReady(snapshotID)
		if err != nil {
			return false, err
		}
		return ready, nil
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("Timeout, Snapshot  %s is still not Ready %v", snapshotID, err)
	}

	return err
}

func (os *OpenStack) snapshotIsReady(snapshotID string) (bool, error) {
	snap, err := os.GetSnapshotByID(snapshotID)
	if err != nil {
		return false, err
	}

	return snap.Status == snapshotReadyStatus, nil
}
