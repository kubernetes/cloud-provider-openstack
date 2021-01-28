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
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

var fakeCs *controllerServer
var osmock *openstack.OpenStackMock

// Init Controller Server
func init() {
	if fakeCs == nil {
		osmock = new(openstack.OpenStackMock)
		openstack.OsInstance = osmock

		d := NewDriver(FakeNodeID, FakeEndpoint, FakeCluster)

		fakeCs = NewControllerServer(d, openstack.OsInstance)
	}
}

// Test CreateVolume
func TestCreateVolume(t *testing.T) {

	// mock OpenStack
	properties := map[string]string{"cinder.csi.openstack.org/cluster": FakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, FakeAvailability, "", "", &properties).Return(&FakeVol, nil)

	osmock.On("GetVolumesByName", FakeVolName).Return(FakeVolListEmpty, nil)
	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name: FakeVolName,
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},

		AccessibilityRequirements: &csi.TopologyRequirement{
			Requisite: []*csi.Topology{
				{
					Segments: map[string]string{"topology.cinder.csi.openstack.org/zone": FakeAvailability},
				},
			},
		},
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.Volume)
	assert.NotNil(actualRes.Volume.CapacityBytes)
	assert.NotEqual(0, len(actualRes.Volume.VolumeId), "Volume Id is nil")
	assert.NotNil(actualRes.Volume.AccessibleTopology)
	assert.Equal(FakeAvailability, actualRes.Volume.AccessibleTopology[0].GetSegments()[topologyKey])

}

func TestCreateVolumeFromSnapshot(t *testing.T) {

	properties := map[string]string{"cinder.csi.openstack.org/cluster": FakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, "", FakeSnapshotID, "", &properties).Return(&FakeVolFromSnapshot, nil)
	osmock.On("GetVolumesByName", FakeVolName).Return(FakeVolListEmpty, nil)

	// Init assert
	assert := assert.New(t)

	src := &csi.VolumeContentSource{
		Type: &csi.VolumeContentSource_Snapshot{
			Snapshot: &csi.VolumeContentSource_SnapshotSource{
				SnapshotId: FakeSnapshotID,
			},
		},
	}

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name: FakeVolName,
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
		VolumeContentSource: src,
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.Volume)

	assert.NotNil(actualRes.Volume.CapacityBytes)

	assert.NotEqual(0, len(actualRes.Volume.VolumeId), "Volume Id is nil")

	assert.Equal(FakeSnapshotID, actualRes.Volume.ContentSource.GetSnapshot().SnapshotId)

}

func TestCreateVolumeFromSourceVolume(t *testing.T) {

	properties := map[string]string{"cinder.csi.openstack.org/cluster": FakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, "", "", FakeVolID, &properties).Return(&FakeVolFromSourceVolume, nil)
	osmock.On("GetVolumesByName", FakeVolName).Return(FakeVolListEmpty, nil)

	// Init assert
	assert := assert.New(t)

	volsrc := &csi.VolumeContentSource{
		Type: &csi.VolumeContentSource_Volume{
			Volume: &csi.VolumeContentSource_VolumeSource{
				VolumeId: FakeVolID,
			},
		},
	}

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name: FakeVolName,
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
		VolumeContentSource: volsrc,
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.Volume)

	assert.NotNil(actualRes.Volume.CapacityBytes)

	assert.NotEqual(0, len(actualRes.Volume.VolumeId), "Volume Id is nil")

	assert.Equal(FakeVolID, actualRes.Volume.ContentSource.GetVolume().VolumeId)

}

// Test CreateVolumeDuplicate
func TestCreateVolumeDuplicate(t *testing.T) {

	// Init assert
	assert := assert.New(t)

	osmock.On("GetVolumesByName", "fake-duplicate").Return(FakeVolList, nil)

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name: "fake-duplicate",
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.Volume)
	assert.NotEqual(0, len(actualRes.Volume.VolumeId), "Volume Id is nil")
	assert.Equal("nova", actualRes.Volume.AccessibleTopology[0].GetSegments()[topologyKey])
	assert.Equal(FakeVolID, actualRes.Volume.VolumeId)
}

// Test DeleteVolume
func TestDeleteVolume(t *testing.T) {

	// DeleteVolume(volumeID string) error
	osmock.On("DeleteVolume", FakeVolID).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.DeleteVolumeRequest{
		VolumeId: FakeVolID,
	}

	// Expected Result
	expectedRes := &csi.DeleteVolumeResponse{}

	// Invoke DeleteVolume
	actualRes, err := fakeCs.DeleteVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to DeleteVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test ControllerPublishVolume
func TestControllerPublishVolume(t *testing.T) {

	// AttachVolume(instanceID, volumeID string) (string, error)
	osmock.On("AttachVolume", FakeNodeID, FakeVolID, false).Return(FakeVolID, nil)
	// WaitDiskAttached(instanceID string, volumeID string) error
	osmock.On("WaitDiskAttached", FakeNodeID, FakeVolID).Return(nil)
	// GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	osmock.On("GetAttachmentDiskPath", FakeNodeID, FakeVolID).Return(FakeDevicePath, nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.ControllerPublishVolumeRequest{
		VolumeId: FakeVolID,
		NodeId:   FakeNodeID,
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
		Readonly: false,
	}

	// Expected Result
	expectedRes := &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			"DevicePath": FakeDevicePath,
		},
	}

	// Invoke ControllerPublishVolume
	actualRes, err := fakeCs.ControllerPublishVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ControllerPublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test ControllerUnpublishVolume
