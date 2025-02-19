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
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

var fakeCs *controllerServer
var fakeCsMultipleClouds *controllerServer
var osmock *openstack.OpenStackMock
var osmockRegionX *openstack.OpenStackMock

// Init Controller Server
func init() {
	if fakeCs == nil {
		osmock = new(openstack.OpenStackMock)
		osmockRegionX = new(openstack.OpenStackMock)

		d := NewDriver(&DriverOpts{Endpoint: FakeEndpoint, ClusterID: FakeCluster, WithTopology: true})

		fakeCs = NewControllerServer(d, map[string]openstack.IOpenStack{
			"": osmock,
		})
		fakeCsMultipleClouds = NewControllerServer(d, map[string]openstack.IOpenStack{
			"":         osmock,
			"region-x": osmockRegionX,
		})
	}
}

// Test CreateVolume
func TestCreateVolume(t *testing.T) {
	// mock OpenStack
	properties := map[string]string{"cinder.csi.openstack.org/cluster": FakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, sourceBackupID string, tags map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, FakeAvailability, "", "", "", properties).Return(&FakeVol, nil)

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

// Test CreateVolume with additional param
func TestCreateVolumeWithParam(t *testing.T) {
	// mock OpenStack
	properties := map[string]string{"cinder.csi.openstack.org/cluster": FakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, sourceBackupID string, tags map[string]string) (string, string, int, error)
	// Vol type and availability comes from CreateVolumeRequest.Parameters
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), "dummyVolType", "cinder", "", "", "", properties).Return(&FakeVol, nil)

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

		Parameters: map[string]string{
			"availability": "cinder",
			"type":         "dummyVolType",
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

func TestCreateVolumeWithExtraMetadata(t *testing.T) {
	// mock OpenStack
	properties := map[string]string{
		"cinder.csi.openstack.org/cluster": FakeCluster,
		"csi.storage.k8s.io/pv/name":       FakePVName,
		"csi.storage.k8s.io/pvc/name":      FakePVCName,
		"csi.storage.k8s.io/pvc/namespace": FakePVCNamespace,
	}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, sourceBackupID string, tags map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, FakeAvailability, "", "", "", properties).Return(&FakeVol, nil)

	osmock.On("GetVolumesByName", FakeVolName).Return(FakeVolListEmpty, nil)

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name: FakeVolName,
		Parameters: map[string]string{
			"csi.storage.k8s.io/pv/name":       FakePVName,
			"csi.storage.k8s.io/pvc/name":      FakePVCName,
			"csi.storage.k8s.io/pvc/namespace": FakePVCNamespace,
		},
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
	_, err := fakeCs.CreateVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

}

func TestCreateVolumeFromSnapshot(t *testing.T) {
	properties := map[string]string{"cinder.csi.openstack.org/cluster": FakeCluster}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, sourceBackupID string, tags map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, "", FakeSnapshotID, "", "", properties).Return(&FakeVolFromSnapshot, nil)
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
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, sourceBackupID string, tags map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, "", "", FakeVolID, "", properties).Return(&FakeVolFromSourceVolume, nil)
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

	// Expected result
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
	osmock.On("AttachVolume", FakeNodeID, FakeVolID).Return(FakeVolID, nil)
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

	// Expected result
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

	// Expected result
	expectedRes := &csi.ControllerUnpublishVolumeResponse{}

	// Invoke ControllerUnpublishVolume
	actualRes, err := fakeCs.ControllerUnpublishVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ControllerUnpublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func extractFakeNodeIDs(attachments []volumes.Attachment) []string {
	nodeIDs := make([]string, len(attachments))
	for i, attachment := range attachments {
		nodeIDs[i] = attachment.ServerID
	}
	return nodeIDs
}

func genFakeVolumeEntry(fakeVol volumes.Volume) *csi.ListVolumesResponse_Entry {
	return &csi.ListVolumesResponse_Entry{
		Volume: &csi.Volume{
			VolumeId:      fakeVol.ID,
			CapacityBytes: int64(fakeVol.Size * 1024 * 1024 * 1024),
		},
		Status: &csi.ListVolumesResponse_VolumeStatus{
			PublishedNodeIds: extractFakeNodeIDs(fakeVol.Attachments),
		},
	}
}
func genFakeVolumeEntries(fakeVolumes []volumes.Volume) []*csi.ListVolumesResponse_Entry {
	entries := make([]*csi.ListVolumesResponse_Entry, 0, len(fakeVolumes))
	for _, fakeVol := range fakeVolumes {
		entries = append(entries, genFakeVolumeEntry(fakeVol))
	}
	return entries
}

