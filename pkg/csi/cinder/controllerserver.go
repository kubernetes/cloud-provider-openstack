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
	"context"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/backups"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"golang.org/x/exp/maps"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	sharedcsi "k8s.io/cloud-provider-openstack/pkg/csi"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

type controllerServer struct {
	Driver *Driver
	Clouds map[string]openstack.IOpenStack
}

const (
	cinderCSIClusterIDKey = "cinder.csi.openstack.org/cluster"
	affinityKey           = "cinder.csi.openstack.org/affinity"
	antiAffinityKey       = "cinder.csi.openstack.org/anti-affinity"
)

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(4).InfoS("CreateVolume() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[CreateVolume] specified cloud undefined")
	}

	// Volume Name
	volName := req.GetName()
	volCapabilities := req.GetVolumeCapabilities()
	volParams := req.GetParameters()

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
	volType := volParams["type"]

	// First check if volAvailability is already specified, if not get preferred from Topology
	// Required, incase vol AZ is different from node AZ
	volAvailability := volParams["availability"]
	if volAvailability == "" {
		// Check from Topology
		if req.GetAccessibilityRequirements() != nil {
			volAvailability = sharedcsi.GetAZFromTopology(topologyKey, req.GetAccessibilityRequirements())
		}
	}

	ignoreVolumeAZ := cloud.GetBlockStorageOpts().IgnoreVolumeAZ

	// get the PVC annotation
	pvcAnnotations := sharedcsi.GetPVCAnnotations(cs.Driver.pvcLister, volParams)
	for k, v := range pvcAnnotations {
		klog.V(4).InfoS("Retrieved pvc annotation", "func", "CreateVolume", "volume", volName, "key", k, "value", v)
	}

	// Verify a volume with the provided name doesn't already exist for this tenant
	vols, err := cloud.GetVolumesByName(volName)
	if err != nil {
		klog.ErrorS(err, "Failed to query for existing volume", "func", "CreateVolume", "volume", volName)
		return nil, status.Errorf(codes.Internal, "[CreateVolume] failed to get volumes: %v", err)
	}

	if len(vols) == 1 {
		if volSizeGB != vols[0].Size {
			return nil, status.Error(codes.AlreadyExists, "[CreateVolume] Volume Already exists with same name and different capacity")
		}
		klog.V(4).InfoS("Volume already exists in Availability Zone", "func", "CreateVolume", "volumeID", vols[0].ID, "size", vols[0].Size, "zone", vols[0].AvailabilityZone)
		return getCreateVolumeResponse(&vols[0], nil, ignoreVolumeAZ, req.GetAccessibilityRequirements()), nil
	} else if len(vols) > 1 {
		klog.V(3).InfoS("Found multiple existing volumes with selected name during create", "func", "CreateVolume", "volume", volName)
		return nil, status.Error(codes.Internal, "[CreateVolume] Multiple volumes reported by Cinder with same name")
	}

	// Volume Create
	properties := map[string]string{cinderCSIClusterIDKey: cs.Driver.cluster}
	//Tag volume with metadata if present: https://github.com/kubernetes-csi/external-provisioner/pull/399
	for _, mKey := range sharedcsi.RecognizedCSIProvisionerParams {
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
	klog.V(4).InfoS("Backups enabled", "func", "CreateVolume", "status", backupsAreEnabled)
	if err != nil {
		klog.ErrorS(err, "Failed to check if backups are enabled", "func", "CreateVolume")
	}

	if content != nil && content.GetSnapshot() != nil {
		snapshotID = content.GetSnapshot().GetSnapshotId()

		snap, err := cloud.GetSnapshotByID(snapshotID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, err
		}
		// If the snapshot exists but is not yet available, fail.
		if err == nil && snap.Status != "available" {
			return nil, status.Errorf(codes.Unavailable, "[CreateVolume] Source snapshot %s is not yet available. status: %s", snapshotID, snap.Status)
		}

		// In case a snapshot is not found
		// check if a Backup with the same ID exists
		if backupsAreEnabled && cpoerrors.IsNotFound(err) {
			var back *backups.Backup
			back, err = cloud.GetBackupByID(snapshotID)
			if err != nil {
				//If there is an error getting the backup as well, fail.
				return nil, status.Errorf(codes.NotFound, "[CreateVolume] Snapshot or Backup with ID %s not found", snapshotID)
			}
			if back.Status != "available" {
				// If the backup exists but is not yet available, fail.
				return nil, status.Errorf(codes.Unavailable, "[CreateVolume] Source backup %s is not yet available. status: %s", snapshotID, back.Status)
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
				return nil, status.Errorf(codes.NotFound, "[CreateVolume] Source volume %s not found", sourceVolID)
			}
			return nil, status.Errorf(codes.Internal, "[CreateVolume] Failed to retrieve the source volume %s: %v", sourceVolID, err)
		}
	}

	opts := &volumes.CreateOpts{
		Name:             volName,
		Size:             volSizeGB,
		VolumeType:       volType,
		AvailabilityZone: volAvailability,
		SnapshotID:       snapshotID,
		SourceVolID:      sourceVolID,
		BackupID:         sourceBackupID,
		Metadata:         properties,
	}

	// Set scheduler hints if affinity or anti-affinity is set in PVC annotations
	var schedulerHints volumes.SchedulerHintOptsBuilder
	var volCtx map[string]string
	affinity := pvcAnnotations[affinityKey]
	antiAffinity := pvcAnnotations[antiAffinityKey]
	if affinity != "" || antiAffinity != "" {
		klog.V(4).InfoS("Getting scheduler hints", "func", "CreateVolume", "affinity", affinity, "anti-affinity", antiAffinity)

		// resolve volume names to UUIDs
		affinity, err = cloud.ResolveVolumeListToUUIDs(affinity)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "[CreateVolume] failed to resolve affinity volume UUIDs: %v", err)
		}
		antiAffinity, err = cloud.ResolveVolumeListToUUIDs(antiAffinity)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "[CreateVolume] failed to resolve anti-affinity volume UUIDs: %v", err)
		}

		volCtx = util.SetMapIfNotEmpty(volCtx, "affinity", affinity)
		volCtx = util.SetMapIfNotEmpty(volCtx, "anti-affinity", antiAffinity)
		schedulerHints = &volumes.SchedulerHintOpts{
			SameHost:      util.SplitTrim(affinity, ','),
			DifferentHost: util.SplitTrim(antiAffinity, ','),
		}

		klog.V(4).InfoS("Resolved scheduler hints", "func", "CreateVolume", "affinity", affinity, "anti-affinity", antiAffinity)
	}

	vol, err := cloud.CreateVolume(opts, schedulerHints)
	if err != nil {
		klog.ErrorS(err, "Failed to CreateVolume", "func", "CreateVolume")
		return nil, status.Errorf(codes.Internal, "[CreateVolume] failed with error %v", err)
	}

	// When creating a volume from a backup, the response does not include the backupID.
	if sourceBackupID != "" {
		vol.BackupID = &sourceBackupID
	}

	klog.V(4).InfoS("Successfully created volume", "func", "CreateVolume", "volumeID", vol.ID, "size", vol.Size, "zone", vol.AvailabilityZone)

	return getCreateVolumeResponse(vol, volCtx, ignoreVolumeAZ, req.GetAccessibilityRequirements()), nil
}

