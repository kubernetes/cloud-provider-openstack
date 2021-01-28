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
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/util/resizefs"
	utilpath "k8s.io/utils/path"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/blockdevice"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/cloud-provider-openstack/pkg/util/mount"
)

type nodeServer struct {
	Driver   *CinderDriver
	Mount    mount.IMount
	Metadata metadata.IMetadata
	Cloud    openstack.IOpenStack
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolume: called with args %+v", *req)

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

	ephemeralVolume := req.GetVolumeContext()["csi.storage.k8s.io/ephemeral"] == "true"
	if ephemeralVolume {
		return nodePublishEphermeral(req, ns)
	}

	// In case of ephemeral volume staging path not provided
	if len(source) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Staging Target Path must be provided")
	}
	_, err := ns.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("GetVolume failed with error %v", err))
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

func nodePublishEphermeral(req *csi.NodePublishVolumeRequest, ns *nodeServer) (*csi.NodePublishVolumeResponse, error) {

	var size int
	var err error

	volID := req.GetVolumeId()
	readOnly := req.GetReadonly()
	volName := fmt.Sprintf("ephemeral-%s", volID)
	properties := map[string]string{"cinder.csi.openstack.org/cluster": ns.Driver.cluster}
	capacity, ok := req.GetVolumeContext()["capacity"]
	volumeCapability := req.GetVolumeCapability()

	volAvailability, err := ns.Metadata.GetAvailabilityZone()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("retrieving availability zone from MetaData service failed with error %v", err))
	}

	size = 1 // default size is 1GB
	if ok && strings.HasSuffix(capacity, "Gi") {
		size, err = strconv.Atoi(strings.TrimSuffix(capacity, "Gi"))
		if err != nil {
			klog.V(3).Infof("Unable to parse capacity: %v", err)
			return nil, status.Error(codes.Internal, fmt.Sprintf("Unable to parse capacity %v", err))
		}
	}

	// Check type in given param, if not, use ""
	volumeType, ok := req.GetVolumeContext()["type"]
	if !ok {
		volumeType = ""
	}

	evol, err := ns.Cloud.CreateVolume(volName, size, volumeType, volAvailability, "", "", &properties)

	if err != nil {
		klog.V(3).Infof("Failed to Create Ephermal Volume: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to create Ephermal Volume %v", err))
	}

	klog.V(4).Infof("Ephermeral Volume %s is created", evol.ID)

	// attach volume
	// for attach volume we need to have information about node.
	nodeID, err := ns.Metadata.GetInstanceID()
	if err != nil {
		klog.V(3).Infof("Ephermal Volume Attach: Failed to get Instance ID: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Ephermal Volume Attach: Failed to get Instance ID: %v", err))
	}

	_, err = ns.Cloud.AttachVolume(nodeID, evol.ID, readOnly)
	if err != nil {
		klog.V(3).Infof("Ephermal Volume Attach: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Ephermal Volume Attach: Failed to Attach Volume: %v", err))
	}

	err = ns.Cloud.WaitDiskAttached(nodeID, evol.ID)
	if err != nil {
		klog.V(3).Infof("Ephermal Volume Attach: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Ephermal Volume Attach: Failed to Attach Volume: %v", err))
	}

	m := ns.Mount

	devicePath, err := getDevicePath(evol.ID, m)
	if devicePath == "" {
		return nil, status.Error(codes.Internal, "Unable to find Device path for volume")
	}

	targetPath := req.GetTargetPath()

	// Verify whether mounted
	notMnt, err := m.IsLikelyNotMountPointAttach(targetPath)
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
			options = append(options, mountFlags...)
		}
		// Mount
		err = m.Mounter().FormatAndMount(devicePath, targetPath, fsType, nil)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil

}

func nodePublishVolumeForBlock(req *csi.NodePublishVolumeRequest, ns *nodeServer, mountOptions []string) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolumeBlock: called with args %+v", *req)

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	podVolumePath := filepath.Dir(targetPath)

	m := ns.Mount

	// Do not trust the path provided by cinder, get the real path on node
	source, err := getDevicePath(volumeID, m)
	if source == "" {
		return nil, status.Error(codes.Internal, "Unable to find Device path for volume")
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
	klog.V(4).Infof("NodeUnPublishVolume: called with args %+v", *req)

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[NodeUnpublishVolume] Target Path must be provided")
	}
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "[NodeUnpublishVolume] volumeID must be provided")
	}

	ephemeralVolume := false

	vol, err := ns.Cloud.GetVolume(volumeID)

	if err != nil {

		if !cpoerrors.IsNotFound(err) {
			return nil, status.Error(codes.Internal, fmt.Sprintf("GetVolume failed with error %v", err))
		}

		// if not found by id, try to search by name
		volName := fmt.Sprintf("ephemeral-%s", volumeID)

		vols, err := ns.Cloud.GetVolumesByName(volName)

		//if volume not found then GetVolumesByName returns empty list
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("GetVolume failed with error %v", err))
		}
		if len(vols) > 0 {
			vol = &vols[0]
			ephemeralVolume = true
		} else {
			return nil, status.Error(codes.NotFound, fmt.Sprintf("Volume not found %s", volName))
		}
	}

	err = ns.Mount.UnmountPath(targetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unmount of targetpath %s failed with error %v", targetPath, err)
	}

	if ephemeralVolume {
		return nodeUnpublishEphermeral(req, ns, vol)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil

}

