/*
Copyright 2024 The Kubernetes Authors.

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

	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/backups"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
)

type NoopOpenStack struct {
	bsOpts       BlockStorageOpts
	metadataOpts metadata.Opts
}

func (os *NoopOpenStack) CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourcevolID string, tags *map[string]string) (*volumes.Volume, error) {
	return nil, fmt.Errorf("CreateVolume is not implemented for ephemeral storage in this configuration")
}

func (os *NoopOpenStack) ListVolumes(limit int, startingToken string) ([]volumes.Volume, string, error) {
	return nil, "", nil
}

func (os *NoopOpenStack) GetVolumesByName(n string) ([]volumes.Volume, error) {
	return nil, nil
}

func (os *NoopOpenStack) DeleteVolume(volumeID string) error {
	return nil
}

func (os *NoopOpenStack) GetVolume(volumeID string) (*volumes.Volume, error) {
	return &volumes.Volume{ID: volumeID}, nil
}

func (os *NoopOpenStack) AttachVolume(instanceID, volumeID string) (string, error) {
	return volumeID, nil
}

func (os *NoopOpenStack) WaitDiskAttached(instanceID string, volumeID string) error {
	return nil
}

func (os *NoopOpenStack) WaitVolumeTargetStatus(volumeID string, tStatus []string) error {
	return nil
}

func (os *NoopOpenStack) DetachVolume(instanceID, volumeID string) error {
	return nil
}

func (os *NoopOpenStack) WaitDiskDetached(instanceID string, volumeID string) error {
	return nil
}

func (os *NoopOpenStack) GetAttachmentDiskPath(instanceID, volumeID string) (string, error) {
	return "", nil
}

func (os *NoopOpenStack) ExpandVolume(volumeID string, status string, newSize int) error {
	return nil
}

func (os *NoopOpenStack) GetMaxVolLimit() int64 {
	if os.bsOpts.NodeVolumeAttachLimit > 0 && os.bsOpts.NodeVolumeAttachLimit <= 256 {
		return os.bsOpts.NodeVolumeAttachLimit
	}

	return defaultMaxVolAttachLimit
}

func (os *NoopOpenStack) GetBlockStorageOpts() BlockStorageOpts {
	return os.bsOpts
}

func (os *NoopOpenStack) GetMetadataOpts() metadata.Opts {
	return os.metadataOpts
}

func (os *NoopOpenStack) CreateBackup(name, volID, snapshotID, availabilityZone string, tags map[string]string) (*backups.Backup, error) {
	return &backups.Backup{}, nil
}

func (os *NoopOpenStack) BackupsAreEnabled() (bool, error) {
	return false, nil
}

func (os *NoopOpenStack) DeleteBackup(backupID string) error {
	return nil
}

func (os *NoopOpenStack) CreateSnapshot(name, volID string, tags *map[string]string) (*snapshots.Snapshot, error) {
	return &snapshots.Snapshot{}, nil
}

func (os *NoopOpenStack) DeleteSnapshot(snapID string) error {
	return nil
}

func (os *NoopOpenStack) GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error) {
	return &snapshots.Snapshot{ID: snapshotID}, nil
}

func (os *NoopOpenStack) ListSnapshots(filters map[string]string) ([]snapshots.Snapshot, string, error) {
	return nil, "", nil
}

func (os *NoopOpenStack) WaitSnapshotReady(snapshotID string) error {
	return nil
}

func (os *NoopOpenStack) GetBackupByID(backupID string) (*backups.Backup, error) {
	return &backups.Backup{ID: backupID}, nil
}

func (os *NoopOpenStack) ListBackups(filters map[string]string) ([]backups.Backup, error) {
	return nil, nil
}

func (os *NoopOpenStack) WaitBackupReady(backupID string, snapshotSize int, backupMaxDurationSecondsPerGB int) (string, error) {
	return "", nil
}

func (os *NoopOpenStack) GetInstanceByID(instanceID string) (*servers.Server, error) {
	return &servers.Server{ID: instanceID}, nil
}
