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
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/backups"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

type controllerServer struct {
	Driver *Driver
	Cloud  openstack.IOpenStack
}

const (
	cinderCSIClusterIDKey = "cinder.csi.openstack.org/cluster"
)

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(4).Infof("CreateVolume: called with args %+v", protosanitizer.StripSecrets(*req))

	// Volume Name
	volName := req.GetName()
	volCapabilities := req.GetVolumeCapabilities()

	if len(volName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[CreateVolume] missing Volume Name")
	}

	if volCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "[CreateVolume] missing Volume capability")
	}

	// Volume Size - Default is 1 GiB
	volSizeBytes := int64(1 * 1024 * 1024 * 1024)
	if req.GetCapacityRange() != nil {
		volSizeBytes = int64(req.GetCapacityRange().GetRequiredBytes())
	}
	volSizeGB := int(util.RoundUpSize(volSizeBytes, 1024*1024*1024))

	// Volume Type
	volType := req.GetParameters()["type"]

	// First check if volAvailability is already specified, if not get preferred from Topology
	// Required, incase vol AZ is different from node AZ
	volAvailability := req.GetParameters()["availability"]
	if volAvailability == "" {
		// Check from Topology
		if req.GetAccessibilityRequirements() != nil {
			volAvailability = util.GetAZFromTopology(topologyKey, req.GetAccessibilityRequirements())
		}
	}

	cloud := cs.Cloud
	ignoreVolumeAZ := cloud.GetBlockStorageOpts().IgnoreVolumeAZ

	// Verify a volume with the provided name doesn't already exist for this tenant
	volumes, err := cloud.GetVolumesByName(volName)
	if err != nil {
		klog.Errorf("Failed to query for existing Volume during CreateVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to get volumes: %v", err)
	}

	if len(volumes) == 1 {
		if volSizeGB != volumes[0].Size {
			return nil, status.Error(codes.AlreadyExists, "Volume Already exists with same name and different capacity")
		}
		klog.V(4).Infof("Volume %s already exists in Availability Zone: %s of size %d GiB", volumes[0].ID, volumes[0].AvailabilityZone, volumes[0].Size)
		return getCreateVolumeResponse(&volumes[0], ignoreVolumeAZ, req.GetAccessibilityRequirements()), nil
	} else if len(volumes) > 1 {
		klog.V(3).Infof("found multiple existing volumes with selected name (%s) during create", volName)
		return nil, status.Error(codes.Internal, "Multiple volumes reported by Cinder with same name")

	}

	// Volume Create
	properties := map[string]string{cinderCSIClusterIDKey: cs.Driver.cluster}
	//Tag volume with metadata if present: https://github.com/kubernetes-csi/external-provisioner/pull/399
	for _, mKey := range []string{"csi.storage.k8s.io/pvc/name", "csi.storage.k8s.io/pvc/namespace", "csi.storage.k8s.io/pv/name"} {
		if v, ok := req.Parameters[mKey]; ok {
			properties[mKey] = v
		}
	}
	content := req.GetVolumeContentSource()
	var snapshotID string
	var sourceVolID string
	var sourceBackupID string
	var backupsAreEnabled bool
	backupsAreEnabled, err = cloud.BackupsAreEnabled()
	klog.V(4).Infof("Backups enabled: %v", backupsAreEnabled)
	if err != nil {
		klog.Errorf("Failed to check if backups are enabled: %v", err)
	}

	if content != nil && content.GetSnapshot() != nil {
		snapshotID = content.GetSnapshot().GetSnapshotId()

		snap, err := cloud.GetSnapshotByID(snapshotID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, err
		}
		// If the snapshot exists but is not yet available, fail.
		if err == nil && snap.Status != "available" {
			return nil, status.Errorf(codes.Unavailable, "VolumeContentSource Snapshot %s is not yet available. status: %s", snapshotID, snap.Status)
		}

		// In case a snapshot is not found
		// check if a Backup with the same ID exists
		if backupsAreEnabled && cpoerrors.IsNotFound(err) {
			var back *backups.Backup
			back, err = cloud.GetBackupByID(snapshotID)
			if err != nil {
				//If there is an error getting the backup as well, fail.
				return nil, status.Errorf(codes.NotFound, "VolumeContentSource Snapshot or Backup with ID %s not found", snapshotID)
			}
			if back.Status != "available" {
				// If the backup exists but is not yet available, fail.
				return nil, status.Errorf(codes.Unavailable, "VolumeContentSource Backup %s is not yet available. status: %s", snapshotID, back.Status)
			}
			// If an available backup is found, create the volume from the backup
			sourceBackupID = snapshotID
			snapshotID = ""
		}
		// In case GetSnapshotByID has error IsNotFound and backups are not enabled
		// TODO: Change 'snapshotID == ""' to '!backupsAreEnabled' when cloud.BackupsAreEnabled() is correctly implemented
		if cpoerrors.IsNotFound(err) && snapshotID == "" {
			return nil, err
		}
	}

	if content != nil && content.GetVolume() != nil {
		sourceVolID = content.GetVolume().GetVolumeId()
		_, err := cloud.GetVolume(sourceVolID)
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				return nil, status.Errorf(codes.NotFound, "Source Volume %s not found", sourceVolID)
			}
			return nil, status.Errorf(codes.Internal, "Failed to retrieve the source volume %s: %v", sourceVolID, err)
		}
	}

	vol, err := cloud.CreateVolume(volName, volSizeGB, volType, volAvailability, snapshotID, sourceVolID, sourceBackupID, properties)
	// When creating a volume from a backup, the response does not include the backupID.
	if sourceBackupID != "" {
		vol.BackupID = &sourceBackupID
	}

	if err != nil {
		klog.Errorf("Failed to CreateVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "CreateVolume failed with error %v", err)
	}

	klog.V(4).Infof("CreateVolume: Successfully created volume %s in Availability Zone: %s of size %d GiB", vol.ID, vol.AvailabilityZone, vol.Size)

	return getCreateVolumeResponse(vol, ignoreVolumeAZ, req.GetAccessibilityRequirements()), nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(4).Infof("DeleteVolume: called with args %+v", protosanitizer.StripSecrets(*req))

	// Volume Delete
	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "DeleteVolume Volume ID must be provided")
	}
	err := cs.Cloud.DeleteVolume(volID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("Volume %s is already deleted.", volID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		klog.Errorf("Failed to DeleteVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "DeleteVolume failed with error %v", err)
	}

	klog.V(4).Infof("DeleteVolume: Successfully deleted volume %s", volID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	klog.V(4).Infof("ControllerPublishVolume: called with args %+v", protosanitizer.StripSecrets(*req))

	// Volume Attach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()
	volumeCapability := req.GetVolumeCapability()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Volume ID must be provided")
	}
	if len(instanceID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Instance ID must be provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Volume capability must be provided")
	}

	_, err := cs.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] get volume failed with error %v", err)
	}

	_, err = cs.Cloud.GetInstanceByID(instanceID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] Instance %s not found", instanceID)
		}
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] GetInstanceByID failed with error %v", err)
	}

	_, err = cs.Cloud.AttachVolume(instanceID, volumeID)
	if err != nil {
		klog.Errorf("Failed to AttachVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] Attach Volume failed with error %v", err)

	}

	err = cs.Cloud.WaitDiskAttached(instanceID, volumeID)
	if err != nil {
		klog.Errorf("Failed to WaitDiskAttached: %v", err)
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] failed to attach volume: %v", err)
	}

	devicePath, err := cs.Cloud.GetAttachmentDiskPath(instanceID, volumeID)
	if err != nil {
		klog.Errorf("Failed to GetAttachmentDiskPath: %v", err)
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] failed to get device path of attached volume: %v", err)
	}

	klog.V(4).Infof("ControllerPublishVolume %s on %s is successful", volumeID, instanceID)

	// Publish Volume Info
	pvInfo := map[string]string{}
	pvInfo["DevicePath"] = devicePath

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: pvInfo,
	}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).Infof("ControllerUnpublishVolume: called with args %+v", protosanitizer.StripSecrets(*req))

	// Volume Detach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerUnpublishVolume] Volume ID must be provided")
	}
	_, err := cs.Cloud.GetInstanceByID(instanceID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because node %s does not exist", volumeID, instanceID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "[ControllerUnpublishVolume] GetInstanceByID failed with error %v", err)
	}

	err = cs.Cloud.DetachVolume(instanceID, volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because it does not exist", volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		klog.Errorf("Failed to DetachVolume: %v", err)
		return nil, status.Errorf(codes.Internal, "ControllerUnpublishVolume Detach Volume failed with error %v", err)
	}

	err = cs.Cloud.WaitDiskDetached(instanceID, volumeID)
	if err != nil {
		klog.Errorf("Failed to WaitDiskDetached: %v", err)
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("ControllerUnpublishVolume assuming volume %s is detached, because it was deleted in the meanwhile", volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "ControllerUnpublishVolume failed with error %v", err)
	}

	klog.V(4).Infof("ControllerUnpublishVolume %s on %s", volumeID, instanceID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(4).Infof("ListVolumes: called with %+#v request", req)

	if req.MaxEntries < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "[ListVolumes] Invalid max entries request %v, must not be negative ", req.MaxEntries)
	}
	maxEntries := int(req.MaxEntries)

	vlist, nextPageToken, err := cs.Cloud.ListVolumes(maxEntries, req.StartingToken)
	if err != nil {
		klog.Errorf("Failed to ListVolumes: %v", err)
		if cpoerrors.IsInvalidError(err) {
			return nil, status.Errorf(codes.Aborted, "[ListVolumes] Invalid request: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "ListVolumes failed with error %v", err)
	}

	ventries := make([]*csi.ListVolumesResponse_Entry, 0, len(vlist))
	for _, v := range vlist {
		ventry := csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      v.ID,
				CapacityBytes: int64(v.Size * 1024 * 1024 * 1024),
			},
		}

		status := &csi.ListVolumesResponse_VolumeStatus{}
		status.PublishedNodeIds = make([]string, 0, len(v.Attachments))
		for _, attachment := range v.Attachments {
			status.PublishedNodeIds = append(status.PublishedNodeIds, attachment.ServerID)
		}
		ventry.Status = status

		ventries = append(ventries, &ventry)
	}

	klog.V(4).Infof("ListVolumes: completed with %d entries and %q next token", len(ventries), nextPageToken)
	return &csi.ListVolumesResponse{
		Entries:   ventries,
		NextToken: nextPageToken,
	}, nil
}

