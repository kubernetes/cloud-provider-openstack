package sanity

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/backups"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
)

type cloud struct {
	volumes   map[string]*volumes.Volume
	snapshots map[string]*snapshots.Snapshot
	instances map[string]*servers.Server
	backups   map[string]*backups.Backup
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
func (cloud *cloud) CreateVolume(_ context.Context, opts *volumes.CreateOpts, _ volumes.SchedulerHintOptsBuilder) (*volumes.Volume, error) {
	vol := &volumes.Volume{
		ID:               randString(10),
		Name:             opts.Name,
		Size:             opts.Size,
		Status:           "available",
		VolumeType:       opts.VolumeType,
		AvailabilityZone: opts.AvailabilityZone,
		SnapshotID:       opts.SnapshotID,
		SourceVolID:      opts.SourceVolID,
		BackupID:         &opts.BackupID,
	}

	cloud.volumes[vol.ID] = vol
	return vol, nil
}

func (cloud *cloud) DeleteVolume(_ context.Context, volumeID string) error {
	// delete the volume from cloud struct
	delete(cloud.volumes, volumeID)

	return nil
}

func (cloud *cloud) AttachVolume(_ context.Context, instanceID, volumeID string) (string, error) {
	// update the volume with attachment

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

func (cloud *cloud) ListVolumes(_ context.Context, limit int, marker string) ([]volumes.Volume, string, error) {

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

func (cloud *cloud) WaitDiskAttached(_ context.Context, instanceID string, volumeID string) error {
	return nil
}

func (cloud *cloud) DetachVolume(_ context.Context, instanceID, volumeID string) error {
	return nil
}

func (cloud *cloud) WaitDiskDetached(_ context.Context, instanceID string, volumeID string) error {
	return nil
}

func (cloud *cloud) WaitVolumeTargetStatus(_ context.Context, volumeID string, tStatus []string) error {
	return nil
}

func (cloud *cloud) GetAttachmentDiskPath(_ context.Context, instanceID, volumeID string) (string, error) {
	return cinder.FakeDevicePath, nil
}

func (cloud *cloud) GetVolumesByName(_ context.Context, name string) ([]volumes.Volume, error) {
	var vlist []volumes.Volume
	for _, v := range cloud.volumes {
		if v.Name == name {
			vlist = append(vlist, *v)

		}
	}

	return vlist, nil
}

func (cloud *cloud) GetVolumeByName(ctx context.Context, n string) (*volumes.Volume, error) {
	vols, err := cloud.GetVolumesByName(ctx, n)
	if err != nil {
		return nil, err
	}

	if len(vols) == 0 {
		return nil, errors.ErrNotFound
	}

	if len(vols) > 1 {
		return nil, fmt.Errorf("found %d volumes with name %q", len(vols), n)
	}

	return &vols[0], nil
}

func (cloud *cloud) GetVolume(_ context.Context, volumeID string) (*volumes.Volume, error) {
	vol, ok := cloud.volumes[volumeID]

	if !ok {
		return nil, notFoundError()
	}

	return vol, nil
}

func notFoundError() error {
	return gophercloud.ErrUnexpectedResponseCode{Actual: 404}
}

func invalidError() error {
	return gophercloud.ErrUnexpectedResponseCode{Actual: 400}
}

func (cloud *cloud) CreateSnapshot(_ context.Context, name, volID string, tags map[string]string) (*snapshots.Snapshot, error) {

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

func (cloud *cloud) ListSnapshots(_ context.Context, filters map[string]string) ([]snapshots.Snapshot, string, error) {
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

func (cloud *cloud) DeleteSnapshot(_ context.Context, snapID string) error {

	delete(cloud.snapshots, snapID)

	return nil
}

func (cloud *cloud) GetSnapshotByID(_ context.Context, snapshotID string) (*snapshots.Snapshot, error) {

	snap, ok := cloud.snapshots[snapshotID]

	if !ok {
		return nil, notFoundError()
	}

	return snap, nil
}

func (cloud *cloud) WaitSnapshotReady(_ context.Context, snapshotID string) (string, error) {
	return "available", nil
}

func (cloud *cloud) CreateBackup(_ context.Context, name, volID, snapshotID, availabilityZone string, tags map[string]string) (*backups.Backup, error) {

	backup := &backups.Backup{
		ID:               randString(10),
		Name:             name,
		Status:           "available",
		VolumeID:         volID,
		SnapshotID:       snapshotID,
		AvailabilityZone: &availabilityZone,
		CreatedAt:        time.Now(),
	}

	cloud.backups[backup.ID] = backup
	return backup, nil
}

func (cloud *cloud) ListBackups(_ context.Context, filters map[string]string) ([]backups.Backup, error) {
	var backuplist []backups.Backup
	startingToken := filters["Marker"]
	limitfilter := filters["Limit"]
	limit, _ := strconv.Atoi(limitfilter)
	name := filters["Name"]
	volumeID := filters["VolumeID"]

	for _, value := range cloud.backups {
		if volumeID != "" {
			if value.VolumeID == volumeID {
				backuplist = append(backuplist, *value)
				break
			}
		} else if name != "" {
			if value.Name == name {
				backuplist = append(backuplist, *value)
				break
			}
		} else {
			backuplist = append(backuplist, *value)
		}
	}

	if startingToken != "" {
		t, _ := strconv.Atoi(startingToken)
		backuplist = backuplist[t:]
	}
	if limit != 0 {
		backuplist = backuplist[:limit]
	}

	return backuplist, nil
}

func (cloud *cloud) DeleteBackup(_ context.Context, backupID string) error {
	delete(cloud.backups, backupID)

	return nil
}

func (cloud *cloud) GetBackupByID(_ context.Context, backupID string) (*backups.Backup, error) {
	backup, ok := cloud.backups[backupID]

	if !ok {
		return nil, notFoundError()
	}

	return backup, nil
}

func (cloud *cloud) BackupsAreEnabled() (bool, error) {
	return true, nil
}

func (cloud *cloud) WaitBackupReady(_ context.Context, backupID string, snapshotSize int, backupMaxDurationSecondsPerGB int) (string, error) {
	return "", nil
}

func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func (cloud *cloud) GetInstanceByID(_ context.Context, instanceID string) (*servers.Server, error) {
	if _, ok := cloud.instances[cinder.FakeInstanceID]; !ok {
		cloud.instances[cinder.FakeInstanceID] = &servers.Server{}
	}
	inst, ok := cloud.instances[instanceID]

	if !ok {
		return nil, gophercloud.ErrUnexpectedResponseCode{Actual: 404}
	}

	return inst, nil
}

func (cloud *cloud) ExpandVolume(_ context.Context, volumeID string, status string, size int) error {
	return nil
}

func (cloud *cloud) GetMaxVolLimit() int64 {
	return 256
}

func (cloud *cloud) GetMetadataOpts() metadata.Opts {
	var m metadata.Opts
	m.SearchOrder = fmt.Sprintf("%s,%s", "configDrive", "metadataService")
	return m
}

func (cloud *cloud) GetBlockStorageOpts() openstack.BlockStorageOpts {
	return openstack.BlockStorageOpts{}
}

func (cloud *cloud) ResolveVolumeListToUUIDs(_ context.Context, v string) (string, error) {
	return v, nil
}
