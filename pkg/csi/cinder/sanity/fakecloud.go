package sanity

import (
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
)

type cloud struct {
}

// Fake Cloud
func (cloud *cloud) CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (*volumes.Volume, error) {

	return &cinder.FakeVol1, nil
}

func (cloud *cloud) DeleteVolume(volumeID string) error {
	return nil

}

func (cloud *cloud) AttachVolume(instanceID, volumeID string) (string, error) {
	return cinder.FakeVolID, nil
}

func (cloud *cloud) ListVolumes() ([]volumes.Volume, error) {
	return cinder.FakeVolList, nil

}

func (cloud *cloud) WaitDiskAttached(instanceID string, volumeID string) error {
	return nil

}

func (cloud *cloud) DetachVolume(instanceID, volumeID string) error {
	return nil

}

func (cloud *cloud) WaitDiskDetached(instanceID string, volumeID string) error {
	return nil

}
func (cloud *cloud) GetAttachmentDiskPath(instanceID, volumeID string) (string, error) {
	return cinder.FakeDevicePath, nil

}
func (cloud *cloud) GetVolumesByName(name string) ([]volumes.Volume, error) {

	return cinder.FakeVolList, nil

}
func (cloud *cloud) CreateSnapshot(name, volID, description string, tags *map[string]string) (*snapshots.Snapshot, error) {
	return &cinder.FakeSnapshotRes, nil
}
func (cloud *cloud) ListSnapshots(limit, offset int, filters map[string]string) ([]snapshots.Snapshot, error) {
	return cinder.FakeSnapshotsRes, nil
}
func (cloud *cloud) DeleteSnapshot(snapID string) error {
	return nil

}
func (cloud *cloud) GetSnapshotByNameAndVolumeID(n string, volumeId string) ([]snapshots.Snapshot, error) {
	return cinder.FakeSnapshotsRes, nil
}

func (cloud *cloud) GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error) {
	return &cinder.FakeSnapshotRes, nil
}

func (cloud *cloud) WaitSnapshotReady(snapshotID string) error {
	return nil
}
