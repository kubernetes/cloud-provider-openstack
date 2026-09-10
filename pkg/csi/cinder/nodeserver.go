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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	utilpath "k8s.io/utils/path"

	sharedcsi "k8s.io/cloud-provider-openstack/pkg/csi"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/blockdevice"
	"k8s.io/cloud-provider-openstack/pkg/util/brick"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/cloud-provider-openstack/pkg/util/mount"
	mountutil "k8s.io/mount-utils"
)

const (
	// connectionInfoFile is the name of the file used to persist Cinder
	// connection_info under the staging target path. It is written before
	// the volume is mounted (so it lives on the host filesystem, hidden
	// behind the mount) and read back after unmount during unstage.
	connectionInfoFile = ".connection_info.json"

	// attachmentIDFile is the name of the file used to persist the Cinder
	// attachment ID under the staging target path. It is written alongside
	// the connection_info file for potential future use in unstage error
	// recovery.
	attachmentIDFile = ".attachment_id"

	// connectionInfoSiblingExt is the extension appended to the staging
	// target path to create a sibling file that persists connection_info
	// *outside* the mount point. The primary copy (connectionInfoFile)
	// lives under the mount and is only accessible after unmount during
	// unstage. This sibling copy is accessible while the volume is
	// mounted and is used by NodeExpandVolume to call os-brick
	// ExtendVolume for protocol-specific device rescans.
	connectionInfoSiblingExt = ".connection_info"
)

