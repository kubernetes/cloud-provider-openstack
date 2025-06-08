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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	sharedcsi "k8s.io/cloud-provider-openstack/pkg/csi"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/cloud-provider-openstack/pkg/util/mount"
)

func fakeNodeServer() (*nodeServer, *openstack.OpenStackMock, *mount.MountMock, *metadata.MetadataMock) {
	d := NewDriver(&DriverOpts{Endpoint: FakeEndpoint, ClusterID: FakeCluster, WithTopology: true})

	osmock := new(openstack.OpenStackMock)
	openstack.OsInstances = map[string]openstack.IOpenStack{
		"": osmock,
	}

	mmock := new(mount.MountMock)
	mount.MInstance = mmock

	metamock := new(metadata.MetadataMock)
	metadata.MetadataService = metamock

	opts := openstack.BlockStorageOpts{
		RescanOnResize:        false,
		NodeVolumeAttachLimit: maxVolumesPerNode,
	}

	fakeNs := NewNodeServer(d, mount.MInstance, metadata.MetadataService, opts, map[string]string{})

	return fakeNs, osmock, mmock, metamock
}

// Test NodeGetInfo
func TestNodeGetInfo(t *testing.T) {
	fakeNs, omock, _, metamock := fakeNodeServer()

	metamock.On("GetInstanceID").Return(FakeNodeID, nil)
	metamock.On("GetAvailabilityZone").Return(FakeAvailability, nil)
	omock.On("GetMaxVolumeLimit").Return(FakeMaxVolume)

	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodeGetInfoResponse{
		NodeId:             FakeNodeID,
		AccessibleTopology: &csi.Topology{Segments: map[string]string{topologyKey: FakeAvailability}},
		MaxVolumesPerNode:  FakeMaxVolume,
	}

	// Fake request
	fakeReq := &csi.NodeGetInfoRequest{}

	// Invoke NodeGetInfo
	actualRes, err := fakeNs.NodeGetInfo(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeGetInfo: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test NodePublishVolume
func TestNodePublishVolume(t *testing.T) {
	fakeNs, omock, mmock, _ := fakeNodeServer()

	mmock.On("ScanForAttach", FakeDevicePath).Return(nil)
	mmock.On("IsLikelyNotMountPointAttach", FakeTargetPath).Return(true, nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)

	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodePublishVolumeResponse{}
	stdVolCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{
			Mount: &csi.VolumeCapability_MountVolume{},
		},
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
	}
	// Fake request
	fakeReq := &csi.NodePublishVolumeRequest{
		VolumeId:          FakeVolID,
		PublishContext:    map[string]string{"DevicePath": FakeDevicePath},
		TargetPath:        FakeTargetPath,
		StagingTargetPath: FakeStagingTargetPath,
		VolumeCapability:  stdVolCap,
		Readonly:          false,
	}

	// Invoke NodePublishVolume
	actualRes, err := fakeNs.NodePublishVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodePublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func TestNodePublishVolumeEphemeral(t *testing.T) {
	fakeNs, omock, _, _ := fakeNodeServer()

	properties := map[string]string{cinderCSIClusterIDKey: FakeCluster}
	fvolName := fmt.Sprintf("ephemeral-%s", FakeVolID)

	omock.On("CreateVolume", fvolName, 2, "test", "nova", "", "", "", properties).Return(&FakeVol, nil)

	assert := assert.New(t)

	// Expected Result
	stdVolCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{
			Mount: &csi.VolumeCapability_MountVolume{},
		},
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
	}

	// Fake request
	fakeReq := &csi.NodePublishVolumeRequest{
		VolumeId:         FakeVolID,
		PublishContext:   map[string]string{"DevicePath": FakeDevicePath},
		TargetPath:       FakeTargetPath,
		VolumeCapability: stdVolCap,
		Readonly:         false,
		VolumeContext:    map[string]string{"capacity": "2Gi", sharedcsi.VolEphemeralKey: "true", "type": "test"},
	}

	// Invoke NodePublishVolume
	_, err := fakeNs.NodePublishVolume(FakeCtx, fakeReq)
	assert.Equal(status.Error(codes.Unimplemented, "CSI inline ephemeral volumes support is removed in 1.31 release."), err)
}

// Test NodeStageVolume
func TestNodeStageVolume(t *testing.T) {
	fakeNs, omock, mmock, _ := fakeNodeServer()

	mmock.On("GetDevicePath", FakeVolID).Return(FakeDevicePath, nil)
	mmock.On("IsLikelyNotMountPointAttach", FakeStagingTargetPath).Return(true, nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)

	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodeStageVolumeResponse{}
	stdVolCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{
			Mount: &csi.VolumeCapability_MountVolume{},
		},
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
	}

	// Fake request
	fakeReq := &csi.NodeStageVolumeRequest{
		VolumeId:          FakeVolID,
		PublishContext:    map[string]string{"DevicePath": FakeDevicePath},
		StagingTargetPath: FakeStagingTargetPath,
		VolumeCapability:  stdVolCap,
	}

	// Invoke NodeStageVolume
	actualRes, err := fakeNs.NodeStageVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeStageVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func TestNodeStageVolumeBlock(t *testing.T) {
	fakeNs, _, mmock, _ := fakeNodeServer()

	mmock.On("GetDevicePath", FakeVolID).Return(FakeDevicePath, nil)

	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodeStageVolumeResponse{}
	stdVolCap := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Block{
			Block: &csi.VolumeCapability_BlockVolume{},
		},
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
	}

	// Fake request
	fakeReq := &csi.NodeStageVolumeRequest{
		VolumeId:          FakeVolID,
		PublishContext:    map[string]string{"DevicePath": FakeDevicePath},
		StagingTargetPath: FakeStagingTargetPath,
		VolumeCapability:  stdVolCap,
	}

	// Invoke NodeStageVolume
	actualRes, err := fakeNs.NodeStageVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeStageVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test NodeUnpublishVolume
func TestNodeUnpublishVolume(t *testing.T) {
	fakeNs, omock, mmock, _ := fakeNodeServer()

	mmock.On("UnmountPath", FakeTargetPath).Return(nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)

	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodeUnpublishVolumeResponse{}

	// Fake request
	fakeReq := &csi.NodeUnpublishVolumeRequest{
		VolumeId:   FakeVolID,
		TargetPath: FakeTargetPath,
	}

	// Invoke NodeUnpublishVolume
	actualRes, err := fakeNs.NodeUnpublishVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeUnpublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test NodeUnstageVolume
func TestNodeUnstageVolume(t *testing.T) {
	fakeNs, omock, mmock, _ := fakeNodeServer()

	mmock.On("UnmountPath", FakeStagingTargetPath).Return(nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)

	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodeUnstageVolumeResponse{}

	// Fake request
	fakeReq := &csi.NodeUnstageVolumeRequest{
		VolumeId:          FakeVolID,
		StagingTargetPath: FakeStagingTargetPath,
	}

	// Invoke NodeUnstageVolume
	actualRes, err := fakeNs.NodeUnstageVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeUnstageVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

func TestNodeExpandVolume(t *testing.T) {
	fakeNs, _, _, _ := fakeNodeServer()

	assert := assert.New(t)

	// setup for test
	tempDir := os.TempDir()
	volumePath := filepath.Join(tempDir, FakeTargetPath)
	err := os.MkdirAll(volumePath, 0750)
	if err != nil {
		t.Fatalf("Failed to set up volumepath: %v", err)
	}
	defer os.RemoveAll(volumePath)

	// Fake request
	fakeReq := &csi.NodeExpandVolumeRequest{
		VolumeId:   FakeVolName,
		VolumePath: volumePath,
	}

	// Expected Result
	expectedRes := &csi.NodeExpandVolumeResponse{}

	// Invoke NodeExpandVolume
	actualRes, err := fakeNs.NodeExpandVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to ExpandVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)

}

func TestNodeGetVolumeStatsBlock(t *testing.T) {
	fakeNs, _, mmock, _ := fakeNodeServer()

	tempDir := os.TempDir()
	volumePath := filepath.Join(tempDir, FakeTargetPath)

	mmock.On("GetDeviceStats", volumePath).Return(FakeBlockDeviceStats, nil)

	assert := assert.New(t)

	// setup for test
	err := os.MkdirAll(volumePath, 0750)
	if err != nil {
		t.Fatalf("Failed to set up volumepath: %v", err)
	}
	defer os.RemoveAll(volumePath)

	// Fake request
	fakeReq := &csi.NodeGetVolumeStatsRequest{
		VolumeId:   FakeVolName,
		VolumePath: volumePath,
	}

	expectedBlockRes := &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{Total: FakeBlockDeviceStats.TotalBytes, Unit: csi.VolumeUsage_BYTES},
		},
	}

	blockRes, err := fakeNs.NodeGetVolumeStats(FakeCtx, fakeReq)

	assert.NoError(err)
	assert.Equal(expectedBlockRes, blockRes)

}

