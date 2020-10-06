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
	"fmt"
	"net/url"
	"time"

	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/apiversions"
	volumeexpand "github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/volumeactions"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/volumeattach"
	"k8s.io/apimachinery/pkg/util/wait"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"

	"k8s.io/klog/v2"
)

const (
	VolumeAvailableStatus    = "available"
	VolumeInUseStatus        = "in-use"
	VolumeDeletedStatus      = "deleted"
	VolumeErrorStatus        = "error"
	operationFinishInitDelay = 1 * time.Second
	operationFinishFactor    = 1.1
	operationFinishSteps     = 10
	diskAttachInitDelay      = 1 * time.Second
	diskAttachFactor         = 1.2
	diskAttachSteps          = 15
	diskDetachInitDelay      = 1 * time.Second
	diskDetachFactor         = 1.2
	diskDetachSteps          = 13
	volumeDescription        = "Created by OpenStack Cinder CSI driver"
)

func (os *OpenStack) CheckBlockStorageAPI() error {
	_, err := apiversions.List(os.blockstorage).AllPages()
	if err != nil {
		return err
	}
	return nil
}

// CreateVolume creates a volume of given size
func (os *OpenStack) CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourcevolID string, tags *map[string]string) (*volumes.Volume, error) {

	opts := &volumes.CreateOpts{
		Name:             name,
		Size:             size,
		VolumeType:       vtype,
		AvailabilityZone: availability,
		Description:      volumeDescription,
		SnapshotID:       snapshotID,
		SourceVolID:      sourcevolID,
	}
	if tags != nil {
		opts.Metadata = *tags
	}

	vol, err := volumes.Create(os.blockstorage, opts).Extract()
	if err != nil {
		return nil, err
	}

	return vol, nil
}

// ListVolumes list all the volumes
func (os *OpenStack) ListVolumes(limit int, startingToken string) ([]volumes.Volume, string, error) {
	nextPageToken := ""
	opts := volumes.ListOpts{Limit: limit, Marker: startingToken}
	pages, err := volumes.List(os.blockstorage, opts).AllPages()
	if err != nil {
		return nil, nextPageToken, err
	}
	vols, err := volumes.ExtractVolumes(pages)
	if err != nil {
		return nil, nextPageToken, err
	}
	nextPageURL, err := pages.NextPageURL()
	if err != nil && nextPageURL != "" {
		if queryParams, nerr := url.ParseQuery(nextPageURL); nerr != nil {
			nextPageToken = queryParams.Get("marker")
		}
	}
	return vols, nextPageToken, nil
}

// GetVolumesByName is a wrapper around ListVolumes that creates a Name filter to act as a GetByName
// Returns a list of Volume references with the specified name
func (os *OpenStack) GetVolumesByName(n string) ([]volumes.Volume, error) {
	opts := volumes.ListOpts{Name: n}
	pages, err := volumes.List(os.blockstorage, opts).AllPages()
	if err != nil {
		return nil, err
	}

	vols, err := volumes.ExtractVolumes(pages)
	if err != nil {
		return nil, err
	}

	return vols, nil
}

// DeleteVolume delete a volume
func (os *OpenStack) DeleteVolume(volumeID string) error {
	used, err := os.diskIsUsed(volumeID)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("Cannot delete the volume %q, it's still attached to a node", volumeID)
	}

	err = volumes.Delete(os.blockstorage, volumeID, nil).ExtractErr()
	return err
}

// GetVolume retrieves Volume by its ID.
func (os *OpenStack) GetVolume(volumeID string) (*volumes.Volume, error) {

	vol, err := volumes.Get(os.blockstorage, volumeID).Extract()
	if err != nil {
		return nil, err
	}

	return vol, nil
}

// AttachVolume attaches given cinder volume to the compute
func (os *OpenStack) AttachVolume(instanceID, volumeID string) (string, error) {
	computeServiceClient := os.compute

	volume, err := os.GetVolume(volumeID)
	if err != nil {
		return "", err
	}

	for _, att := range volume.Attachments {
		if instanceID == att.ServerID {
			klog.V(4).Infof("Disk %s is already attached to instance %s", volumeID, instanceID)
			return volume.ID, nil
		}
	}

	if volume.Multiattach {
		// For multiattach volumes, supported compute api version is 2.60
		// Init a local thread safe copy of the compute ServiceClient
		computeServiceClient, err = openstack.NewComputeV2(os.compute.ProviderClient, os.epOpts)
		if err != nil {
			return "", err
		}
		computeServiceClient.Microversion = "2.60"
	}

	_, err = volumeattach.Create(computeServiceClient, instanceID, &volumeattach.CreateOpts{
		VolumeID: volume.ID,
	}).Extract()

	if err != nil {
		return "", fmt.Errorf("failed to attach %s volume to %s compute: %v", volumeID, instanceID, err)
	}

	return volume.ID, nil
}

// WaitDiskAttached waits for attched
func (os *OpenStack) WaitDiskAttached(instanceID string, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskAttachInitDelay,
		Factor:   diskAttachFactor,
		Steps:    diskAttachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := os.diskIsAttached(instanceID, volumeID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			// if this is a race condition indicate the volume is deleted
			// during sleep phase, ignore the error and return attach=false
			return false, err
		}
		return attached, nil
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("Volume %q failed to be attached within the alloted time", volumeID)
	}

	return err
}