func (d *controllerServer) ControllerModifyVolume(ctx context.Context, req *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	klog.V(4).InfoS("ControllerModifyVolume() called", "args", protosanitizer.StripSecrets(*req))

	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(4).InfoS("DeleteVolume() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[DeleteVolume] Specified cloud undefined")
	}

	// Volume Delete
	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[DeleteVolume] Volume ID must be provided")
	}

	err := cloud.DeleteVolume(volID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).InfoS("Volume already deleted", "func", "DeleteVolume", "volumeID", volID, "cloud", volCloud)
			return &csi.DeleteVolumeResponse{}, nil
		}
		klog.ErrorS(err, "Failed to DeleteVolume", "volumeID", volID)
		return nil, status.Errorf(codes.Internal, "[DeleteVolume] Delete volume failed with error %v", err)
	}

	klog.V(4).InfoS("Successfully deleted volume", "func", "DeleteVolume", "volumeID", volID, "cloud", volCloud)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	klog.V(4).InfoS("ControllerPublishVolume() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] Specified cloud undefined")
	}

	// Volume Attach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()
	volumeCapability := req.GetVolumeCapability()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] volume ID must be provided")
	}
	if len(instanceID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] instance ID must be provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "[ControllerPublishVolume] volume capability must be provided")
	}

	_, err := cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] GetVolume failed with error %v", err)
	}

	_, err = cloud.GetInstanceByID(instanceID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerPublishVolume] Instance %s not found", instanceID)
		}
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] GetInstanceByID failed with error %v", err)
	}

	_, err = cloud.AttachVolume(instanceID, volumeID)
	if err != nil {
		klog.ErrorS(err, "Failed to AttachVolume", "func", "ControllerPublishVolume")
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] AttachVolume failed with error %v", err)

	}

	err = cloud.WaitDiskAttached(instanceID, volumeID)
	if err != nil {
		klog.ErrorS(err, "Failed to WaitDiskAttached", "func", "ControllerPublishVolume")
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] failed to attach volume: %v", err)
	}

	devicePath, err := cloud.GetAttachmentDiskPath(instanceID, volumeID)
	if err != nil {
		klog.ErrorS(err, "Failed to GetAttachmentDiskPath", "func", "ControllerPublishVolume")
		return nil, status.Errorf(codes.Internal, "[ControllerPublishVolume] failed to get device path of attached volume: %v", err)
	}

	klog.V(4).InfoS("Successfully attached volume", "func", "ControllerPublishVolume", "volumeID", volumeID, "instanceID", instanceID, "cloud", volCloud)

	// Publish Volume Info
	pvInfo := map[string]string{}
	pvInfo["DevicePath"] = devicePath

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: pvInfo,
	}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).InfoS("ControllerUnpublishVolume() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[ControllerUnpublishVolume] specified cloud undefined")
	}

	// Volume Detach
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerUnpublishVolume] volume ID must be provided")
	}

	_, err := cloud.GetInstanceByID(instanceID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).InfoS("Assuming volume is detached, because node does not exist",
				"func", "ControllerUnpublishVolume", "volumeID", volumeID, "instanceID", instanceID, "cloud", volCloud)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "[ControllerUnpublishVolume] GetInstanceByID failed with error %v", err)
	}

	err = cloud.DetachVolume(instanceID, volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).InfoS("Assuming volume is detached, because it was deleted in the meanwhile",
				"func", "ControllerUnpublishVolume", "volumeID", volumeID, "cloud", volCloud)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		klog.ErrorS(err, "Failed to DetachVolume")
		return nil, status.Errorf(codes.Internal, "[ControllerUnpublishVolume] Detach Volume failed with error %v", err)
	}

	err = cloud.WaitDiskDetached(instanceID, volumeID)
	if err != nil {
		klog.ErrorS(err, "Failed to WaitDiskDetached")
		if cpoerrors.IsNotFound(err) {
			klog.V(3).InfoS("Assuming volume is detached, because it was deleted in the meanwhile",
				"func", "ControllerUnpublishVolume", "volumeID", volumeID, "cloud", volCloud)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "[ControllerUnpublishVolume] Failed with error %v", err)
	}

	klog.V(4).InfoS("Successfully detached volume", "func", "ControllerUnpublishVolume", "volumeID", volumeID, "instanceID", instanceID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

type CloudsStartingToken struct {
	CloudName string `json:"cloud"`
	Token     string `json:"token"`
	isEmpty   bool
}

func (cs *controllerServer) extractNodeIDs(attachments []volumes.Attachment) []string {
	nodeIDs := make([]string, len(attachments))
	for i, attachment := range attachments {
		nodeIDs[i] = attachment.ServerID
	}
	return nodeIDs
}

func (cs *controllerServer) createVolumeEntries(vlist []volumes.Volume) []*csi.ListVolumesResponse_Entry {
	entries := make([]*csi.ListVolumesResponse_Entry, len(vlist))
	for i, v := range vlist {
		entries[i] = &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      v.ID,
				CapacityBytes: int64(v.Size * 1024 * 1024 * 1024),
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: cs.extractNodeIDs(v.Attachments),
			},
		}
	}
	return entries
}