type nodeServer struct {
	Driver     *Driver
	Mount      mount.IMount
	Metadata   metadata.IMetadata
	Brick      brick.IConnector
	Clouds     map[string]openstack.IOpenStack // only set in direct mode, for AttachmentComplete
	KubeClient kubernetes.Interface
	NodeName   string
	Opts       openstack.BlockStorageOpts
	Topologies map[string]string
	csi.UnimplementedNodeServer
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	source := req.GetStagingTargetPath()
	targetPath := req.GetTargetPath()
	volumeCapability := req.GetVolumeCapability()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume ID must be provided")
	}
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Target Path must be provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume Capability must be provided")
	}

	ephemeralVolume := req.GetVolumeContext()[sharedcsi.VolEphemeralKey] == "true"
	if ephemeralVolume {
		// See https://github.com/kubernetes/cloud-provider-openstack/issues/2599
		return nil, status.Error(codes.Unimplemented, "CSI inline ephemeral volumes support is removed in 1.31 release.")
	}

	// In case of ephemeral volume staging path not provided
	if len(source) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Staging Target Path must be provided")
	}

	mountOptions := []string{"bind"}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	} else {
		mountOptions = append(mountOptions, "rw")
	}

	if blk := volumeCapability.GetBlock(); blk != nil {
		return nodePublishVolumeForBlock(req, ns, mountOptions)
	}

	m := ns.Mount
	// Verify whether mounted
	notMnt, err := m.IsLikelyNotMountPointAttach(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Volume Mount
	if notMnt {
		fsType := "ext4"
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType = mnt.FsType
			}
		}
		// Mount
		err = m.Mounter().Mount(source, targetPath, fsType, mountOptions)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func nodePublishVolumeForBlock(req *csi.NodePublishVolumeRequest, ns *nodeServer, mountOptions []string) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolumeBlock: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	podVolumePath := filepath.Dir(targetPath)

	m := ns.Mount

	// Do not trust the path provided by cinder, get the real path on node
	source, err := getDevicePath(volumeID, m)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to find Device path for volume: %v", err)
	}

	exists, err := utilpath.Exists(utilpath.CheckFollowSymlink, podVolumePath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !exists {
		if err := m.MakeDir(podVolumePath); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not create dir %q: %v", podVolumePath, err)
		}
	}
	err = m.MakeFile(targetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error in making file %v", err)
	}

	if err := m.Mounter().Mount(source, targetPath, "", mountOptions); err != nil {
		if removeErr := os.Remove(targetPath); removeErr != nil {
			return nil, status.Errorf(codes.Internal, "Could not remove mount target %q: %v", targetPath, err)
		}
		return nil, status.Errorf(codes.Internal, "Could not mount %q at %q: %v", source, targetPath, err)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).Infof("NodeUnPublishVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[NodeUnpublishVolume] Target Path must be provided")
	}
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[NodeUnpublishVolume] volumeID must be provided")
	}

	if err := ns.Mount.UnmountPath(targetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "Unmount of targetpath %s failed with error %v", targetPath, err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(4).Infof("NodeStageVolume: called with args %+v", protosanitizer.StripSecrets(req))

	stagingTarget := req.GetStagingTargetPath()
	volumeCapability := req.GetVolumeCapability()
	volumeContext := req.GetVolumeContext()
	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	if len(stagingTarget) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target not provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Volume Capability must be provided")
	}

	var devicePath string
	var err error

	m := ns.Mount

	if ns.Driver.IsDirectMode() {
		// Direct mode: obtain the device path via the os-brick sidecar.
		connectionInfo := req.GetPublishContext()["ConnectionInfo"]
		if connectionInfo == "" {
			return nil, status.Error(codes.InvalidArgument, "[NodeStageVolume] ConnectionInfo not found in publish context for direct attach mode")
		}

		devicePath, err = ns.Brick.ConnectVolume(ctx, connectionInfo)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "[NodeStageVolume] ConnectVolume failed: %v", err)
		}
		klog.V(4).Infof("NodeStageVolume: ConnectVolume returned device path %s for volume %s", devicePath, volumeID)

		// Ensure we disconnect the volume if any subsequent step fails
		// before mount. This prevents orphaned devices on the host.
		disconnectOnFailure := true
		defer func() {
			if disconnectOnFailure {
				klog.Warningf("NodeStageVolume: rolling back ConnectVolume for volume %s", volumeID)
				if discErr := ns.Brick.DisconnectVolume(ctx, connectionInfo); discErr != nil {
					klog.Errorf("NodeStageVolume: cleanup DisconnectVolume failed for volume %s: %v", volumeID, discErr)
				}
			}
		}()

		// Mark the attachment as "in-use" in Cinder (best-effort).
		attachmentID := req.GetPublishContext()["AttachmentID"]
		volCloud := req.GetPublishContext()["Cloud"]
		cloud := ns.Clouds[volCloud]
		if cloud != nil && attachmentID != "" {
			if completeErr := cloud.AttachmentComplete(ctx, attachmentID); completeErr != nil {
				// AttachmentComplete (microversion 3.44) transitions the
				// volume to "in-use" in Cinder. Without it the volume
				// stays in "attaching" and Cinder will reject subsequent
				// operations (expand, snapshot, detach from another node).
				// Treat failure as fatal to avoid leaving the volume in an
				// inconsistent state.
				return nil, status.Errorf(codes.Internal, "[NodeStageVolume] AttachmentComplete failed for attachment %s (volume %s): %v", attachmentID, volumeID, completeErr)
			}
			klog.V(4).Infof("NodeStageVolume: AttachmentComplete succeeded for attachment %s (volume %s)", attachmentID, volumeID)
		}

		// Persist connection_info *before* mount so the file lives on the
		// host filesystem underneath the mount point. NodeUnstageVolume
		// reads it back after unmounting.
		connInfoPath := filepath.Join(stagingTarget, connectionInfoFile)
		if writeErr := os.WriteFile(connInfoPath, []byte(connectionInfo), 0600); writeErr != nil {
			return nil, status.Errorf(codes.Internal, "[NodeStageVolume] failed to persist connection info: %v", writeErr)
		}

		// Persist attachment ID for potential future use in unstage
		// error recovery.
		if attachmentID != "" {
			attachIDPath := filepath.Join(stagingTarget, attachmentIDFile)
			if writeErr := os.WriteFile(attachIDPath, []byte(attachmentID), 0600); writeErr != nil {
				klog.Warningf("NodeStageVolume: failed to persist attachment ID: %v", writeErr)
			}
		}

		// Also persist a sibling copy of connection_info *outside* the
		// staging directory so NodeExpandVolume can read it while the
		// volume is mounted (the primary copy is hidden by the mount).
		// This is required for protocol-aware rescans during volume
		// expansion, so treat write failure as fatal.
		siblingPath := stagingTarget + connectionInfoSiblingExt
		if writeErr := os.WriteFile(siblingPath, []byte(connectionInfo), 0600); writeErr != nil {
			return nil, status.Errorf(codes.Internal, "[NodeStageVolume] failed to persist sibling connection info at %s: %v", siblingPath, writeErr)
		}

		// All pre-mount steps succeeded, disarm the cleanup so the
		// deferred DisconnectVolume does not fire.
		disconnectOnFailure = false
	} else {
		// Nova mode: discover the device through the metadata service.
		devicePath, err = getDevicePath(volumeID, m)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Unable to find Device path for volume: %v", err)
		}
	}

	if blk := volumeCapability.GetBlock(); blk != nil {
		// If block volume, do nothing
		return &csi.NodeStageVolumeResponse{}, nil
	}

	// Verify whether mounted
	notMnt, err := m.IsLikelyNotMountPointAttach(stagingTarget)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Volume Mount
	if notMnt {
		// set default fstype is ext4
		fsType := "ext4"
		var options []string
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType = mnt.FsType
			}
			mountFlags := mnt.GetMountFlags()
			options = append(options, collectMountOptions(fsType, mountFlags)...)
		}

		// Parse mkfs options from volume context
		var formatOptions []string
		if mkfsOpts, ok := volumeContext["mkfs-options"]; ok && mkfsOpts != "" {
			formatOptions = strings.Fields(mkfsOpts)
			klog.V(4).Infof("NodeStageVolume: mkfs-options for volume %s: %v", volumeID, formatOptions)
		}

		// Mount
		err = ns.formatAndMountRetry(devicePath, stagingTarget, fsType, options, formatOptions)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	if required, ok := volumeContext[ResizeRequired]; ok && strings.EqualFold(required, "true") {
		r := mountutil.NewResizeFs(ns.Mount.Mounter().Exec)

		needResize, err := r.NeedResize(devicePath, stagingTarget)

		if err != nil {
			return nil, status.Errorf(codes.Internal, "Could not determine if volume %q need to be resized: %v", volumeID, err)
		}

		if needResize {
			klog.V(4).Infof("NodeStageVolume: Resizing volume %q created from a snapshot/volume", volumeID)
			if _, err := r.Resize(devicePath, stagingTarget); err != nil {
				return nil, status.Errorf(codes.Internal, "Could not resize volume %q: %v", volumeID, err)
			}
		}
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

// formatAndMountRetry attempts to format and mount a device at the given path.
// If the initial mount fails, it rescans the device and retries the mount operation.
func (ns *nodeServer) formatAndMountRetry(devicePath, stagingTarget, fsType string, options []string, formatOptions []string) error {
	m := ns.Mount
	err := m.Mounter().FormatAndMountSensitiveWithFormatOptions(devicePath, stagingTarget, fsType, options, nil, formatOptions)
	if err != nil {
		klog.Infof("Initial format and mount failed: %v. Attempting rescan.", err)
		// Attempting rescan if the initial mount fails
		rescanErr := blockdevice.RescanDevice(devicePath)
		if rescanErr != nil {
			klog.Infof("Rescan failed: %v. Returning original mount error.", rescanErr)
			return err
		}
		klog.Infof("Rescan succeeded, retrying format and mount")
		err = m.Mounter().FormatAndMountSensitiveWithFormatOptions(devicePath, stagingTarget, fsType, options, nil, formatOptions)
	}
	return err
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(4).Infof("NodeUnstageVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Staging Target Path must be provided")
	}

	err := ns.Mount.UnmountPath(stagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unmount of targetPath %s failed with error %v", stagingTargetPath, err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	nodeID, err := ns.Metadata.GetInstanceID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "[NodeGetInfo] unable to retrieve instance id of node %v", err)
	}

	nodeInfo := &csi.NodeGetInfoResponse{
		NodeId:            nodeID,
		MaxVolumesPerNode: ns.Opts.NodeVolumeAttachLimit,
	}

	if !ns.Driver.withTopology {
		return nodeInfo, nil
	}

	zone, err := ns.Metadata.GetAvailabilityZone()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "[NodeGetInfo] Unable to retrieve availability zone of node %v", err)
	}
	topologyMap := make(map[string]string, len(ns.Topologies)+1)
	topologyMap[topologyKey] = zone
	for k, v := range ns.Topologies {
		topologyMap[k] = v
	}
	nodeInfo.AccessibleTopology = &csi.Topology{Segments: topologyMap}

	return nodeInfo, nil
}

