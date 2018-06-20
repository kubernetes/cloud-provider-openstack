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
	"github.com/golang/glog"
	snapshots_v1 "github.com/gophercloud/gophercloud/openstack/blockstorage/v1/snapshots"
	snapshots_v2 "github.com/gophercloud/gophercloud/openstack/blockstorage/v2/snapshots"
	snapshots_v3 "github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
)

type Snapshot struct {
	// Unique identifier.
	ID string
	// Display name.
	Name string
	// Display description.
	Description string
	// ID of the Volume from which this Snapshot was created.
	VolumeID string
	// Currect status of the Snapshot.
	Status string
	// Size of the Snapshot, in GB.
	Size int
	// User-defined key-value pairs.
	Metadata map[string]string
}

type snapshotCreateOpts struct {
	VolumeID    string
	Name        string
	Description string
	Metadata    map[string]string
}

type snapshotListOpts struct {
	Name     string
	Status   string
	VolumeID string
}

func (volumes *VolumesV1) createSnapshot(opts snapshotCreateOpts) (*Snapshot, error) {
	createOpts := snapshots_v1.CreateOpts{
		VolumeID:    opts.VolumeID,
		Name:        opts.Name,
		Description: opts.Description,
	}

	if len(opts.Metadata) > 0 {
		m := make(map[string]interface{})
		for k, v := range opts.Metadata {
			m[k] = interface{}(v)
		}
		createOpts.Metadata = m
	}

	snapv1, err := snapshots_v1.Create(volumes.blockstorage, createOpts).Extract()
	if err != nil {
		return &Snapshot{}, err
	}

	snap := &Snapshot{
		ID:          snapv1.ID,
		Name:        snapv1.Name,
		Description: snapv1.Description,
		VolumeID:    snapv1.VolumeID,
		Status:      snapv1.Status,
		Size:        snapv1.Size,
		Metadata:    snapv1.Metadata,
	}

	return snap, nil
}

func (volumes *VolumesV2) createSnapshot(opts snapshotCreateOpts) (*Snapshot, error) {
	createOpts := snapshots_v2.CreateOpts{
		VolumeID:    opts.VolumeID,
		Name:        opts.Name,
		Description: opts.Description,
		Metadata:    opts.Metadata,
	}

	snapv2, err := snapshots_v2.Create(volumes.blockstorage, createOpts).Extract()
	if err != nil {
		return &Snapshot{}, err
	}

	snap := &Snapshot{
		ID:          snapv2.ID,
		Name:        snapv2.Name,
		Description: snapv2.Description,
		VolumeID:    snapv2.VolumeID,
		Status:      snapv2.Status,
		Size:        snapv2.Size,
		Metadata:    snapv2.Metadata,
	}

	return snap, nil
}

func (volumes *VolumesV3) createSnapshot(opts snapshotCreateOpts) (*Snapshot, error) {
	createOpts := snapshots_v3.CreateOpts{
		VolumeID:    opts.VolumeID,
		Name:        opts.Name,
		Description: opts.Description,
		Metadata:    opts.Metadata,
	}

	snapv3, err := snapshots_v3.Create(volumes.blockstorage, createOpts).Extract()
	if err != nil {
		return &Snapshot{}, err
	}

	snap := &Snapshot{
		ID:          snapv3.ID,
		Name:        snapv3.Name,
		Description: snapv3.Description,
		VolumeID:    snapv3.VolumeID,
		Status:      snapv3.Status,
		Size:        snapv3.Size,
		Metadata:    snapv3.Metadata,
	}

	return snap, nil
}

func (volumes *VolumesV1) deleteSnapshot(snapshotID string) error {
	err := snapshots_v1.Delete(volumes.blockstorage, snapshotID).ExtractErr()
	if err != nil {
		glog.V(3).Infof("Failed to delete snapshot: %v", err)
	}
	return err
}

func (volumes *VolumesV2) deleteSnapshot(snapshotID string) error {
	err := snapshots_v2.Delete(volumes.blockstorage, snapshotID).ExtractErr()
	if err != nil {
		glog.V(3).Infof("Failed to delete snapshot: %v", err)
	}
	return err
}

func (volumes *VolumesV3) deleteSnapshot(snapshotID string) error {
	err := snapshots_v3.Delete(volumes.blockstorage, snapshotID).ExtractErr()
	if err != nil {
		glog.V(3).Infof("Failed to delete snapshot: %v", err)
	}
	return err
}