func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.V(4).Infof("CreateSnapshot: called with args %+v", protosanitizer.StripSecrets(*req))

	name := req.Name
	volumeID := req.GetSourceVolumeId()
	snapshotType := req.Parameters[openstack.SnapshotType]
	filters := map[string]string{"Name": name}
	backupMaxDurationSecondsPerGB := openstack.BackupMaxDurationSecondsPerGBDefault

	// Current time, used for CreatedAt
	var ctime *timestamppb.Timestamp
	// Size of the created snapshot, used to calculate the amount of time to wait for the backup to finish
	var snapSize int
	// If true, skips creating a snapshot because a backup already exists
	var backupAlreadyExists bool
	var snap *snapshots.Snapshot
	var backup *backups.Backup
	var backups []backups.Backup
	var err error

	// Set snapshot type to 'snapshot' by default
	if snapshotType == "" {
		snapshotType = "snapshot"
	}

	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot name must be provided in CreateSnapshot request")
	}

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeID must be provided in CreateSnapshot request")
	}

	// Verify snapshot type has a valid value
	if snapshotType != "snapshot" && snapshotType != "backup" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot type must be 'backup', 'snapshot' or not defined")
	}
	var backupsAreEnabled bool
	backupsAreEnabled, err = cs.Cloud.BackupsAreEnabled()
	klog.V(4).Infof("Backups enabled: %v", backupsAreEnabled)
	if err != nil {
		klog.Errorf("Failed to check if backups are enabled: %v", err)
	}

	// Prechecks in case of a backup
	if snapshotType == "backup" {
		if !backupsAreEnabled {
			return nil, status.Error(codes.FailedPrecondition, "Backups are not enabled in Cinder")
		}
		// Get a list of backups with the provided name
		backups, err = cs.Cloud.ListBackups(filters)
		if err != nil {
			klog.Errorf("Failed to query for existing Backup during CreateSnapshot: %v", err)
			return nil, status.Error(codes.Internal, "Failed to get backups")
		}
		// If more than one backup with the provided name exists, fail
		if len(backups) > 1 {
			klog.Errorf("found multiple existing backups with selected name (%s) during create", name)
			return nil, status.Error(codes.Internal, "Multiple backups reported by Cinder with same name")
		}

		if len(backups) == 1 {
			// since backup.VolumeID is not part of ListBackups response
			// we need fetch single backup to get the full object.
			backup, err = cs.Cloud.GetBackupByID(backups[0].ID)
			if err != nil {
				klog.Errorf("Failed to get backup by ID %s: %v", backup.ID, err)
				return nil, status.Error(codes.Internal, "Failed to get backup by ID")
			}

			// Verify the existing backup has the same VolumeID, otherwise it belongs to another volume
			if backup.VolumeID != volumeID {
				klog.Errorf("found existing backup for volumeID (%s) but different source volume ID (%s)", volumeID, backup.VolumeID)
				return nil, status.Error(codes.AlreadyExists, "Backup with given name already exists, with different source volume ID")
			}

			// If a backup of the volume already exists, skip creating the snapshot
			backupAlreadyExists = true
			klog.V(3).Infof("Found existing backup %s from volume with ID: %s", name, volumeID)
		}

		// Get the max duration to wait in seconds per GB of snapshot and fail if parsing fails
		if item, ok := (req.Parameters)[openstack.BackupMaxDurationPerGB]; ok {
			backupMaxDurationSecondsPerGB, err = strconv.Atoi(item)
			if err != nil {
				klog.Errorf("Setting backup-max-duration-seconds-per-gb failed due to a parsing error: %v", err)
				return nil, status.Error(codes.Internal, "Failed to parse backup-max-duration-seconds-per-gb")
			}
		}
	}

	// Create the snapshot if the backup does not already exist and wait for it to be ready
	if !backupAlreadyExists {
		snap, err = cs.createSnapshot(name, volumeID, req.Parameters)
		if err != nil {
			return nil, err
		}

		ctime = timestamppb.New(snap.CreatedAt)
		if err = ctime.CheckValid(); err != nil {
			klog.Errorf("Error to convert time to timestamp: %v", err)
		}

		snap.Status, err = cs.Cloud.WaitSnapshotReady(snap.ID)
		if err != nil {
			klog.Errorf("Failed to WaitSnapshotReady: %v", err)
			return nil, status.Errorf(codes.Internal, "CreateSnapshot failed with error: %v. Current snapshot status: %v", err, snap.Status)
		}

		snapSize = snap.Size
	}

	if snapshotType == "snapshot" {
		return &csi.CreateSnapshotResponse{
			Snapshot: &csi.Snapshot{
				SnapshotId:     snap.ID,
				SizeBytes:      int64(snap.Size * 1024 * 1024 * 1024),
				SourceVolumeId: snap.VolumeID,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}, nil
	}

	// If snapshotType is 'backup', create a backup from the snapshot and delete the snapshot.
	if snapshotType == "backup" {

		if !backupAlreadyExists {
			backup, err = cs.createBackup(name, volumeID, snap, req.Parameters)
			if err != nil {
				return nil, err
			}
		}

		ctime = timestamppb.New(backup.CreatedAt)
		if err := ctime.CheckValid(); err != nil {
			klog.Errorf("Error to convert time to timestamp: %v", err)
		}

		backup.Status, err = cs.Cloud.WaitBackupReady(backup.ID, snapSize, backupMaxDurationSecondsPerGB)
		if err != nil {
			klog.Errorf("Failed to WaitBackupReady: %v", err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("CreateBackup failed with error %v. Current backups status: %s", err, backup.Status))
		}

		// Necessary to get all the backup information, including size.
		backup, err = cs.Cloud.GetBackupByID(backup.ID)
		if err != nil {
			klog.Errorf("Failed to GetBackupByID after backup creation: %v", err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("GetBackupByID failed with error %v", err))
		}

		err = cs.Cloud.DeleteSnapshot(backup.SnapshotID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			klog.Errorf("Failed to DeleteSnapshot: %v", err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("DeleteSnapshot failed with error %v", err))
		}
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     backup.ID,
			SizeBytes:      int64(backup.Size * 1024 * 1024 * 1024),
			SourceVolumeId: backup.VolumeID,
			CreationTime:   ctime,
			ReadyToUse:     true,
		},
	}, nil

}

