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
	"encoding/json"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	sharedcsi "k8s.io/cloud-provider-openstack/pkg/csi"
	openstack "k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
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
	properties := map[string]string{cinderCSIClusterIDKey: FakeCluster}
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
					Segments: map[string]string{topologyKey: FakeAvailability},
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
	properties := map[string]string{cinderCSIClusterIDKey: FakeCluster}
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
					Segments: map[string]string{topologyKey: FakeAvailability},
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
		cinderCSIClusterIDKey:     FakeCluster,
		sharedcsi.PvNameKey:       FakePVName,
		sharedcsi.PvcNameKey:      FakePVCName,
		sharedcsi.PvcNamespaceKey: FakePVCNamespace,
	}
	// CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, sourceBackupID string, tags map[string]string) (string, string, int, error)
	osmock.On("CreateVolume", FakeVolName, mock.AnythingOfType("int"), FakeVolType, FakeAvailability, "", "", "", properties).Return(&FakeVol, nil)

	osmock.On("GetVolumesByName", FakeVolName).Return(FakeVolListEmpty, nil)

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Name: FakeVolName,
		Parameters: map[string]string{
			sharedcsi.PvNameKey:       FakePVName,
			sharedcsi.PvcNameKey:      FakePVCName,
			sharedcsi.PvcNamespaceKey: FakePVCNamespace,
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
					Segments: map[string]string{topologyKey: FakeAvailability},
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
	properties := map[string]string{cinderCSIClusterIDKey: FakeCluster}
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
	properties := map[string]string{cinderCSIClusterIDKey: FakeCluster}
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
	var entries []*csi.ListVolumesResponse_Entry
	for _, fakeVol := range fakeVolumes {
		entries = append(entries, genFakeVolumeEntry(fakeVol))
	}
	return entries
}