func (cs *controllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(4).InfoS("ListVolumes() called", "args", protosanitizer.StripSecrets(*req))

	if req.MaxEntries < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "[ListVolumes] Invalid max entries request %v, must not be negative ", req.MaxEntries)
	}
	maxEntries := int(req.MaxEntries)

	var err error
	var cloudsToken = CloudsStartingToken{
		CloudName: "",
		Token:     "",
		isEmpty:   len(req.StartingToken) == 0,
	}

	cloudsNames := maps.Keys(cs.Clouds)
	sort.Strings(cloudsNames)

	currentCloudName := cloudsNames[0]
	if req.StartingToken != "" {
		err = json.Unmarshal([]byte(req.StartingToken), &cloudsToken)
		if err != nil {
			return nil, status.Errorf(codes.Aborted, "[ListVolumes] Invalid request: Token invalid")
		}
		currentCloudName = cloudsToken.CloudName
	}

	startingToken := cloudsToken.Token
	var cloudsVentries []*csi.ListVolumesResponse_Entry
	var vlist []volumes.Volume
	var nextPageToken string

	if !cloudsToken.isEmpty && startingToken == "" {
		// previous call ended on last volumes from "currentCloudName" we should pass to next one
		for i := range cloudsNames {
			if cloudsNames[i] == currentCloudName {
				currentCloudName = cloudsNames[i+1]
				break
			}
		}
	}

	startIdx := 0
	for _, cloudName := range cloudsNames {
		if cloudName == currentCloudName {
			break
		}
		startIdx++
	}
	for idx := startIdx; idx < len(cloudsNames); idx++ {
		if maxEntries > 0 {
			vlist, nextPageToken, err = cs.Clouds[cloudsNames[idx]].ListVolumes(maxEntries-len(cloudsVentries), startingToken)
		} else {
			vlist, nextPageToken, err = cs.Clouds[cloudsNames[idx]].ListVolumes(maxEntries, startingToken)
		}
		startingToken = nextPageToken
		if err != nil {
			klog.ErrorS(err, "Failed to ListVolumes")
			if cpoerrors.IsInvalidError(err) {
				return nil, status.Errorf(codes.Aborted, "[ListVolumes] Invalid request: %v", err)
			}
			return nil, status.Errorf(codes.Internal, "[ListVolumes] Failed with error %v", err)
		}

		ventries := cs.createVolumeEntries(vlist)
		klog.V(4).InfoS("Retrieved entries", "func", "ListVolumes", "entries", len(ventries), "cloud", cloudsNames[idx])

		cloudsVentries = append(cloudsVentries, ventries...)

		// Reach maxEntries setup nextToken with cloud identifier if needed
		sendEmptyToken := false
		if maxEntries > 0 && len(cloudsVentries) == maxEntries {
			if nextPageToken == "" {
				if idx+1 == len(cloudsNames) {
					// no more entries and no more clouds
					// send no token its finished
					klog.V(4).InfoS("Completed with max entries", "func", "ListVolumes", "entries", len(cloudsVentries))
					return &csi.ListVolumesResponse{
						Entries:   cloudsVentries,
						NextToken: "",
					}, nil
				} else {
					// still clouds to process
					// set token to next non empty cloud
					i := 0
					for i = idx + 1; i < len(cloudsNames); i++ {
						vlistTmp, _, err := cs.Clouds[cloudsNames[i]].ListVolumes(1, "")
						if err != nil {
							klog.ErrorS(err, "Failed to ListVolumes", "func", "ListVolumes")
							if cpoerrors.IsInvalidError(err) {
								return nil, status.Errorf(codes.Aborted, "[ListVolumes] Invalid request: %v", err)
							}
							return nil, status.Errorf(codes.Internal, "[ListVolumes] Failed with error %v", err)
						}
						if len(vlistTmp) > 0 {
							cloudsToken.CloudName = cloudsNames[i]
							cloudsToken.isEmpty = false
							break
						}
					}
					if i == len(cloudsNames) {
						sendEmptyToken = true
					}
				}
			}
			cloudsToken.CloudName = cloudsNames[idx]
			cloudsToken.Token = nextPageToken
			var data []byte
			data, _ = json.Marshal(cloudsToken)
			if sendEmptyToken {
				data = []byte("")
			}
			klog.V(4).InfoS("Completed with max entries", "func", "ListVolumes", "entries", len(cloudsVentries), "nextToken", string(data))
			return &csi.ListVolumesResponse{
				Entries:   cloudsVentries,
				NextToken: string(data),
			}, nil
		}
	}

	klog.V(4).InfoS("Completed with all entries", "func", "ListVolumes", "entries", len(cloudsVentries))
	return &csi.ListVolumesResponse{
		Entries:   cloudsVentries,
		NextToken: "",
	}, nil
}

