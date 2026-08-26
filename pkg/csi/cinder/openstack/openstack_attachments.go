/*
Copyright 2026 The Kubernetes Authors.

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
	"context"

	gophercloud "github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/attachments"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/klog/v2"
)

// blockStorageClientWithMicroversion returns a shallow copy of the
// pre-built blockstorage ServiceClient with the given microversion.
func (os *OpenStack) blockStorageClientWithMicroversion(mv string) *gophercloud.ServiceClient {
	c := *os.blockstorage // shallow copy
	c.Microversion = mv
	return &c
}

// AttachmentCreate creates a Cinder volume attachment using the
// attachment API (microversion 3.27+). When a Connector map is
// provided the attachment is created in a single step and the
// connection_info is returned directly.
//
// Returns (attachmentID, connectionInfo, error).
func (os *OpenStack) AttachmentCreate(ctx context.Context, volumeID string, instanceID string, connectorProperties map[string]any) (string, map[string]any, error) {
	blockstorageClient := os.blockStorageClientWithMicroversion("3.27")

	opts := attachments.CreateOpts{
		VolumeUUID:   volumeID,
		InstanceUUID: instanceID,
		Connector:    connectorProperties,
	}

	mc := metrics.NewMetricContext("volume_attachment", "create")
	att, err := attachments.Create(ctx, blockstorageClient, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		klog.Errorf("Failed to create attachment for volume %s: %v", volumeID, err)
		return "", nil, err
	}

	klog.V(4).Infof("Created attachment %s for volume %s", att.ID, volumeID)
	return att.ID, att.ConnectionInfo, nil
}

// AttachmentDelete deletes a Cinder volume attachment (microversion
// 3.27+). Only the attachment ID is required, no connector properties.
func (os *OpenStack) AttachmentDelete(ctx context.Context, attachmentID string) error {
	blockstorageClient := os.blockStorageClientWithMicroversion("3.27")

	mc := metrics.NewMetricContext("volume_attachment", "delete")
	err := attachments.Delete(ctx, blockstorageClient, attachmentID).ExtractErr()
	if mc.ObserveRequest(err) != nil {
		klog.Errorf("Failed to delete attachment %s: %v", attachmentID, err)
		return err
	}

	klog.V(4).Infof("Deleted attachment %s", attachmentID)
	return nil
}

// AttachmentComplete marks a Cinder volume attachment as "in-use"
// (microversion 3.44+). This tells Cinder the volume is actually
// connected on the host.
func (os *OpenStack) AttachmentComplete(ctx context.Context, attachmentID string) error {
	blockstorageClient := os.blockStorageClientWithMicroversion("3.44")

	mc := metrics.NewMetricContext("volume_attachment", "complete")
	err := attachments.Complete(ctx, blockstorageClient, attachmentID).ExtractErr()
	if mc.ObserveRequest(err) != nil {
		klog.Errorf("Failed to complete attachment %s: %v", attachmentID, err)
		return err
	}

	klog.V(4).Infof("Completed attachment %s", attachmentID)
	return nil
}

// ResetVolumeStatus force-resets a Cinder volume's status.
// This is needed when a volume is stuck in a transient state
// (e.g. "attaching") with no attachments, which can happen when
// a previous attachment operation was interrupted.
func (os *OpenStack) ResetVolumeStatus(ctx context.Context, volumeID string, targetStatus string) error {
	blockstorageClient := os.blockStorageClientWithMicroversion("")

	opts := volumes.ResetStatusOpts{
		Status: targetStatus,
	}

	mc := metrics.NewMetricContext("volume", "reset_status")
	err := volumes.ResetStatus(ctx, blockstorageClient, volumeID, opts).ExtractErr()
	if mc.ObserveRequest(err) != nil {
		klog.Errorf("Failed to reset volume %s status to %s: %v", volumeID, targetStatus, err)
		return err
	}

	klog.V(4).Infof("Reset volume %s status to %s", volumeID, targetStatus)
	return nil
}