func TestListVolumes(t *testing.T) {
	osmock.On("ListVolumes", 2, FakeVolID).Return(FakeVolListMultiple, "", nil)

	// Init assert
	assert := assert.New(t)
	token := CloudsStartingToken{
		CloudName: "",
		Token:     FakeVolID,
	}
	data, _ := json.Marshal(token)
	fakeReq := &csi.ListVolumesRequest{MaxEntries: 2, StartingToken: string(data)}

	// Expected Result
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

type ListVolumeTestOSMock struct {
	//name           string
	mockCloud      *openstack.OpenStackMock
	mockMaxEntries int
	mockVolumes    []volumes.Volume
	mockToken      string
	mockTokenCall  string
}
type ListVolumesTest struct {
	volumeSet     map[string]ListVolumeTestOSMock
	maxEntries    int
	StartingToken string
	Result        ListVolumesTestResult
}
type ListVolumesTestResult struct {
	ExpectedToken CloudsStartingToken
	Entries       []*csi.ListVolumesResponse_Entry
}

func TestGlobalListVolumesMultipleClouds(t *testing.T) {
	//osmock.On("ListVolumes", 2, "").Return([]volumes.Volume{FakeVol1}, "", nil)
	//osmockRegionX.On("ListVolumes", 1, "").Return([]volumes.Volume{FakeVol2}, FakeVol2.ID, nil)

	tests := []*ListVolumesTest{
		{
			// no pagination, no clouds has volumes
			maxEntries:    0,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 0,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 0,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{}),
			},
		},
		{
			// no pagination, all clouds has volumes
			maxEntries:    0,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 0,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
						{ID: "vol3"},
						{ID: "vol4"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 0,
					mockVolumes: []volumes.Volume{
						{ID: "vol5"},
						{ID: "vol6"},
						{ID: "vol7"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
					{ID: "vol3"},
					{ID: "vol4"},
					{ID: "vol5"},
					{ID: "vol6"},
					{ID: "vol7"},
				}),
			},
		},
		{
			// no pagination, only first cloud have volumes
			maxEntries:    0,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 0,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
						{ID: "vol3"},
						{ID: "vol4"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 0,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		{
			// no pagination, first cloud without volumes
			maxEntries:    0,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 0,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 0,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
						{ID: "vol3"},
						{ID: "vol4"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		// PAGINATION
		{
			// no volmues
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{}),
			},
		},
		{
			// cloud1: 1 volume, cloud2: 0 volume
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
				}),
			},
		},
		{
			// cloud1: 0 volume, cloud2: 1 volume
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
				}),
			},
		},
		{
			// cloud1: 2 volume, cloud2: 0 volume
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			// cloud1: 0 volume, cloud2: 2 volume
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes:    []volumes.Volume{},
					mockToken:      "",
					mockTokenCall:  "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			// cloud1: 2 volume, cloud2: 1 volume : 1st call
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "",
					Token:     "",
					isEmpty:   false,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			// cloud1: 2 volume, cloud2: 1 volume : 2nd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"\",\"token\":\"\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 1234,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol3"},
				}),
			},
		},
		{
			// cloud1: 1 volume, cloud2: 2 volume : 1st call
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes: []volumes.Volume{
						{ID: "vol2"},
					},
					mockToken:     "vol2",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "region-x",
					Token:     "vol2",
					isEmpty:   false,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			// cloud1: 1 volume, cloud2: 2 volume : 2nd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"region-x\",\"token\":\"vol2\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 1234,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "",
					mockTokenCall: "vol2",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol3"},
				}),
			},
		},
		{
			// cloud1: 2 volume, cloud2: 2 volume : 1st call
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "vol3",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "",
					Token:     "",
					isEmpty:   false,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			// cloud1: 2 volume, cloud2: 2 volume : 2nd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"\",\"token\":\"\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 1234,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
						{ID: "vol4"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "region-x",
					Token:     "",
					isEmpty:   true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		{
			// cloud1: 3 volume, cloud2: 2 volume : 1st call
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "vol2",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1234,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
						{ID: "vol4"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "",
					Token:     "vol2",
					isEmpty:   false,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			// cloud1: 3 volume, cloud2: 2 volume : 2nd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"\",\"token\":\"vol2\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "",
					mockTokenCall: "vol2",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes: []volumes.Volume{
						{ID: "vol4"},
					},
					mockToken:     "vol4",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "region-x",
					Token:     "vol4",
					isEmpty:   false,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		{
			// cloud1: 3 volume, cloud2: 2 volume : 3rd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"region-x\",\"token\":\"vol4\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 1234,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol5"},
					},
					mockToken:     "",
					mockTokenCall: "vol4",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "region-x",
					Token:     "",
					isEmpty:   true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol5"},
				}),
			},
		},
		{
			// cloud1: 2 volume, cloud2: 3 volume : 1st call
			maxEntries:    2,
			StartingToken: "",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol1"},
						{ID: "vol2"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "vol3",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "",
					Token:     "",
					isEmpty:   false,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol1"},
					{ID: "vol2"},
				}),
			},
		},
		{
			// cloud1: 3 volume, cloud2: 2 volume : 2nd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"\",\"token\":\"\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 1234,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
						{ID: "vol4"},
					},
					mockToken:     "vol4",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "region-x",
					Token:     "vol4",
					isEmpty:   false,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
		{
			// cloud1: 2 volume, cloud2: 3 volume : 3rd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"region-x\",\"token\":\"vol4\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 1234,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol5"},
					},
					mockToken:     "",
					mockTokenCall: "vol4",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					CloudName: "region-x",
					Token:     "",
					isEmpty:   true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol5"},
				}),
			},
		},
		{
			// cloud1: 3 volume, cloud2: 1 volume : 2rd call
			maxEntries:    2,
			StartingToken: "{\"cloud\":\"\",\"token\":\"vol2\"}",
			volumeSet: map[string]ListVolumeTestOSMock{
				"": {
					mockCloud:      osmock,
					mockMaxEntries: 2,
					mockVolumes: []volumes.Volume{
						{ID: "vol3"},
					},
					mockToken:     "",
					mockTokenCall: "vol2",
				},
				"region-x": {
					mockCloud:      osmockRegionX,
					mockMaxEntries: 1,
					mockVolumes: []volumes.Volume{
						{ID: "vol4"},
					},
					mockToken:     "",
					mockTokenCall: "",
				},
			},
			Result: ListVolumesTestResult{
				ExpectedToken: CloudsStartingToken{
					isEmpty: true,
				},
				Entries: genFakeVolumeEntries([]volumes.Volume{
					{ID: "vol3"},
					{ID: "vol4"},
				}),
			},
		},
	}

	// Init assert
	assert := assert.New(t)
	for _, test := range tests {
		// Setup Mock
		for _, volumeSet := range test.volumeSet {
			cloud := volumeSet.mockCloud
			cloud.On(
				"ListVolumes",
				volumeSet.mockMaxEntries,
				volumeSet.mockTokenCall,
			).Return(
				volumeSet.mockVolumes,
				volumeSet.mockToken,
				nil,
			).Once()
		}
		fakeReq := &csi.ListVolumesRequest{MaxEntries: int32(test.maxEntries), StartingToken: test.StartingToken}
		expectedToken, _ := json.Marshal(test.Result.ExpectedToken)
		if test.Result.ExpectedToken.isEmpty {
			expectedToken = []byte("")
		}
		expectedRes := &csi.ListVolumesResponse{
			Entries:   test.Result.Entries,
			NextToken: string(expectedToken),
		}
		// Invoke ListVolumes
		actualRes, err := fakeCsMultipleClouds.ListVolumes(FakeCtx, fakeReq)
		if err != nil {
			t.Errorf("failed to ListVolumes: %v", err)
		}
		// Assert
		assert.Equal(expectedRes, actualRes)

		// Unset Mock
		for _, volumeSet := range test.volumeSet {
			cloud := volumeSet.mockCloud
			cloud.On(
				"ListVolumes",
				volumeSet.mockMaxEntries,
				volumeSet.mockTokenCall,
			).Return(
				volumeSet.mockVolumes,
				volumeSet.mockToken,
				nil,
			).Unset()
		}
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
		cinderCSIClusterIDKey:               FakeCluster,
		sharedcsi.VolSnapshotNameKey:        FakeSnapshotName,
		sharedcsi.VolSnapshotContentNameKey: FakeSnapshotContentName,
		sharedcsi.VolSnapshotNamespaceKey:   FakeSnapshotNamespace,
		openstack.SnapshotForceCreate:       "true",
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
			sharedcsi.VolSnapshotNameKey:        FakeSnapshotName,
			sharedcsi.VolSnapshotContentNameKey: FakeSnapshotContentName,
			sharedcsi.VolSnapshotNamespaceKey:   FakeSnapshotNamespace,
			openstack.SnapshotForceCreate:       "true",
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