func TestNodeGetVolumeStatsFs(t *testing.T) {
	fakeNs, _, mmock, _ := fakeNodeServer()

	assert := assert.New(t)

	// setup for test
	tempDir := os.TempDir()
	volumePath := filepath.Join(tempDir, FakeTargetPath)
	err := os.MkdirAll(volumePath, 0750)
	if err != nil {
		t.Fatalf("Failed to set up volumepath: %v", err)
	}
	defer os.RemoveAll(volumePath)

	// Fake request
	fakeReq := &csi.NodeGetVolumeStatsRequest{
		VolumeId:   FakeVolName,
		VolumePath: volumePath,
	}

	mmock.On("GetDeviceStats", volumePath).Return(FakeFsStats, nil)
	expectedFsRes := &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{Total: FakeFsStats.TotalBytes, Available: FakeFsStats.AvailableBytes, Used: FakeFsStats.UsedBytes, Unit: csi.VolumeUsage_BYTES},
			{Total: FakeFsStats.TotalInodes, Available: FakeFsStats.AvailableInodes, Used: FakeFsStats.UsedInodes, Unit: csi.VolumeUsage_INODES},
		},
	}

	fsRes, err := fakeNs.NodeGetVolumeStats(FakeCtx, fakeReq)

	assert.NoError(err)
	assert.Equal(expectedFsRes, fsRes)

}