// DetachVolume detaches given cinder volume from the compute
func (os *OpenStack) DetachVolume(instanceID, volumeID string) error {
	volume, err := os.GetVolume(volumeID)
	if err != nil {
		return err
	}
	if volume.Status == VolumeAvailableStatus {
		klog.V(2).Infof("volume: %s has been detached from compute: %s ", volume.ID, instanceID)
		return nil
	}

	if volume.Status != VolumeInUseStatus {
		return fmt.Errorf("can not detach volume %s, its status is %s", volume.Name, volume.Status)
	}

	// Incase volume is of type multiattach, it could be attached to more than one instance
	for _, att := range volume.Attachments {
		if att.ServerID == instanceID {
			err = volumeattach.Delete(os.compute, instanceID, volume.ID).ExtractErr()
			if err != nil {
				return fmt.Errorf("failed to detach volume %s from compute %s : %v", volume.ID, instanceID, err)
			}
			klog.V(2).Infof("Successfully detached volume: %s from compute: %s", volume.ID, instanceID)
			return nil
		}
	}

	return fmt.Errorf("disk: %s has no attachments or not attached to compute %s", volume.ID, instanceID)
}

// WaitDiskDetached waits for detached
func (os *OpenStack) WaitDiskDetached(instanceID string, volumeID string) error {
	backoff := wait.Backoff{
		Duration: diskDetachInitDelay,
		Factor:   diskDetachFactor,
		Steps:    diskDetachSteps,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		attached, err := os.diskIsAttached(instanceID, volumeID)
		if err != nil {
			return false, err
		}
		return !attached, nil
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("Volume %q failed to detach within the alloted time", volumeID)
	}

	return err
}

// GetAttachmentDiskPath gets device path of attached volume to the compute
func (os *OpenStack) GetAttachmentDiskPath(instanceID, volumeID string) (string, error) {
	volume, err := os.GetVolume(volumeID)
	if err != nil {
		return "", err
	}
	if volume.Status != VolumeInUseStatus {
		return "", fmt.Errorf("can not get device path of volume %s, its status is %s ", volume.Name, volume.Status)
	}

	if len(volume.Attachments) > 0 {
		for _, att := range volume.Attachments {
			if att.ServerID == instanceID {
				return att.Device, nil
			}
		}
		return "", fmt.Errorf("disk %q is not attached to compute: %q", volumeID, instanceID)
	}
	return "", fmt.Errorf("volume %s has no Attachments", volumeID)
}

// ExpandVolume expands the volume to new size
func (os *OpenStack) ExpandVolume(volumeID string, newSize int) error {
	volume, err := os.GetVolume(volumeID)
	if err != nil {
		return err
	}

	createOpts := volumeexpand.ExtendSizeOpts{
		NewSize: newSize,
	}

	switch volume.Status {
	case VolumeInUseStatus:
		// Init a local thread safe copy of the Cinder ServiceClient
		blockstorageClient, err := openstack.NewBlockStorageV3(os.blockstorage.ProviderClient, os.epOpts)
		if err != nil {
			return err
		}

		// cinder online resize is available since 3.42 microversion
		// https://docs.openstack.org/cinder/latest/contributor/api_microversion_history.html#id40
		blockstorageClient.Microversion = "3.42"

		return volumeexpand.ExtendSize(blockstorageClient, volumeID, createOpts).ExtractErr()
	case VolumeAvailableStatus:
		return volumeexpand.ExtendSize(os.blockstorage, volumeID, createOpts).ExtractErr()
	}

	// cinder volume can not be expanded when volume status is not volumeInUseStatus or not volumeAvailableStatus
	return fmt.Errorf("volume cannot be resized, when status is %s", volume.Status)
}

//GetMaxVolLimit returns max vol limit
func (os *OpenStack) GetMaxVolLimit() int64 {
	if os.bsOpts.NodeVolumeAttachLimit > 0 && os.bsOpts.NodeVolumeAttachLimit <= 256 {
		return os.bsOpts.NodeVolumeAttachLimit
	}

	return defaultMaxVolAttachLimit
}

// diskIsAttached queries if a volume is attached to a compute instance
func (os *OpenStack) diskIsAttached(instanceID, volumeID string) (bool, error) {
	volume, err := os.GetVolume(volumeID)
	if err != nil {
		return false, err
	}
	for _, att := range volume.Attachments {
		if att.ServerID == instanceID {
			return true, nil
		}
	}
	return false, nil
}

// diskIsUsed returns true a disk is attached to any node.
func (os *OpenStack) diskIsUsed(volumeID string) (bool, error) {
	volume, err := os.GetVolume(volumeID)
	if err != nil {
		return false, err
	}

	if len(volume.Attachments) > 0 {
		return volume.Attachments[0].ServerID != "", nil
	}

	return false, nil
}

// GetBlockStorageOpts returns OpenStack block storage options
func (os *OpenStack) GetBlockStorageOpts() BlockStorageOpts {
	return os.bsOpts
}
