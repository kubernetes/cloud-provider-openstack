package sanity

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
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

var _ openstack.IOpenStack = &cloud{}

// Fake Cloud
func (cloud *cloud) CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, tags *map[string]string) (*volumes.Volume, error) {

	vol := &volumes.Volume{
		ID:               randString(10),
		Name:             name,
		Status:           "available",
		Size:             size,
		VolumeType:       vtype,
		AvailabilityZone: availability,
		SnapshotID:       snapshotID,
		SourceVolID:      sourceVolID,
	}

	cloud.volumes[vol.ID] = vol
	return vol, nil
}

func (cloud *cloud) DeleteVolume(volumeID string) error {
	// delete the volume from cloud struct
	delete(cloud.volumes, volumeID)

	return nil

}

func (cloud *cloud) CheckBlockStorageAPI() error {
	return nil
}

func (cloud *cloud) AttachVolume(instanceID, volumeID string, readOnly bool) (string, error) {
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

func (cloud *cloud) ListVolumes(limit int, marker string) ([]volumes.Volume, string, error) {

	var vollist []volumes.Volume

	if marker != "" {
		if _, ok := cloud.volumes[marker]; !ok {
			return nil, "", invalidError()
		}
	}

	count := 0
	retToken := ""
	for _, value := range cloud.volumes {
		if limit != 0 && count >= limit {
			retToken = value.ID
			break
		}
		vollist = append(vollist, *value)
		count++

	}
	return vollist, retToken, nil

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

func invalidError() error {
	return gophercloud.ErrDefault400{}
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

func (cloud *cloud) ListSnapshots(filters map[string]string) ([]snapshots.Snapshot, string, error) {
	var snaplist []snapshots.Snapshot
	startingToken := filters["Marker"]
	limitfilter := filters["Limit"]
	limit, _ := strconv.Atoi(limitfilter)
	name := filters["Name"]
	volumeID := filters["VolumeID"]

	for _, value := range cloud.snapshots {
		if volumeID != "" {
			if value.VolumeID == volumeID {
				snaplist = append(snaplist, *value)
				break
			}
		} else if name != "" {
			if value.Name == name {
				snaplist = append(snaplist, *value)
				break
			}
		} else {
			snaplist = append(snaplist, *value)
		}
	}

	if startingToken != "" {
		t, _ := strconv.Atoi(startingToken)
		snaplist = snaplist[t:]
	}

	retToken := ""

	if limit != 0 {
		snaplist = snaplist[:limit]
		retToken = limitfilter
	}

	return snaplist, retToken, nil
}

func (cloud *cloud) DeleteSnapshot(snapID string) error {

	delete(cloud.snapshots, snapID)

	return nil

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
	if _, ok := cloud.instances[cinder.FakeInstanceID]; !ok {
		cloud.instances[cinder.FakeInstanceID] = &servers.Server{}
	}
	inst, ok := cloud.instances[instanceID]

	if !ok {
		return nil, gophercloud.ErrDefault404{}
	}

	return inst, nil
}

func (cloud *cloud) ExpandVolume(volumeID string, status string, size int) error {
	return nil
}

func (cloud *cloud) GetMaxVolLimit() int64 {
	return 256
}

func (cloud *cloud) GetMetadataOpts() metadata.MetadataOpts {
	var m metadata.MetadataOpts
	m.SearchOrder = fmt.Sprintf("%s,%s", "configDrive", "metadataService")
	return m
}

func (cloud *cloud) GetBlockStorageOpts() openstack.BlockStorageOpts {
	return openstack.BlockStorageOpts{}
}