func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.V(4).InfoS("CreateSnapshot() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[CreateSnapshot] specified cloud undefined")
	}

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
		return nil, status.Error(codes.InvalidArgument, "[CreateSnapshot] Snapshot name must be provided")
	}

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "[CreateSnapshot] VolumeID must be provided")
	}

	// Verify snapshot type has a valid value
	if snapshotType != "snapshot" && snapshotType != "backup" {
		return nil, status.Error(codes.InvalidArgument, "[CreateSnapshot] Snapshot type must be 'backup', 'snapshot' or not defined")
	}
	var backupsAreEnabled bool
	backupsAreEnabled, err = cloud.BackupsAreEnabled()
	klog.V(4).InfoS("Backups enabled", "func", "CreateSnapshot", "status", backupsAreEnabled)
	if err != nil {
		klog.ErrorS(err, "Failed to check if backups are enabled")
	}

	// Prechecks in case of a backup
	if snapshotType == "backup" {
		if !backupsAreEnabled {
			return nil, status.Error(codes.FailedPrecondition, "[CreateSnapshot] Backups are not enabled in Cinder")
		}
		// Get a list of backups with the provided name
		backups, err = cloud.ListBackups(filters)
		if err != nil {
			klog.ErrorS(err, "Failed to query for existing Backup", "func", "CreateSnapshot")
			return nil, status.Error(codes.Internal, "[CreateSnapshot] Failed to get backups")
		}
		// If more than one backup with the provided name exists, fail
		if len(backups) > 1 {
			klog.Errorf("found multiple existing backups with selected name (%s) during create", name)
			return nil, status.Error(codes.Internal, "[CreateSnapshot] Multiple backups reported by Cinder with same name")
		}

		if len(backups) == 1 {
			// since backup.VolumeID is not part of ListBackups response
			// we need fetch single backup to get the full object.
			backup, err = cloud.GetBackupByID(backups[0].ID)
			if err != nil {
				klog.ErrorS(err, "Failed to get backup by ID", "func", "CreateSnapshot", "backupID", backup.ID)
				return nil, status.Error(codes.Internal, "[CreateSnapshot] Failed to get backup by ID")
			}

			// Verify the existing backup has the same VolumeID, otherwise it belongs to another volume
			if backup.VolumeID != volumeID {
				klog.Errorf("found existing backup for volumeID (%s) but different source volume ID (%s)", volumeID, backup.VolumeID)
				return nil, status.Error(codes.AlreadyExists, "[CreateSnapshot] Backup with given name already exists, with different source volume ID")
			}

			// If a backup of the volume already exists, skip creating the snapshot
			backupAlreadyExists = true
			klog.V(3).InfoS("Backup already exists", "name", name, "volumeID", volumeID)
		}

		// Get the max duration to wait in seconds per GB of snapshot and fail if parsing fails
		if item, ok := (req.Parameters)[openstack.BackupMaxDurationPerGB]; ok {
			backupMaxDurationSecondsPerGB, err = strconv.Atoi(item)
			if err != nil {
				klog.ErrorS(err, "Setting backup-max-duration-seconds-per-gb failed due to a parsing error", "func", "CreateSnapshot")
				return nil, status.Error(codes.Internal, "[CreateSnapshot] Failed to parse backup-max-duration-seconds-per-gb")
			}
		}
	}

	// Create the snapshot if the backup does not already exist and wait for it to be ready
	if !backupAlreadyExists {
		snap, err = cs.createSnapshot(cloud, name, volumeID, req.Parameters)
		if err != nil {
			return nil, err
		}

		ctime = timestamppb.New(snap.CreatedAt)
		if err = ctime.CheckValid(); err != nil {
			klog.ErrorS(err, "Error to convert time to timestamp", "func", "CreateSnapshot")
		}

		snap.Status, err = cloud.WaitSnapshotReady(snap.ID)
		if err != nil {
			klog.ErrorS(err, "Failed to WaitSnapshotReady", "func", "CreateSnapshot", "snapshotID", snap.ID)
			return nil, status.Errorf(codes.Internal, "[CreateSnapshot] WaitSnapshotReady failed with error: %v. Current snapshot status: %v", err, snap.Status)
		}

		snapSize = snap.Size
	}

	if snapshotType == "snapshot" {
		klog.V(4).InfoS("Snapshot created", "func", "CreateSnapshot", "name", name, "volumeID", volumeID)
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
			backup, err = cs.createBackup(cloud, name, volumeID, snap, req.Parameters)
			if err != nil {
				return nil, err
			}
		}

		ctime = timestamppb.New(backup.CreatedAt)
		if err := ctime.CheckValid(); err != nil {
			klog.ErrorS(err, "Error to convert time to timestamp", "func", "CreateSnapshot")
		}

		backup.Status, err = cloud.WaitBackupReady(backup.ID, snapSize, backupMaxDurationSecondsPerGB)
		if err != nil {
			klog.ErrorS(err, "Failed to WaitBackupReady", "func", "CreateSnapshot")
			return nil, status.Errorf(codes.Internal, "[CreateSnapshot] WaitBackupReady failed with error %v. Current backups status: %s", err, backup.Status)
		}

		// Necessary to get all the backup information, including size.
		backup, err = cloud.GetBackupByID(backup.ID)
		if err != nil {
			klog.ErrorS(err, "Failed to GetBackupByID after backup creation")
			return nil, status.Errorf(codes.Internal, "[CreateSnapshot] GetBackupByID failed with error %v", err)
		}

		err = cloud.DeleteSnapshot(backup.SnapshotID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to DeleteSnapshot")
			return nil, status.Errorf(codes.Internal, "[CreateSnapshot] DeleteSnapshot failed with error %v", err)
		}
	}

	klog.V(4).InfoS("Backup from the snapshot created", "func", "CreateSnapshot", "name", name, "volumeID", volumeID)
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