func TestListVolumes(t *testing.T) {
	osmock.On("ListVolumes", 2, FakeVolID).Return(FakeVolListMultiple, "", nil)

	// Init assert
	assert := assert.New(t)
	fakeReq := &csi.ListVolumesRequest{MaxEntries: 2, StartingToken: FakeVolID}

	// Expected result
	expectedRes := &csi.ListVolumesResponse{
		Entries:   genFakeVolumeEntries(FakeVolListMultiple),
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

type ListVolumesTest struct {
	name          string
	maxEntries    int
	startingToken string
	volumeSet     map[string]ListVolumeTestOSMock
	result        ListVolumesTestResult
}

type ListVolumeTestOSMock struct {
	mockCloud      *openstack.OpenStackMock
	mockTokenReq   string
	mockVolumesRes []volumes.Volume
	mockTokenRes   string
}

type ListVolumesTestResult struct {
	ExpectedToken string
	Entries       []*csi.ListVolumesResponse_Entry
}

func TestGlobalListVolumesMultipleClouds(t *testing.T) {
	tests := []*ListVolumesTest{
		{
			name:       "Single cloud, no volume",
			maxEntries: 0,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockVolumesRes: []volumes.Volume{},
				},
			},
			result: ListVolumesTestResult{
				Entries: genFakeVolumeEntries([]volumes.Volume{}),
			},
		},
		{
			name:       "Single cloud, with token",
			maxEntries: 0,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud: osmock,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockTokenRes: "vol2",
				},
			},

			result: ListVolumesTestResult{
				ExpectedToken: "vol2",
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			name:       "Single cloud, no token",
			maxEntries: 0,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud: osmock,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
						{ID: "vol3"},
						{ID: "vol4"},
					},
				},
			},

			result: ListVolumesTestResult{
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		{
			name:       "Multiple clouds, no volumes",
			maxEntries: 0,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockVolumesRes: []volumes.Volume{},
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockVolumesRes: []volumes.Volume{},
				},
			},
			result: ListVolumesTestResult{
				ExpectedToken: ":region-x",
				Entries:       genFakeVolumeEntries([]volumes.Volume{}),
			},
		},
		{
			name:          "Multiple clouds, no volumes, 2nd request",
			startingToken: ":region-x",
			maxEntries:    0,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockVolumesRes: []volumes.Volume{},
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockVolumesRes: []volumes.Volume{},
				},
			},
			result: ListVolumesTestResult{
				Entries: genFakeVolumeEntries([]volumes.Volume{}),
			},
		},
		{
			name:       "Multiple clouds",
			maxEntries: 0,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud: osmock,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
						{ID: "vol3"},
						{ID: "vol4"},
					},
				},
				"region-x": {
					mockCloud: osmockRegionX,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol5"},
						{ID: "vol6"},
						{ID: "vol7"},
					},
				},
			},
			result: ListVolumesTestResult{
				ExpectedToken: ":region-x",
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		{
			name:          "Multiple clouds, 2nd request",
			maxEntries:    0,
			startingToken: ":region-x",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud: osmock,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
						{ID: "vol3"},
						{ID: "vol4"},
					},
				},
				"region-x": {
					mockCloud: osmockRegionX,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol5"},
						{ID: "vol6"},
						{ID: "vol7"},
					},
				},
			},
			result: ListVolumesTestResult{
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol5"},
					{ID: "vol6"},
					{ID: "vol7"},
				}),
			},
		},
		// PAGINATION
		{
			name:       "Pagination, no volumes",
			maxEntries: 2,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockVolumesRes: []volumes.Volume{},
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockVolumesRes: []volumes.Volume{},
				},
			},
			result: ListVolumesTestResult{
				ExpectedToken: ":region-x",
				Entries:       genFakeVolumeEntries([]volumes.Volume{}),
			},
		},
		{
			name:       "Pagination",
			maxEntries: 2,
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud: osmock,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
				},
				"region-x": {
					mockCloud: osmockRegionX,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol3"},
						{ID: "vol4"},
						{ID: "vol5"},
					},
				},
			},
			result: ListVolumesTestResult{
				ExpectedToken: ":region-x",
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			name:          "Pagination, 2nd request",
			maxEntries:    2,
			startingToken: ":region-x",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud: osmock,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
				},
				"region-x": {
					mockCloud: osmockRegionX,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol3"},
						{ID: "vol4"},
					},
					mockTokenRes: "vol4",
				},
			},
			result: ListVolumesTestResult{
				ExpectedToken: "vol4:region-x",
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		{
			name:          "Pagination, 3rd request",
			maxEntries:    2,
			startingToken: "vol4:region-x",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud: osmock,
					mockVolumesRes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
				},
				"region-x": {
					mockCloud:    osmockRegionX,
					mockTokenReq: "vol4",
					mockVolumesRes: []volumes.Volume{
						{ID: "vol5"},
					},
				},
			},
			result: ListVolumesTestResult{
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol5"},
				}),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Init assert
			assert := assert.New(t)
			// Setup Mock
			for _, volumeSet := range test.volumeSet {
				cloud := volumeSet.mockCloud
				cloud.On(
					"ListVolumes",
					test.maxEntries,
					volumeSet.mockTokenReq,
				).Return(
					volumeSet.mockVolumesRes,
					volumeSet.mockTokenRes,
					nil,
				).Once()
				defer cloud.On(
					"ListVolumes",
					test.maxEntries,
					volumeSet.mockTokenReq,
				).Return(
					volumeSet.mockVolumesRes,
					volumeSet.mockTokenRes,
					nil,
				).Unset()
			}
			var err error
			fakeReq := &csi.ListVolumesRequest{MaxEntries: int32(test.maxEntries), StartingToken: test.startingToken}
			expectedRes := &csi.ListVolumesResponse{
				Entries:   test.result.Entries,
				NextToken: test.result.ExpectedToken,
			}
			// Invoke ListVolumes
			cs := fakeCs
			if len(test.volumeSet) > 1 {
				cs = fakeCsMultipleClouds
			}
			actualRes, err := cs.ListVolumes(FakeCtx, fakeReq)
			if err != nil {
				t.Errorf("failed to ListVolumes: %v", err)
			}
			// Assert
			assert.Equal(expectedRes, actualRes)
		})
	}
}

