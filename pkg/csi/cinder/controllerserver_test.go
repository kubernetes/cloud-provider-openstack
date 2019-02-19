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

package cinder

import (
	"flag"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

var fakeCs *controllerServer

// Init Controller Server
func init() {
	if fakeCs == nil {
		// to avoid annoying ERROR: logging before flag.Parse
		flag.Parse()

		d := NewDriver(fakeNodeID, fakeEndpoint, fakeCluster, fakeConfig)
		fakeCs = NewControllerServer(d)
	}
}

// Test CreateVolume
func TestCreateVolume(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	properties := map[string]string{"cinder.csi.openstack.org/cluster": fakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", fakeVolName, mock.AnythingOfType("int"), fakeVolType, fakeAvailability, "", &properties).Return(fakeVolID, fakeAvailability, fakeCapacityGiB, nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name:               fakeVolName,
		VolumeCapabilities: nil,
		AccessibilityRequirements: &csi.TopologyRequirement{
			Requisite: []*csi.Topology{
				{
					Segments: map[string]string{"topology.cinder.csi.openstack.org/zone": fakeAvailability},
				},
			},
		},
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.Volume)
	assert.NotNil(actualRes.Volume.CapacityBytes)
	assert.NotEqual(0, len(actualRes.Volume.VolumeId), "Volume Id is nil")
	assert.NotNil(actualRes.Volume.AccessibleTopology)
	assert.Equal(fakeAvailability, actualRes.Volume.AccessibleTopology[0].GetSegments()[topologyKey])

}

func TestCreateVolumeFromSnapshot(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	properties := map[string]string{"cinder.csi.openstack.org/cluster": fakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", fakeVolName, mock.AnythingOfType("int"), fakeVolType, "", fakeSnapshotID, &properties).Return(fakeVolID, fakeAvailability, fakeCapacityGiB, nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	src := &csi.VolumeContentSource{
		Type: &csi.VolumeContentSource_Snapshot{
			Snapshot: &csi.VolumeContentSource_SnapshotSource{
				SnapshotId: fakeSnapshotID,
			},
		},
	}

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name:                fakeVolName,
		VolumeCapabilities:  nil,
		VolumeContentSource: src,
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.Volume)

	assert.NotNil(actualRes.Volume.CapacityBytes)

	assert.NotEqual(0, len(actualRes.Volume.VolumeId), "Volume Id is nil")

	assert.Equal(fakeSnapshotID, actualRes.Volume.ContentSource.GetSnapshot().SnapshotId)

}

// Test CreateVolumeDuplicate
func TestCreateVolumeDuplicate(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)

	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name:               "fake-duplicate",
		VolumeCapabilities: nil,
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.Volume)
	assert.NotEqual(0, len(actualRes.Volume.VolumeId), "Volume Id is nil")
	assert.Equal("nova", actualRes.Volume.AccessibleTopology[0].GetSegments()[topologyKey])
	assert.Equal("261a8b81-3660-43e5-bab8-6470b65ee4e9", actualRes.Volume.VolumeId)
}

// Test DeleteVolume
func TestDeleteVolume(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	// DeleteVolume(volumeID string) error
	osmock.On("DeleteVolume", fakeVolID).Return(nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.DeleteVolumeRequest{
		VolumeId: fakeVolID,
	}

	// Expected Result
	expectedRes := &csi.DeleteVolumeResponse{}

	// Invoke DeleteVolume
	actualRes, err := fakeCs.DeleteVolume(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to DeleteVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test ControllerPublishVolume
func TestControllerPublishVolume(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	// AttachVolume(instanceID, volumeID string) (string, error)
	osmock.On("AttachVolume", fakeNodeID, fakeVolID).Return(fakeVolID, nil)
	// WaitDiskAttached(instanceID string, volumeID string) error
	osmock.On("WaitDiskAttached", fakeNodeID, fakeVolID).Return(nil)
	// GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	osmock.On("GetAttachmentDiskPath", fakeNodeID, fakeVolID).Return(fakeDevicePath, nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.ControllerPublishVolumeRequest{
		VolumeId:         fakeVolID,
		NodeId:           fakeNodeID,
		VolumeCapability: nil,
		Readonly:         false,
	}

	// Expected Result
	expectedRes := &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			"DevicePath": fakeDevicePath,
		},
	}

	// Invoke ControllerPublishVolume
	actualRes, err := fakeCs.ControllerPublishVolume(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ControllerPublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test ControllerUnpublishVolume
func TestControllerUnpublishVolume(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	// DetachVolume(instanceID, volumeID string) error
	osmock.On("DetachVolume", fakeNodeID, fakeVolID).Return(nil)
	// WaitDiskDetached(instanceID string, volumeID string) error
	osmock.On("WaitDiskDetached", fakeNodeID, fakeVolID).Return(nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.ControllerUnpublishVolumeRequest{
		VolumeId: fakeVolID,
		NodeId:   fakeNodeID,
	}

	// Expected Result
	expectedRes := &csi.ControllerUnpublishVolumeResponse{}

	// Invoke ControllerUnpublishVolume
	actualRes, err := fakeCs.ControllerUnpublishVolume(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ControllerUnpublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func TestListVolumes(t *testing.T) {
	// mock OpenStack
	osmock := new(openstack.OpenStackMock)

	osmock.On("ListVolumes").Return(nil)

	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	fakeReq := &csi.ListVolumesRequest{}

	// Expected Result
	expectedRes := &csi.ListVolumesResponse{}

	// Invoke ListVolumes
	actualRes, err := fakeCs.ListVolumes(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ListVolumes: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test CreateSnapshot
func TestCreateSnapshot(t *testing.T) {
	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	osmock.On("CreateSnapshot", fakeSnapshotName, fakeVolID, "", &map[string]string{"tag": "tag1"}).Return(&fakeSnapshotRes, nil)
	osmock.On("WaitSnapshotReady", fakeSnapshotID).Return(nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.CreateSnapshotRequest{
		Name:           fakeSnapshotName,
		SourceVolumeId: fakeVolID,
		Parameters:     map[string]string{"tag": "tag1"},
	}

	// Invoke CreateSnapshot
	actualRes, err := fakeCs.CreateSnapshot(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateSnapshot: %v", err)
	}

	// Assert
	assert.Equal(fakeVolID, actualRes.Snapshot.SourceVolumeId)

	assert.NotNil(fakeSnapshotID, actualRes.Snapshot.SnapshotId)
}

// Test DeleteSnapshot
func TestDeleteSnapshot(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	// DeleteSnapshot(volumeID string) error
	osmock.On("DeleteSnapshot", fakeSnapshotID).Return(nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.DeleteSnapshotRequest{
		SnapshotId: fakeSnapshotID,
	}

	// Expected Result
	expectedRes := &csi.DeleteSnapshotResponse{}

	// Invoke DeleteSnapshot
	actualRes, err := fakeCs.DeleteSnapshot(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to DeleteVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func TestListSnapshots(t *testing.T) {
	// mock OpenStack
	osmock := new(openstack.OpenStackMock)

	osmock.On("ListSnapshots", 0, 0, map[string]string{}).Return(fakeSnapshotsRes, nil)

	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	fakeReq := &csi.ListSnapshotsRequest{}

	// Invoke ListVolumes
	actualRes, err := fakeCs.ListSnapshots(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ListSnapshots: %v", err)
	}

	// Assert
	assert.Equal(fakeVolID, actualRes.Entries[0].Snapshot.SourceVolumeId)

	assert.NotNil(fakeSnapshotID, actualRes.Entries[0].Snapshot.SnapshotId)
}