func (cs *controllerServer) createSnapshot(cloud openstack.IOpenStack, name string, volumeID string, parameters map[string]string) (snap *snapshots.Snapshot, err error) {
	filters := map[string]string{}
	filters["Name"] = name

	// List existing snapshots with the same name
	snapshots, _, err := cloud.ListSnapshots(filters)
	if err != nil {
		klog.ErrorS(err, "Failed to query for existing snapshots", "func", "createSnapshot")
		return nil, status.Error(codes.Internal, "[CreateSnapshot] Failed to get snapshots")
	}

	// If more than one snapshot with the provided name exists, fail
	if len(snapshots) > 1 {
		klog.InfoS("Found multiple existing snapshots with selected name", "func", "createSnapshot", "name", name)
		return nil, status.Error(codes.Internal, "[CreateSnapshot] Multiple snapshots reported by Cinder with same name")
	}

	// Verify a snapshot with the provided name doesn't already exist for this tenant
	if len(snapshots) == 1 {
		snap = &snapshots[0]
		if snap.VolumeID != volumeID {
			return nil, status.Error(codes.AlreadyExists, "[CreateSnapshot] Snapshot with given name already exists, with different source volume ID")
		}

		// If the snapshot for the correct volume already exists, return it
		klog.V(3).InfoS("Found existing snapshot from volume with ID", "func", "createSnapshot", "name", name, "volumeID", volumeID)
		return snap, nil
	}

	// Add cluster ID to the snapshot metadata
	properties := map[string]string{cinderCSIClusterIDKey: cs.Driver.cluster}

	// see https://github.com/kubernetes-csi/external-snapshotter/pull/375/
	// Also, we don't want to tag every param but we still want to send the
	// 'force-create' flag to openstack layer so that we will honor the
	// force create functions
	for _, mKey := range append(sharedcsi.RecognizedCSISnapshotterParams, openstack.SnapshotForceCreate) {
		if v, ok := parameters[mKey]; ok {
			properties[mKey] = v
		}
	}

	// TODO: Delegate the check to openstack itself and ignore the conflict
	snap, err = cloud.CreateSnapshot(name, volumeID, properties)
	if err != nil {
		klog.ErrorS(err, "Failed to create snapshot", "func", "createSnapshot")
		return nil, status.Errorf(codes.Internal, "[CreateSnapshot] Failed with error %v", err)
	}

	return snap, nil
}

