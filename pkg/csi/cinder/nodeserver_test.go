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
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/mount"
)

var fakeNs *nodeServer
var mmock *mount.MountMock
var omock *openstack.OpenStackMock

// Init Node Server
func init() {
	if fakeNs == nil {

		d := NewDriver(FakeNodeID, FakeEndpoint, FakeCluster)

		// mock MountMock
		mmock = new(mount.MountMock)
		mount.MInstance = mmock

		omock = new(openstack.OpenStackMock)
		openstack.MetadataService = omock
		openstack.OsInstance = omock
		fakeNs = NewNodeServer(d, mount.MInstance, openstack.MetadataService, openstack.OsInstance)
	}
}

// Test NodeGetInfo
func TestNodeGetInfo(t *testing.T) {

	// GetInstanceID() (string, error)
	mmock.On("GetInstanceID").Return(FakeNodeID, nil)

	omock.On("GetAvailabilityZone").Return(FakeAvailability, nil)

	omock.On("GetMaxVolumeLimit").Return(FakeMaxVolume)

	// Init assert
	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodeGetInfoResponse{
		NodeId:             FakeNodeID,
		AccessibleTopology: &csi.Topology{Segments: map[string]string{topologyKey: FakeAvailability}},
		MaxVolumesPerNode:  FakeMaxVolume,
	}

	// Fake request
	fakeReq := &csi.NodeGetInfoRequest{}

	// Invoke NodeGetId
	actualRes, err := fakeNs.NodeGetInfo(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeGetInfo: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test NodePublishVolume
func TestNodePublishVolume(t *testing.T) {

	// ScanForAttach(devicePath string) error
	mmock.On("ScanForAttach", FakeDevicePath).Return(nil)
	// IsLikelyNotMountPointAttach(targetpath string) (bool, error)
	mmock.On("IsLikelyNotMountPointAttach", FakeTargetPath).Return(true, nil)
	// Mount(source string, target string, fstype string, options []string) error
	mmock.On("Mount", FakeStagingTargetPath, FakeTargetPath, mock.AnythingOfType("string"), []string{"bind", "rw"}).Return(nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)
	// Init assert
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

func TestNodePublishVolumeEphermeral(t *testing.T) {

	properties := map[string]string{"cinder.csi.openstack.org/cluster": FakeCluster}
	fvolName := fmt.Sprintf("ephemeral-%s", FakeVolID)

	omock.On("CreateVolume", fvolName, 2, "", "", "", "", &properties).Return(&FakeVol, nil)

	omock.On("AttachVolume", FakeNodeID, FakeVolID).Return(FakeVolID, nil)
	omock.On("WaitDiskAttached", FakeNodeID, FakeVolID).Return(nil)
	mmock.On("GetDevicePath", FakeVolID).Return(FakeDevicePath, nil)
	mmock.On("IsLikelyNotMountPointAttach", FakeTargetPath).Return(true, nil)
	mmock.On("FormatAndMount", FakeDevicePath, FakeTargetPath, "ext4", []string(nil)).Return(nil)

	mount.MInstance = mmock
	openstack.MetadataService = omock
	openstack.OsInstance = omock

	d := NewDriver(FakeNodeID, FakeEndpoint, FakeCluster)
	fakeNse := NewNodeServer(d, mount.MInstance, openstack.MetadataService, openstack.OsInstance)

	// Init assert
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
		VolumeId:         FakeVolID,
		PublishContext:   map[string]string{"DevicePath": FakeDevicePath},
		TargetPath:       FakeTargetPath,
		VolumeCapability: stdVolCap,
		Readonly:         false,
		VolumeContext:    map[string]string{"capacity": "2Gi", "csi.storage.k8s.io/ephemeral": "true"},
	}

	// Invoke NodePublishVolume
	actualRes, err := fakeNse.NodePublishVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodePublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test NodeStageVolume
func TestNodeStageVolume(t *testing.T) {

	// GetDevicePath(volumeID string) error
	mmock.On("GetDevicePath", FakeVolID).Return(FakeDevicePath, nil)
	// IsLikelyNotMountPointAttach(targetpath string) (bool, error)
	mmock.On("IsLikelyNotMountPointAttach", FakeStagingTargetPath).Return(true, nil)
	// FormatAndMount(source string, target string, fstype string, options []string) error
	mmock.On("FormatAndMount", FakeDevicePath, FakeStagingTargetPath, "ext4", []string(nil)).Return(nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)

	// Init assert
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

	// Init assert
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

	// IsLikelyNotMountPointDetach(targetpath string) (bool, error)
	mmock.On("IsLikelyNotMountPointDetach", FakeTargetPath).Return(false, nil)
	// UnmountPath(mountPath string) error
	mmock.On("UnmountPath", FakeTargetPath).Return(nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)

	// Init assert
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

func TestNodeUnpublishVolumeEphermeral(t *testing.T) {

	mount.MInstance = mmock
	openstack.MetadataService = omock
	openstack.OsInstance = omock
	fvolName := fmt.Sprintf("ephemeral-%s", FakeVolID)

	mmock.On("IsLikelyNotMountPointDetach", FakeTargetPath).Return(false, nil)
	mmock.On("UnmountPath", FakeTargetPath).Return(nil)
	omock.On("GetVolumesByName", fvolName).Return(FakeVolList, nil)
	omock.On("DetachVolume", FakeNodeID, FakeVolID).Return(nil)
	omock.On("WaitDiskDetached", FakeNodeID, FakeVolID).Return(nil)
	omock.On("DeleteVolume", FakeVolID).Return(nil)

	d := NewDriver(FakeNodeID, FakeEndpoint, FakeCluster)
	fakeNse := NewNodeServer(d, mount.MInstance, openstack.MetadataService, openstack.OsInstance)

	// Init assert
	assert := assert.New(t)

	// Expected Result
	expectedRes := &csi.NodeUnpublishVolumeResponse{}

	// Fake request
	fakeReq := &csi.NodeUnpublishVolumeRequest{
		VolumeId:   FakeVolID,
		TargetPath: FakeTargetPath,
	}

	// Invoke NodeUnpublishVolume
	actualRes, err := fakeNse.NodeUnpublishVolume(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeUnpublishVolume: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}

// Test NodeUnstageVolume
func TestNodeUnstageVolume(t *testing.T) {

	// IsLikelyNotMountPointDetach(targetpath string) (bool, error)
	mmock.On("IsLikelyNotMountPointDetach", FakeStagingTargetPath).Return(false, nil)
	// UnmountPath(mountPath string) error
	mmock.On("UnmountPath", FakeStagingTargetPath).Return(nil)
	omock.On("GetVolume", FakeVolID).Return(FakeVol, nil)

	// Init assert
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

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.NodeExpandVolumeRequest{
		VolumeId:   FakeVolName,
		VolumePath: FakeDevicePath,
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

func TestNodeGetVolumeStats(t *testing.T) {

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.NodeGetVolumeStatsRequest{
		VolumeId:   FakeVolName,
		VolumePath: FakeDevicePath,
	}

	// Invoke NodeGetVolumeStats
	_, err := fakeNs.NodeGetVolumeStats(FakeCtx, fakeReq)
	assert.Equal(status.Error(codes.Internal, "Unable to statfs target /dev/xxx, err: no such file or directory"), err)
}
