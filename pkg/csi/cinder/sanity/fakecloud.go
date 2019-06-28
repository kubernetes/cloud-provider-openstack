package sanity

import (
	"math/rand"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
)

type cloud struct {
	volumes   map[string]*volumes.Volume
	snapshots map[string]*snapshots.Snapshot
	instances map[string]*servers.Server
}

func getfakecloud() *cloud {
	return &cloud{
		volumes:   make(map[string]*volumes.Volume, 0),
		snapshots: make(map[string]*snapshots.Snapshot, 0),
		instances: make(map[string]*servers.Server, 0),
	}
}

// Fake Cloud
func (cloud *cloud) CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (*volumes.Volume, error) {

	vol := &volumes.Volume{
		ID:               randString(10),
		Name:             name,
		Status:           "available",
		Size:             size,
		VolumeType:       vtype,
		AvailabilityZone: availability,
		SnapshotID:       snapshotID,
	}

	cloud.volumes[vol.ID] = vol
	return vol, nil
}

func (cloud *cloud) DeleteVolume(volumeID string) error {
	// delete the volume from cloud struct
	delete(cloud.volumes, volumeID)

	return nil

}

func (cloud *cloud) AttachVolume(instanceID, volumeID string) (string, error) {
	// update the volume with attachement

	vol, ok := cloud.volumes[volumeID]

	if ok {
		att := volumes.Attachment{
			ServerID: instanceID,
			VolumeID: volumeID,
		}

		vol.Attachments = append(vol.Attachments, att)

		return vol.ID, nil
	}

	return "", notFoundError()
}

func (cloud *cloud) ListVolumes() ([]volumes.Volume, error) {

	var vollist []volumes.Volume

	for _, value := range cloud.volumes {
		vollist = append(vollist, *value)
	}
	return vollist, nil

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
	var vlist []volumes.Volume
	for _, v := range cloud.volumes {
		if v.Name == name {
			vlist = append(vlist, *v)

		}
	}

	return vlist, nil

}

func (cloud *cloud) GetVolume(volumeID string) (*volumes.Volume, error) {
	vol, ok := cloud.volumes[volumeID]

	if !ok {
		return nil, notFoundError()
	}

	return vol, nil
}

func notFoundError() error {
	return gophercloud.ErrDefault404{}
}

func (cloud *cloud) CreateSnapshot(name, volID string, tags *map[string]string) (*snapshots.Snapshot, error) {

	snap := &snapshots.Snapshot{
		ID:        randString(10),
		Name:      name,
		Status:    "Available",
		VolumeID:  volID,
		CreatedAt: time.Now(),
	}

	cloud.snapshots[snap.ID] = snap
	return snap, nil
}

func (cloud *cloud) ListSnapshots(limit, offset int, filters map[string]string) ([]snapshots.Snapshot, error) {
	var snaplist []snapshots.Snapshot

	for _, value := range cloud.snapshots {
		snaplist = append(snaplist, *value)
	}
	return snaplist, nil

}

func (cloud *cloud) DeleteSnapshot(snapID string) error {

	delete(cloud.snapshots, snapID)

	return nil

}

func (cloud *cloud) GetSnapshotByNameAndVolumeID(n string, volumeId string) ([]snapshots.Snapshot, error) {

	var vlist []snapshots.Snapshot

	for _, v := range cloud.snapshots {
		if v.Name == n {
			vlist = append(vlist, *v)

		}
	}

	return vlist, nil

}

func (cloud *cloud) GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error) {

	snap, ok := cloud.snapshots[snapshotID]

	if !ok {
		return nil, notFoundError()
	}

	return snap, nil
}

func (cloud *cloud) WaitSnapshotReady(snapshotID string) error {
	return nil
}

func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func (cloud *cloud) GetInstanceByID(instanceID string) (*servers.Server, error) {
	inst, ok := cloud.instances[instanceID]

	if !ok {
		return nil, gophercloud.ErrDefault404{}
	}

	return inst, nil
}

func (cloud *cloud) ExpandVolume(volumeID string, size int) error {
	return nil
}

func (cloud *cloud) GetMaxVolLimit() int64 {
	return 256
}
