/*
Copyright 2017 The Kubernetes Authors.

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

package openstack

import (
	"strings"

	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/mock"
)

var fakeVol1 = volumes.Volume{
	ID:               "261a8b81-3660-43e5-bab8-6470b65ee4e9",
	Name:             "fake-duplicate",
	Status:           "available",
	AvailabilityZone: "nova",
	Size:             1,
}

var fakeVol2 = volumes.Volume{
	ID:               "261a8b81-3660-43e5-bab8-6470b65ee4e9",
	Name:             "fake-duplicate",
	Status:           "available",
	AvailabilityZone: "nova",
	Size:             1,
}

var fakeSnapshot = snapshots.Snapshot{
	ID:       "261a8b81-3660-43e5-bab8-6470b65ee4e8",
	Name:     "fake-snapshot",
	Status:   "available",
	Size:     1,
	VolumeID: "CSIVolumeID",
	Metadata: make(map[string]string),
}

// OpenStackMock is an autogenerated mock type for the IOpenStack type
// ORIGINALLY GENERATED BY mockery with hand edits
type OpenStackMock struct {
	mock.Mock
}

// AttachVolume provides a mock function with given fields: instanceID, volumeID
func (_m *OpenStackMock) AttachVolume(instanceID string, volumeID string) (string, error) {
	ret := _m.Called(instanceID, volumeID)

	var r0 string
	if rf, ok := ret.Get(0).(func(string, string) string); ok {
		r0 = rf(instanceID, volumeID)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, string) error); ok {
		r1 = rf(instanceID, volumeID)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// CreateVolume provides a mock function with given fields: name, size, vtype, availability, tags
func (_m *OpenStackMock) CreateVolume(name string, size int, vtype string, availability string, snapshotID string, tags *map[string]string) (*volumes.Volume, error) {
	ret := _m.Called(name, size, vtype, availability, snapshotID, tags)

	var r0 *volumes.Volume
	if rf, ok := ret.Get(0).(func(string, int, string, string, string, *map[string]string) *volumes.Volume); ok {
		r0 = rf(name, size, vtype, availability, snapshotID, tags)
	} else {
		r0 = ret.Get(0).(*volumes.Volume)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, int, string, string, string, *map[string]string) error); ok {
		r1 = rf(name, size, vtype, availability, snapshotID, tags)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DeleteVolume provides a mock function with given fields: volumeID
func (_m *OpenStackMock) DeleteVolume(volumeID string) error {
	ret := _m.Called(volumeID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string) error); ok {
		r0 = rf(volumeID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// GetVolume provides a mock function with given fields: volumeID
func (_m *OpenStackMock) GetVolume(volumeID string) (*volumes.Volume, error) {
	return &fakeVol1, nil
}

// DetachVolume provides a mock function with given fields: instanceID, volumeID
func (_m *OpenStackMock) DetachVolume(instanceID string, volumeID string) error {
	ret := _m.Called(instanceID, volumeID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string) error); ok {
		r0 = rf(instanceID, volumeID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// GetAttachmentDiskPath provides a mock function with given fields: instanceID, volumeID
func (_m *OpenStackMock) GetAttachmentDiskPath(instanceID string, volumeID string) (string, error) {
	ret := _m.Called(instanceID, volumeID)

	var r0 string
	if rf, ok := ret.Get(0).(func(string, string) string); ok {
		r0 = rf(instanceID, volumeID)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, string) error); ok {
		r1 = rf(instanceID, volumeID)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// WaitDiskAttached provides a mock function with given fields: instanceID, volumeID
func (_m *OpenStackMock) WaitDiskAttached(instanceID string, volumeID string) error {
	ret := _m.Called(instanceID, volumeID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string) error); ok {
		r0 = rf(instanceID, volumeID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// WaitDiskDetached provides a mock function with given fields: instanceID, volumeID
func (_m *OpenStackMock) WaitDiskDetached(instanceID string, volumeID string) error {
	ret := _m.Called(instanceID, volumeID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string, string) error); ok {
		r0 = rf(instanceID, volumeID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// GetVolumesByName provides a mock function with given fields: name
func (_m *OpenStackMock) GetVolumesByName(name string) ([]volumes.Volume, error) {
	var vlist []volumes.Volume
	if strings.Contains(name, "fake-duplicate") {
		vlist = append(vlist, fakeVol1)
	}

	if name == "fake-duplicate2x" {
		vlist[0].Name = "fake-duplicate2x"
		vlist = append(vlist, fakeVol2)
		vlist[1].Name = "fake-duplicate2x"
	}
	return vlist, nil
}

// ListSnapshots provides a mock function with given fields: limit, offset, filters
func (_m *OpenStackMock) ListSnapshots(limit int, offset int, filters map[string]string) ([]snapshots.Snapshot, error) {
	ret := _m.Called(limit, offset, filters)

	var r0 []snapshots.Snapshot
	if rf, ok := ret.Get(0).(func(int, int, map[string]string) []snapshots.Snapshot); ok {
		r0 = rf(limit, offset, filters)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]snapshots.Snapshot)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(int, int, map[string]string) error); ok {
		r1 = rf(limit, offset, filters)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// CreateSnapshot provides a mock function with given fields: name, volID, description, tags
func (_m *OpenStackMock) CreateSnapshot(name string, volID string, description string, tags *map[string]string) (*snapshots.Snapshot, error) {
	ret := _m.Called(name, volID, description, tags)

	var r0 *snapshots.Snapshot
	if rf, ok := ret.Get(0).(func(string, string, string, *map[string]string) *snapshots.Snapshot); ok {
		r0 = rf(name, volID, description, tags)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*snapshots.Snapshot)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string, string, string, *map[string]string) error); ok {
		r1 = rf(name, volID, description, tags)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// DeleteSnapshot provides a mock function with given fields: snapID
func (_m *OpenStackMock) DeleteSnapshot(snapID string) error {
	ret := _m.Called(snapID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string) error); ok {
		r0 = rf(snapID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// ListVolumes provides a mock function without param
func (_m *OpenStackMock) ListVolumes() ([]volumes.Volume, error) {
	ret := _m.Called()
	var vlist []volumes.Volume

	var r0 error
	if rf, ok := ret.Get(0).(func() error); ok {
		r0 = rf()
	} else {
		r0 = ret.Error(0)
	}

	return vlist, r0
}

func (_m *OpenStackMock) GetSnapshotByNameAndVolumeID(n string, volumeId string) ([]snapshots.Snapshot, error) {
	var slist []snapshots.Snapshot
	slist = append(slist, fakeSnapshot)
	return slist, nil
}

func (_m *OpenStackMock) GetAvailabilityZone() (string, error) {
	ret := _m.Called()
	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func (_m *OpenStackMock) GetInstanceID() (string, error) {
	return "", nil
}

func (_m *OpenStackMock) GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error) {

	return &fakeSnapshot, nil
}

func (_m *OpenStackMock) WaitSnapshotReady(snapshotID string) error {
	ret := _m.Called(snapshotID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string) error); ok {
		r0 = rf(snapshotID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