func (cs *controllerServer) createSnapshot(name string, volumeID string, parameters map[string]string) (snap *snapshots.Snapshot, err error) {

	filters := map[string]string{}
	filters["Name"] = name

	// List existing snapshots with the same name
	snapshots, _, err := cs.Cloud.ListSnapshots(filters)
	if err != nil {
		klog.Errorf("Failed to query for existing Snapshot during CreateSnapshot: %v", err)
		return nil, status.Error(codes.Internal, "Failed to get snapshots")
	}

	// If more than one snapshot with the provided name exists, fail
	if len(snapshots) > 1 {
		klog.Errorf("found multiple existing snapshots with selected name (%s) during create", name)

		return nil, status.Error(codes.Internal, "Multiple snapshots reported by Cinder with same name")
	}

	// Verify a snapshot with the provided name doesn't already exist for this tenant
	if len(snapshots) == 1 {
		snap = &snapshots[0]
		if snap.VolumeID != volumeID {
			return nil, status.Error(codes.AlreadyExists, "Snapshot with given name already exists, with different source volume ID")
		}

		// If the snapshot for the correct volume already exists, return it
		klog.V(3).Infof("Found existing snapshot %s from volume with ID: %s", name, volumeID)
		return snap, nil
	}

	// Add cluster ID to the snapshot metadata
	properties := map[string]string{cinderCSIClusterIDKey: cs.Driver.cluster}

	// see https://github.com/kubernetes-csi/external-snapshotter/pull/375/
	// Also, we don't want to tag every param but we still want to send the
	// 'force-create' flag to openstack layer so that we will honor the
	// force create functions
	for _, mKey := range []string{"csi.storage.k8s.io/volumesnapshot/name", "csi.storage.k8s.io/volumesnapshot/namespace", "csi.storage.k8s.io/volumesnapshotcontent/name", openstack.SnapshotForceCreate} {
		if v, ok := parameters[mKey]; ok {
			properties[mKey] = v
		}
	}

	// TODO: Delegate the check to openstack itself and ignore the conflict
	snap, err = cs.Cloud.CreateSnapshot(name, volumeID, properties)
	if err != nil {
		klog.Errorf("Failed to Create snapshot: %v", err)
		return nil, status.Errorf(codes.Internal, "CreateSnapshot failed with error %v", err)
	}

	klog.V(3).Infof("CreateSnapshot %s from volume with ID: %s", name, volumeID)

	return snap, nil
}