func (cs *controllerServer) createBackup(cloud openstack.IOpenStack, name string, volumeID string, snap *snapshots.Snapshot, parameters map[string]string) (*backups.Backup, error) {
	// Add cluster ID to the snapshot metadata
	properties := map[string]string{cinderCSIClusterIDKey: cs.Driver.cluster}

	// see https://github.com/kubernetes-csi/external-snapshotter/pull/375/
	// Also, we don't want to tag every param but we still want to send the
	// 'force-create' flag to openstack layer so that we will honor the
	// force create functions
	for _, mKey := range append(sharedcsi.RecognizedCSISnapshotterParams, openstack.SnapshotForceCreate, openstack.SnapshotType) {
		if v, ok := parameters[mKey]; ok {
			properties[mKey] = v
		}
	}

	backup, err := cloud.CreateBackup(name, volumeID, snap.ID, parameters[openstack.SnapshotAvailabilityZone], properties)
	if err != nil {
		klog.ErrorS(err, "Failed to create backup", "func", "createBackup")
		return nil, status.Errorf(codes.Internal, "[CreateBackup] Failed with error %v", err)
	}

	return backup, nil
}

func (cs *controllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.V(4).InfoS("DeleteSnapshot() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[DeleteSnapshot] Specified cloud undefined")
	}

	id := req.GetSnapshotId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "[DeleteSnapshot] Snapshot ID must be provided")
	}

	// If volumeSnapshot object was linked to a cinder backup, delete the backup.
	back, err := cloud.GetBackupByID(id)
	if err == nil && back != nil {
		err = cloud.DeleteBackup(id)
		if err != nil {
			klog.ErrorS(err, "Failed to delete backup", "func", "DeleteSnapshot", "cloud", volCloud)
			return nil, status.Errorf(codes.Internal, "[DeleteSnapshot] Failed with error %v", err)
		}
	}

	// Delegate the check to openstack itself
	err = cloud.DeleteSnapshot(id)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(3).InfoS("Snapshot already deleted", "func", "DeleteSnapshot", "snapshotID", id, "cloud", volCloud)
			return &csi.DeleteSnapshotResponse{}, nil
		}
		klog.ErrorS(err, "Failed to delete snapshot", "func", "DeleteSnapshot", "snapshotID", id, "cloud", volCloud)
		return nil, status.Errorf(codes.Internal, "[DeleteSnapshot] Failed with error %v", err)
	}

	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	klog.V(4).InfoS("ListSnapshots() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[ListSnapshots] Specified cloud undefined")
	}

	snapshotID := req.GetSnapshotId()
	if len(snapshotID) != 0 {
		snap, err := cloud.GetSnapshotByID(snapshotID)
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				klog.V(3).InfoS("Snapshot not found", "func", "ListSnapshots", "snapshotID", snapshotID, "cloud", volCloud)
				return &csi.ListSnapshotsResponse{}, nil
			}
			return nil, status.Errorf(codes.Internal, "[ListSnapshots] Failed to GetSnapshot %s: %v", snapshotID, err)
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
	slist, nextPageToken, err = cloud.ListSnapshots(filters)
	if err != nil {
		klog.ErrorS(err, "Failed to ListSnapshots", "func", "ListSnapshots", "cloud", volCloud)
		return nil, status.Errorf(codes.Internal, "[ListSnapshots] Failed with error %v", err)
	}

	sentries := make([]*csi.ListSnapshotsResponse_Entry, 0, len(slist))
	for _, v := range slist {
		ctime := timestamppb.New(v.CreatedAt)
		if err := ctime.CheckValid(); err != nil {
			klog.ErrorS(err, "Error to convert time to timestamp", "func", "ListSnapshots", "cloud", volCloud)
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
	klog.V(5).InfoS("ControllerGetCapabilities() called", "args", protosanitizer.StripSecrets(*req))

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.Driver.cscap,
	}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	klog.V(4).InfoS("ValidateVolumeCapabilities() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[ValidateVolumeCapabilities] Specified cloud undefined")
	}

	reqVolCap := req.GetVolumeCapabilities()

	if len(reqVolCap) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ValidateVolumeCapabilities] Volume Capabilities must be provided")
	}
	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ValidateVolumeCapabilities] Volume ID must be provided")
	}

	_, err := cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ValidateVolumeCapabilities] Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "[ValidateVolumeCapabilities] %v", err)
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
	klog.V(4).InfoS("GetCapacity() called", "args", protosanitizer.StripSecrets(*req))

	return nil, status.Error(codes.Unimplemented, "GetCapacity is not yet implemented")
}

