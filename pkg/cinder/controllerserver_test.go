package cinder

import (
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/cinder/openstack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var fakeCs *controllerServer

// Init Controller Server
func init() {
	if fakeCs == nil {
		d := NewDriver(fakeNodeID, fakeEndpoint)
		fakeCs = NewControllerServer(d)
	}
}

// Test CreateVolume
func TestCreateVolume(t *testing.T) {

	// mock OpenStack
	osmock := new(openstack.OpenStackMock)
	// CreateVolume(name string, size int, vtype, availability string, tags *map[string]string) (string, string, error)
	osmock.On("CreateVolume", fakeVolName, mock.AnythingOfType("int"), fakeVolType, fakeAvailability, (*map[string]string)(nil)).Return(fakeVolID, fakeAvailability, nil)
	openstack.OsInstance = osmock

	// Init assert
	assert := assert.New(t)

	// Fake request
	fakeReq := &csi.CreateVolumeRequest{
		Version:            &version,
		Name:               fakeVolName,
		VolumeCapabilities: nil,
	}

	// Invoke CreateVolume
	actualRes, err := fakeCs.CreateVolume(fakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to CreateVolume: %v", err)
	}

	// Assert
	assert.NotNil(actualRes.VolumeInfo)

	assert.NotEqual(0, len(actualRes.VolumeInfo.Id), "Volume Id is nil")

	assert.Equal(fakeAvailability, actualRes.VolumeInfo.Attributes["availability"])
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
		Version:  &version,
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
		Version:          &version,
		VolumeId:         fakeVolID,
		NodeId:           fakeNodeID,
		VolumeCapability: nil,
		Readonly:         false,
	}

	// Expected Result
	expectedRes := &csi.ControllerPublishVolumeResponse{
		PublishVolumeInfo: map[string]string{
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
		Version:  &version,
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