func (cs *controllerServer) createBackup(name string, volumeID string, snap *snapshots.Snapshot, parameters map[string]string) (*backups.Backup, error) {

	// Add cluster ID to the snapshot metadata
	properties := map[string]string{cinderCSIClusterIDKey: cs.Driver.cluster}

	// see https://github.com/kubernetes-csi/external-snapshotter/pull/375/
	// Also, we don't want to tag every param but we still want to send the
	// 'force-create' flag to openstack layer so that we will honor the
	// force create functions
	for _, mKey := range []string{"csi.storage.k8s.io/volumesnapshot/name", "csi.storage.k8s.io/volumesnapshot/namespace", "csi.storage.k8s.io/volumesnapshotcontent/name", openstack.SnapshotForceCreate, openstack.SnapshotType} {
		if v, ok := parameters[mKey]; ok {
			properties[mKey] = v
		}
	}

	backup, err := cs.Cloud.CreateBackup(name, volumeID, snap.ID, parameters[openstack.SnapshotAvailabilityZone], properties)
	if err != nil {
		klog.Errorf("Failed to Create backup: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("CreateBackup failed with error %v", err))
	}
	klog.V(4).Infof("Backup created: %+v", backup)

	return backup, nil
}

func (cs *controllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.V(4).Infof("DeleteSnapshot: called with args %+v", protosanitizer.StripSecrets(*req))

	id := req.GetSnapshotId()

	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "Snapshot ID must be provided in DeleteSnapshot request")
	}

	// If volumeSnapshot object was linked to a cinder backup, delete the backup.
	back, err := cs.Cloud.GetBackupByID(id)
	if err == nil && back != nil {
		err = cs.Cloud.DeleteBackup(id)
		if err != nil {
			klog.Errorf("Failed to Delete backup: %v", err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("DeleteBackup failed with error %v", err))
		}
	}

	// Delegate the check to openstack itself
	err = cs.Cloud.DeleteSnapshot(id)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).Infof("Snapshot %s is already deleted.", id)
			return &csi.DeleteSnapshotResponse{}, nil
		}
		klog.Errorf("Failed to Delete snapshot: %v", err)
		return nil, status.Errorf(codes.Internal, "DeleteSnapshot failed with error %v", err)
	}
	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {

	snapshotID := req.GetSnapshotId()
	if len(snapshotID) != 0 {
		snap, err := cs.Cloud.GetSnapshotByID(snapshotID)
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				klog.V(3).Infof("Snapshot %s not found", snapshotID)
				return &csi.ListSnapshotsResponse{}, nil
			}
			return nil, status.Errorf(codes.Internal, "Failed to GetSnapshot %s: %v", snapshotID, err)
		}

		ctime := timestamppb.New(snap.CreatedAt)

		entry := &csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      int64(snap.Size * 1024 * 1024 * 1024),
				SnapshotId:     snap.ID,
				SourceVolumeId: snap.VolumeID,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}

		entries := []*csi.ListSnapshotsResponse_Entry{entry}
		return &csi.ListSnapshotsResponse{
			Entries: entries,
		}, ctime.CheckValid()

	}

	filters := map[string]string{}

	var slist []snapshots.Snapshot
	var err error
	var nextPageToken string

	// Add the filters
	if len(req.GetSourceVolumeId()) != 0 {
		filters["VolumeID"] = req.GetSourceVolumeId()
	} else {
		filters["Limit"] = strconv.Itoa(int(req.MaxEntries))
		filters["Marker"] = req.StartingToken
	}

	// Only retrieve snapshots that are available
	filters["Status"] = "available"
	slist, nextPageToken, err = cs.Cloud.ListSnapshots(filters)
	if err != nil {
		klog.Errorf("Failed to ListSnapshots: %v", err)
		return nil, status.Errorf(codes.Internal, "ListSnapshots failed with error %v", err)
	}

	sentries := make([]*csi.ListSnapshotsResponse_Entry, 0, len(slist))
	for _, v := range slist {
		ctime := timestamppb.New(v.CreatedAt)
		if err := ctime.CheckValid(); err != nil {
			klog.Errorf("Error to convert time to timestamp: %v", err)
		}
		sentry := csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      int64(v.Size * 1024 * 1024 * 1024),
				SnapshotId:     v.ID,
				SourceVolumeId: v.VolumeID,
				CreationTime:   ctime,
				ReadyToUse:     true,
			},
		}
		sentries = append(sentries, &sentry)
	}
	return &csi.ListSnapshotsResponse{
		Entries:   sentries,
		NextToken: nextPageToken,
	}, nil

}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *controllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(5).Infof("Using default ControllerGetCapabilities")

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.Driver.cscap,
	}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	reqVolCap := req.GetVolumeCapabilities()

	if len(reqVolCap) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume Capabilities must be provided")
	}
	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume ID must be provided")
	}

	_, err := cs.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "ValidateVolumeCapabilities Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "ValidateVolumeCapabilities %v", err)
	}

	for _, cap := range reqVolCap {
		if cap.GetAccessMode().GetMode() != cs.Driver.vcap[0].Mode {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "Requested Volume Capability not supported"}, nil
		}
	}

	// Cinder CSI driver currently supports one mode only
	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: cs.Driver.vcap[0],
				},
			},
		},
	}

	return resp, nil
}