func (cs *controllerServer) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(4).InfoS("ControllerGetVolume() called", "args", protosanitizer.StripSecrets(*req))

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerGetVolume] Volume ID not provided")
	}

	var volume *volumes.Volume
	var err error
	for _, cloud := range cs.Clouds {
		volume, err = cloud.GetVolume(volumeID)
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				continue
			}
			return nil, status.Errorf(codes.Internal, "[ControllerGetVolume] GetVolume failed with error %v", err)
		}
	}
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "[ControllerGetVolume] Volume %s not found", volumeID)
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
	klog.V(4).InfoS("ControllerExpandVolume() called", "args", protosanitizer.StripSecrets(*req))

	// Volume cloud
	volCloud := req.GetSecrets()["cloud"]
	cloud, cloudExist := cs.Clouds[volCloud]
	if !cloudExist {
		return nil, status.Error(codes.InvalidArgument, "[ControllerExpandVolume] Specified cloud undefined")
	}

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[ControllerExpandVolume] Volume ID not provided")
	}
	cap := req.GetCapacityRange()
	if cap == nil {
		return nil, status.Error(codes.InvalidArgument, "[ControllerExpandVolume] Capacity range not provided")
	}

	volSizeBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	volSizeGB := int(util.RoundUpSize(volSizeBytes, 1024*1024*1024))
	maxVolSize := cap.GetLimitBytes()

	if maxVolSize > 0 && maxVolSize < volSizeBytes {
		return nil, status.Error(codes.OutOfRange, "[ControllerExpandVolume] After round-up, volume size exceeds the limit specified")
	}

	volume, err := cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[ControllerExpandVolume] Volume %s not found", volumeID)
		}
		return nil, status.Errorf(codes.Internal, "[ControllerExpandVolume] GetVolume failed with error %v", err)
	}

	if volume.Size >= volSizeGB {
		// a volume was already resized
		klog.V(2).InfoS("Volume has been already expanded", "func", "ControllerExpandVolume", "volumeID", volumeID, "size", volume.Size, "cloud", volCloud)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(volume.Size * 1024 * 1024 * 1024),
			NodeExpansionRequired: true,
		}, nil
	}

	err = cloud.ExpandVolume(volumeID, volume.Status, volSizeGB)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "[ControllerExpandVolume] Could not resize volume %q to size %v: %v", volumeID, volSizeGB, err)
	}

	// we need wait for the volume to be available or InUse, it might be error_extending in some scenario
	targetStatus := []string{openstack.VolumeAvailableStatus, openstack.VolumeInUseStatus}
	err = cloud.WaitVolumeTargetStatus(volumeID, targetStatus)
	if err != nil {
		klog.ErrorS(err, "Failed to WaitVolumeTargetStatus of volume", "func", "ControllerExpandVolume", "volumeID", volumeID)
		return nil, status.Errorf(codes.Internal, "[ControllerExpandVolume] Volume %s not in target state after resize operation: %v", volumeID, err)
	}

	klog.V(4).InfoS("Resized volume", "func", "ControllerExpandVolume", "volumeID", volumeID, "size", volSizeGB, "cloud", volCloud)

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         volSizeBytes,
		NodeExpansionRequired: true,
	}, nil
}

func getCreateVolumeResponse(vol *volumes.Volume, volCtx map[string]string, ignoreVolumeAZ bool, accessibleTopologyReq *csi.TopologyRequirement) *csi.CreateVolumeResponse {
	var volsrc *csi.VolumeContentSource
	volCnx := map[string]string{}

	if vol.SnapshotID != "" {
		volCnx[ResizeRequired] = "true"

		volsrc = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: vol.SnapshotID,
				},
			},
		}
	}

	if vol.SourceVolID != "" {
		volCnx[ResizeRequired] = "true"

		volsrc = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Volume{
				Volume: &csi.VolumeContentSource_VolumeSource{
					VolumeId: vol.SourceVolID,
				},
			},
		}
	}

	if vol.BackupID != nil && *vol.BackupID != "" {
		volCnx[ResizeRequired] = "true"

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

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           vol.ID,
			CapacityBytes:      int64(vol.Size * 1024 * 1024 * 1024),
			AccessibleTopology: accessibleTopology,
			ContentSource:      volsrc,
			VolumeContext:      volCnx,
		},
	}
}