func TestControllerUnpublishVolume(t *testing.T) {

	// DetachVolume(instanceID, volumeID string) error
	osmock.On("DetachVolume", FakeNodeID, FakeVolID).Return(nil)
	// WaitDiskDetached(instanceID string, volumeID string) error
	osmock.On("WaitDiskDetached", FakeNodeID, FakeVolID).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.ControllerUnpublishVolumeRequest{
		VolumeId: FakeVolID,
		NodeId:   FakeNodeID,
	}

	// Expected Result
	expectedRes := &csi.ControllerUnpublishVolumeResponse{}

	// Invoke ControllerUnpublishVolume
	actualRes, err := fakeCs.ControllerUnpublishVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ControllerUnpublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func TestListVolumes(t *testing.T) {

	osmock.On("ListVolumes", 2, FakeVolID).Return(FakeVolListMultiple, "", nil)

	// Init assert
	assert := assert.New(t)

	fakeReq := &csi.ListVolumesRequest{MaxEntries: 2, StartingToken: FakeVolID}

	// Expected Result
	expectedRes := &csi.ListVolumesResponse{
		Entries: []*csi.ListVolumesResponse_Entry{
			{
				Volume: &csi.Volume{
					VolumeId:      FakeVol1.ID,
					CapacityBytes: int64(FakeVol1.Size * 1024 * 1024 * 1024),
				},
				Status: &csi.ListVolumesResponse_VolumeStatus{
					PublishedNodeIds: []string{FakeNodeID},
				},
			},
			{
				Volume: &csi.Volume{
					VolumeId:      FakeVol3.ID,
					CapacityBytes: int64(FakeVol3.Size * 1024 * 1024 * 1024),
				},
				Status: &csi.ListVolumesResponse_VolumeStatus{},
			},
		},
		NextToken: "",
	}

	// Invoke ListVolumes
	actualRes, err := fakeCs.ListVolumes(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ListVolumes: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test CreateSnapshot
func TestCreateSnapshot(t *testing.T) {

	osmock.On("CreateSnapshot", FakeSnapshotName, FakeVolID, &map[string]string{"tag": "tag1"}).Return(&FakeSnapshotRes, nil)
	osmock.On("ListSnapshots", map[string]string{"Name": FakeSnapshotName}).Return(FakeSnapshotListEmpty, "", nil)
	osmock.On("WaitSnapshotReady", FakeSnapshotID).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.CreateSnapshotRequest{
		Name:           FakeSnapshotName,
		SourceVolumeId: FakeVolID,
		Parameters:     map[string]string{"tag": "tag1"},
	}

	// Invoke CreateSnapshot
	actualRes, err := fakeCs.CreateSnapshot(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateSnapshot: %v", err)
	}

	// Assert
	assert.Equal(FakeVolID, actualRes.Snapshot.SourceVolumeId)

	assert.NotNil(FakeSnapshotID, actualRes.Snapshot.SnapshotId)
}

// Test DeleteSnapshot
func TestDeleteSnapshot(t *testing.T) {

	// DeleteSnapshot(volumeID string) error
	osmock.On("DeleteSnapshot", FakeSnapshotID).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.DeleteSnapshotRequest{
		SnapshotId: FakeSnapshotID,
	}

	// Expected Result
	expectedRes := &csi.DeleteSnapshotResponse{}

	// Invoke DeleteSnapshot
	actualRes, err := fakeCs.DeleteSnapshot(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to DeleteVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func TestListSnapshots(t *testing.T) {

	osmock.On("ListSnapshots", map[string]string{"Limit": "1", "Marker": FakeVolID, "Status": "available"}).Return(FakeSnapshotsRes, "", nil)
	assert := assert.New(t)

	fakeReq := &csi.ListSnapshotsRequest{MaxEntries: 1, StartingToken: FakeVolID}
	actualRes, err := fakeCs.ListSnapshots(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ListSnapshots: %v", err)
	}

	// Assert
	assert.Equal(FakeVolID, actualRes.Entries[0].Snapshot.SourceVolumeId)
	assert.NotNil(FakeSnapshotID, actualRes.Entries[0].Snapshot.SnapshotId)
}

func TestControllerExpandVolume(t *testing.T) {

	// ExpandVolume(volumeID string, status string, size int)
	osmock.On("ExpandVolume", FakeVolName, openstack.VolumeAvailableStatus, 5).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.ControllerExpandVolumeRequest{
		VolumeId: FakeVolName,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 5 * 1024 * 1024 * 1024,
		},
	}

	// Expected Result
	expectedRes := &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         5 * 1024 * 1024 * 1024,
		NodeExpansionRequired: true,
	}

	// Invoke ControllerExpandVolume
	actualRes, err := fakeCs.ControllerExpandVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ExpandVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)

}

func TestValidateVolumeCapabilities(t *testing.T) {

	// GetVolume(volumeID string)
	osmock.On("GetVolume", FakeVolID).Return(FakeVol1)

	// Init assert
	assert := assert.New(t)

	// fake req
	fakereq := &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: FakeVolID,
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
	}

	// expected result
	expectedRes := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
		},
	}

	// Testing Negative case
	fakereq2 := &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: FakeVolID,
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
				},
			},
		},
	}

	expectedRes2 := &csi.ValidateVolumeCapabilitiesResponse{Message: "Requested Volume Capabilty not supported"}

	// Invoke ValidateVolumeCapabilties
	actualRes, err := fakeCs.ValidateVolumeCapabilities(FakeCtx, fakereq)
	actualRes2, err := fakeCs.ValidateVolumeCapabilities(FakeCtx, fakereq2)

	if err != nil {
		t.Errorf("failed to ValidateVolumeCapabilties: %v", err)
	}

	// assert
	assert.Equal(expectedRes, actualRes)
	assert.Equal(expectedRes2, actualRes2)

}