// Test CreateSnapshot
func TestCreateSnapshot(t *testing.T) {

	osmock.On("CreateSnapshot", FakeSnapshotName, FakeVolID, map[string]string{cinderCSIClusterIDKey: "cluster"}).Return(&FakeSnapshotRes, nil)
	osmock.On("ListSnapshots", map[string]string{"Name": FakeSnapshotName}).Return(FakeSnapshotListEmpty, "", nil)
	osmock.On("WaitSnapshotReady", FakeSnapshotID).Return(FakeSnapshotRes.Status, nil)
	osmock.On("ListBackups", map[string]string{"Name": FakeSnapshotName}).Return(FakeBackupListEmpty, nil)
	osmock.On("GetSnapshotByID", FakeVolID).Return(&FakeSnapshotRes, nil)
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

// Test CreateSnapshot with extra metadata
func TestCreateSnapshotWithExtraMetadata(t *testing.T) {
	properties := map[string]string{
		"cinder.csi.openstack.org/cluster":              FakeCluster,
		"csi.storage.k8s.io/volumesnapshot/name":        FakeSnapshotName,
		"csi.storage.k8s.io/volumesnapshotcontent/name": FakeSnapshotContentName,
		"csi.storage.k8s.io/volumesnapshot/namespace":   FakeSnapshotNamespace,
		openstack.SnapshotForceCreate:                   "true",
	}

	osmock.On("CreateSnapshot", FakeSnapshotName, FakeVolID, properties).Return(&FakeSnapshotRes, nil)
	osmock.On("ListSnapshots", map[string]string{"Name": FakeSnapshotName}).Return(FakeSnapshotListEmpty, "", nil)
	osmock.On("WaitSnapshotReady", FakeSnapshotID).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.CreateSnapshotRequest{
		Name:           FakeSnapshotName,
		SourceVolumeId: FakeVolID,
		Parameters: map[string]string{
			"csi.storage.k8s.io/volumesnapshot/name":        FakeSnapshotName,
			"csi.storage.k8s.io/volumesnapshotcontent/name": FakeSnapshotContentName,
			"csi.storage.k8s.io/volumesnapshot/namespace":   FakeSnapshotNamespace,
			openstack.SnapshotForceCreate:                   "true",
		},
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
	osmock.On("DeleteBackup", FakeSnapshotID).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.DeleteSnapshotRequest{
		SnapshotId: FakeSnapshotID,
	}

	// Expected result
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
	tState := []string{"available", "in-use"}
	// ExpandVolume(volumeID string, status string, size int)
	osmock.On("ExpandVolume", FakeVolID, openstack.VolumeAvailableStatus, 5).Return(nil)

	// WaitVolumeTargetStatus(volumeID string, tState []string) error
	osmock.On("WaitVolumeTargetStatus", FakeVolID, tState).Return(nil)

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.ControllerExpandVolumeRequest{
		VolumeId: FakeVolID,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 5 * 1024 * 1024 * 1024,
		},
	}

	// Expected result
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

	expectedRes2 := &csi.ValidateVolumeCapabilitiesResponse{Message: "Requested Volume Capability not supported"}

	// Invoke ValidateVolumeCapabilities
	actualRes, err := fakeCs.ValidateVolumeCapabilities(FakeCtx, fakereq)
	if err != nil {
		t.Errorf("failed to ValidateVolumeCapabilities: %v", err)
	}

	actualRes2, err := fakeCs.ValidateVolumeCapabilities(FakeCtx, fakereq2)

	if err != nil {
		t.Errorf("failed to ValidateVolumeCapabilities: %v", err)
	}

	// assert
	assert.Equal(expectedRes, actualRes)
	assert.Equal(expectedRes2, actualRes2)

}