func (cs *controllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity is not yet implemented")
}

func (cs *controllerServer) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(4).Infof("ControllerGetVolume: called with args %+v", protosanitizer.StripSecrets(*req))

	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	volume, err := cs.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "ControllerGetVolume failed with error %v", err)
	}

	ventry := csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volumeID,
			CapacityBytes: int64(volume.Size * 1024 * 1024 * 1024),
		},
	}

	status := &csi.ControllerGetVolumeResponse_VolumeStatus{}
	status.PublishedNodeIds = make([]string, 0, len(volume.Attachments))
	for _, attachment := range volume.Attachments {
		status.PublishedNodeIds = append(status.PublishedNodeIds, attachment.ServerID)
	}
	ventry.Status = status

	return &ventry, nil
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(4).Infof("ControllerExpandVolume: called with args %+v", protosanitizer.StripSecrets(*req))

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	cap := req.GetCapacityRange()
	if cap == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range not provided")
	}

	volSizeBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	volSizeGB := int(util.RoundUpSize(volSizeBytes, 1024*1024*1024))
	maxVolSize := cap.GetLimitBytes()

	if maxVolSize > 0 && maxVolSize < volSizeBytes {
		return nil, status.Error(codes.OutOfRange, "After round-up, volume size exceeds the limit specified")
	}

	volume, err := cs.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Errorf(codes.Internal, "GetVolume failed with error %v", err)
	}

	if volume.Size >= volSizeGB {
		// a volume was already resized
		klog.V(2).Infof("Volume %q has been already expanded to %d, requested %d", volumeID, volume.Size, volSizeGB)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(volume.Size * 1024 * 1024 * 1024),
			NodeExpansionRequired: true,
		}, nil
	}

	err = cs.Cloud.ExpandVolume(volumeID, volume.Status, volSizeGB)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q to size %v: %v", volumeID, volSizeGB, err)
	}

	// we need wait for the volume to be available or InUse, it might be error_extending in some scenario
	targetStatus := []string{openstack.VolumeAvailableStatus, openstack.VolumeInUseStatus}
	err = cs.Cloud.WaitVolumeTargetStatus(volumeID, targetStatus)
	if err != nil {
		klog.Errorf("Failed to WaitVolumeTargetStatus of volume %s: %v", volumeID, err)
		return nil, status.Errorf(codes.Internal, "[ControllerExpandVolume] Volume %s not in target state after resize operation: %v", volumeID, err)
	}

	klog.V(4).Infof("ControllerExpandVolume resized volume %v to size %v", volumeID, volSizeGB)

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         volSizeBytes,
		NodeExpansionRequired: true,
	}, nil
}