func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(5).Infof("NodeGetCapabilities called with req: %#v", req)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.Driver.nscap,
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).Infof("NodeGetVolumeStats: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	volumePath := req.GetVolumePath()
	if len(volumePath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume path not provided")
	}

	exists, err := utilpath.Exists(utilpath.CheckFollowSymlink, req.VolumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check whether volumePath exists: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "target: %s not found", volumePath)
	}
	stats, err := ns.Mount.GetDeviceStats(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get stats by path: %v", err)
	}

	if stats.Block {
		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Total: stats.TotalBytes,
					Unit:  csi.VolumeUsage_BYTES,
				},
			},
		}, nil
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{Total: stats.TotalBytes, Available: stats.AvailableBytes, Used: stats.UsedBytes, Unit: csi.VolumeUsage_BYTES},
			{Total: stats.TotalInodes, Available: stats.AvailableInodes, Used: stats.UsedInodes, Unit: csi.VolumeUsage_INODES},
		},
	}, nil
}

func (ns *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	klog.V(4).Infof("NodeExpandVolume: called with args %+v", protosanitizer.StripSecrets(req))

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	volumePath := req.GetVolumePath()
	if len(volumePath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume path not provided")
	}

	_, err := blockdevice.IsBlockDevice(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Failed to determine device path for volumePath %s: %v", volumePath, err)
	}

	output, err := ns.Mount.GetMountFs(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to find mount file system %s: %v", volumePath, err)
	}

	devicePath := strings.TrimSpace(string(output))
	if devicePath == "" {
		return nil, status.Error(codes.Internal, "Unable to find Device path for volume")
	}

	if ns.Opts.RescanOnResize {
		// comparing current volume size with the expected one
		newSize := req.GetCapacityRange().GetRequiredBytes()
		if err := blockdevice.RescanBlockDeviceGeometry(devicePath, volumePath, newSize); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not verify %q volume size: %v", volumeID, err)
		}
	}

	r := mountutil.NewResizeFs(ns.Mount.Mounter().Exec)
	if _, err := r.Resize(devicePath, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q: %v", volumeID, err)
	}
	return &csi.NodeExpandVolumeResponse{}, nil
}

func getDevicePath(volumeID string, m mount.IMount) (string, error) {
	var devicePath string
	devicePath, err := m.GetDevicePath(volumeID)
	if err != nil {
		klog.Warningf("Couldn't get device path from mount: %v", err)
	}

	if devicePath == "" {
		// try to get from metadata service
		klog.Info("Trying to get device path from metadata service")
		devicePath, err = metadata.GetDevicePath(volumeID)
		if err != nil {
			klog.Errorf("Couldn't get device path from metadata service: %v", err)
			return "", fmt.Errorf("couldn't get device path from metadata service: %v", err)
		}
	}

	return devicePath, nil
}

func collectMountOptions(fsType string, mntFlags []string) []string {
	var options []string
	options = append(options, mntFlags...)

	// By default, xfs does not allow mounting of two volumes with the same filesystem uuid.
	// Force ignore this uuid to be able to mount volume + its clone / restored snapshot on the same node.
	if fsType == "xfs" {
		options = append(options, "nouuid")
	}
	return options
}