func nodeUnpublishEphermeral(req *csi.NodeUnpublishVolumeRequest, ns *nodeServer, vol *volumes.Volume) (*csi.NodeUnpublishVolumeResponse, error) {
	volumeID := vol.ID
	var instanceID string

	if len(vol.Attachments) > 0 {
		instanceID = vol.Attachments[0].ServerID
	} else {
		return nil, status.Error(codes.FailedPrecondition, "Volume attachement not found in request")
	}

	err := ns.Cloud.DetachVolume(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to DetachVolume: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	err = ns.Cloud.WaitDiskDetached(instanceID, volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitDiskDetached: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	err = ns.Cloud.DeleteVolume(volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to DeleteVolume: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(4).Infof("NodeStageVolume: called with args %+v", *req)

	stagingTarget := req.GetStagingTargetPath()
	volumeCapability := req.GetVolumeCapability()
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

	_, err := ns.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("GetVolume failed with error %v", err))
	}

	m := ns.Mount
	// Do not trust the path provided by cinder, get the real path on node
	devicePath, err := getDevicePath(volumeID, m)
	if devicePath == "" {
		return nil, status.Error(codes.Internal, "Unable to find Device path for volume")
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
			options = append(options, mountFlags...)
		}
		// Mount
		err = m.Mounter().FormatAndMount(devicePath, stagingTarget, fsType, options)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(4).Infof("NodeUnstageVolume: called with args %+v", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Staging Target Path must be provided")
	}

	_, err := ns.Cloud.GetVolume(volumeID)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(4).Infof("NodeUnstageVolume: Unable to find volume: %v", err)
			return nil, status.Error(codes.NotFound, "Volume not found")
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("GetVolume failed with error %v", err))
	}

	err = ns.Mount.UnmountPath(stagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unmount of targetPath %s failed with error %v", stagingTargetPath, err)
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {

	nodeID, err := ns.Metadata.GetInstanceID()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("[NodeGetInfo] unable to retrieve instance id of node %v", err))
	}

	zone, err := ns.Metadata.GetAvailabilityZone()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("[NodeGetInfo] Unable to retrieve availability zone of node %v", err))
	}
	topology := &csi.Topology{Segments: map[string]string{topologyKey: zone}}

	maxVolume := ns.Cloud.GetMaxVolLimit()

	return &csi.NodeGetInfoResponse{
		NodeId:             nodeID,
		AccessibleTopology: topology,
		MaxVolumesPerNode:  maxVolume,
	}, nil
}

func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(5).Infof("NodeGetCapabilities called with req: %#v", req)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.Driver.nscap,
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).Infof("NodeGetVolumeStats: called with args %+v", *req)

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
		return nil, status.Errorf(codes.Internal, "failed to check whether volumePath exists: %s", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "target: %s not found", volumePath)
	}
	stats, err := ns.Mount.GetDeviceStats(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get stats by path: %s", err)
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
	klog.V(4).Infof("NodeExpandVolume: called with args %+v", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	volumePath := req.GetVolumePath()

	args := []string{"-o", "source", "--noheadings", "--target", volumePath}
	output, err := ns.Mount.Mounter().Exec.Command("findmnt", args...).CombinedOutput()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not determine device path: %v", err)

	}
	devicePath := strings.TrimSpace(string(output))

	if devicePath == "" {
		return nil, status.Error(codes.Internal, "Unable to find Device path for volume")
	}

	if ns.Cloud.GetBlockStorageOpts().RescanOnResize {
		// comparing current volume size with the expected one
		newSize := req.GetCapacityRange().GetRequiredBytes()
		if err := blockdevice.RescanBlockDeviceGeometry(devicePath, volumePath, newSize); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not verify %q volume size: %v", volumeID, err)
		}
	}

	r := resizefs.NewResizeFs(ns.Mount.Mounter())
	if _, err := r.Resize(devicePath, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q:  %v", volumeID, err)
	}
	return &csi.NodeExpandVolumeResponse{}, nil
}

func getDevicePath(volumeID string, m mount.IMount) (string, error) {
	var devicePath string
	devicePath, _ = m.GetDevicePath(volumeID)
	if devicePath == "" {
		// try to get from metadata service
		devicePath = metadata.GetDevicePath(volumeID)
	}

	return devicePath, nil

}