func getCreateVolumeResponse(vol *volumes.Volume, ignoreVolumeAZ bool, accessibleTopologyReq *csi.TopologyRequirement) *csi.CreateVolumeResponse {

	var volsrc *csi.VolumeContentSource

	if vol.SnapshotID != "" {
		volsrc = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: vol.SnapshotID,
				},
			},
		}
	}

	if vol.SourceVolID != "" {
		volsrc = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Volume{
				Volume: &csi.VolumeContentSource_VolumeSource{
					VolumeId: vol.SourceVolID,
				},
			},
		}
	}

	if vol.BackupID != nil && *vol.BackupID != "" {
		volsrc = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: *vol.BackupID,
				},
			},
		}
	}

	var accessibleTopology []*csi.Topology
	// If ignore-volume-az is true , dont set the accessible topology to volume az,
	// use from preferred topologies instead.
	if ignoreVolumeAZ {
		if accessibleTopologyReq != nil {
			accessibleTopology = accessibleTopologyReq.GetPreferred()
		}
	} else {
		accessibleTopology = []*csi.Topology{
			{
				Segments: map[string]string{topologyKey: vol.AvailabilityZone},
			},
		}
	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           vol.ID,
			CapacityBytes:      int64(vol.Size * 1024 * 1024 * 1024),
			AccessibleTopology: accessibleTopology,
			ContentSource:      volsrc,
		},
	}

	return resp

}