func (volumes *VolumesV1) listSnapshot(opts snapshotListOpts) ([]Snapshot, error) {
	var vlist []Snapshot
	listOpts := snapshots_v1.ListOpts{}
	listOpts.Name = opts.Name
	listOpts.Status = opts.Status
	listOpts.VolumeID = opts.VolumeID

	pages, err := snapshots_v1.List(volumes.blockstorage, listOpts).AllPages()
	if err != nil {
		glog.V(3).Infof("Failed to retrieve snapshots from Cinder: %v", err)
		return nil, err
	}
	snaps, err := snapshots_v1.ExtractSnapshots(pages)
	if err != nil {
		glog.V(3).Infof("Failed to extract snapshot pages from Cinder: %v", err)
		return nil, err
	}

	for _, s := range snaps {
		snapshot := Snapshot{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			VolumeID:    s.VolumeID,
			Status:      s.Status,
			Size:        s.Size,
			Metadata:    s.Metadata,
		}
		vlist = append(vlist, snapshot)
	}
	return vlist, nil
}

func (volumes *VolumesV2) listSnapshot(opts snapshotListOpts) ([]Snapshot, error) {
	var vlist []Snapshot
	listOpts := snapshots_v2.ListOpts{}
	listOpts.Name = opts.Name
	listOpts.Status = opts.Status
	listOpts.VolumeID = opts.VolumeID

	pages, err := snapshots_v2.List(volumes.blockstorage, listOpts).AllPages()
	if err != nil {
		glog.V(3).Infof("Failed to retrieve snapshots from Cinder: %v", err)
		return nil, err
	}
	snaps, err := snapshots_v2.ExtractSnapshots(pages)
	if err != nil {
		glog.V(3).Infof("Failed to extract snapshot pages from Cinder: %v", err)
		return nil, err
	}

	for _, s := range snaps {
		snapshot := Snapshot{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			VolumeID:    s.VolumeID,
			Status:      s.Status,
			Size:        s.Size,
			Metadata:    s.Metadata,
		}
		vlist = append(vlist, snapshot)
	}
	return vlist, nil
}

func (volumes *VolumesV3) listSnapshot(opts snapshotListOpts) ([]Snapshot, error) {
	var vlist []Snapshot
	listOpts := snapshots_v2.ListOpts{}
	listOpts.Name = opts.Name
	listOpts.Status = opts.Status
	listOpts.VolumeID = opts.VolumeID

	pages, err := snapshots_v3.List(volumes.blockstorage, listOpts).AllPages()
	if err != nil {
		glog.V(3).Infof("Failed to retrieve snapshots from Cinder: %v", err)
		return nil, err
	}
	snaps, err := snapshots_v3.ExtractSnapshots(pages)
	if err != nil {
		glog.V(3).Infof("Failed to extract snapshot pages from Cinder: %v", err)
		return nil, err
	}

	for _, s := range snaps {
		snapshot := Snapshot{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			VolumeID:    s.VolumeID,
			Status:      s.Status,
			Size:        s.Size,
			Metadata:    s.Metadata,
		}
		vlist = append(vlist, snapshot)
	}
	return vlist, nil
}

// CreateSnapshot issues a request to take a Snapshot of the specified Volume with the corresponding ID and
// returns the resultant gophercloud Snapshot Item upon success
func (os *OpenStack) CreateSnapshot(name, volID, description string, tags *map[string]string) (*Snapshot, error) {
	opts := snapshotCreateOpts{
		VolumeID:    volID,
		Name:        name,
		Description: description,
	}
	if tags != nil {
		opts.Metadata = *tags
	}

	return os.volumes.createSnapshot(opts)
}

// ListSnapshots retrieves a list of active snapshots from Cinder for the corresponding Tenant.  We also
// provide the ability to provide limit and offset to enable the consumer to provide accurate pagination.
// In addition the filters argument provides a mechanism for passing in valid filter strings to the list
// operation.  Valid filter keys are:  Name, Status, VolumeID (TenantID has no effect)
func (os *OpenStack) ListSnapshots(limit, offset int, filters map[string]string) ([]Snapshot, error) {
	opts := snapshotListOpts{}
	return os.volumes.listSnapshot(opts)
}

// DeleteSnapshot issues a request to delete the Snapshot with the specified ID from the Cinder backend
func (os *OpenStack) DeleteSnapshot(snapID string) error {
	return os.volumes.deleteSnapshot(snapID)
}
