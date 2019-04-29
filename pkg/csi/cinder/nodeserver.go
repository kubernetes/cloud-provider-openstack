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

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/mount"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

type nodeServer struct {
	Driver   *CinderDriver
	Mount    mount.IMount
	Metadata openstack.IMetadata
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishVolume: called with args %+v", *req)

	source := req.GetStagingTargetPath()
	targetPath := req.GetTargetPath()
	volumeCapability := req.GetVolumeCapability()

	if len(source) == 0 || len(targetPath) == 0 || volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume missing required arguments")
	}

	m := ns.Mount

	// Verify whether mounted
	notMnt, err := m.IsLikelyNotMountPointAttach(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Volume Mount
	if notMnt {
		// Perform a bind mount
		options := []string{"bind"}
		fsType := "ext4"
		if req.GetReadonly() {
			options = append(options, "ro")
		} else {
			options = append(options, "rw")
		}
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType = mnt.FsType
			}
		}
		// Mount
		err = m.Mount(source, targetPath, fsType, options)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).Infof("NodeUnPublishVolume: called with args %+v", *req)

	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume Target Path must be provided")
	}

	m := ns.Mount

	notMnt, err := m.IsLikelyNotMountPointDetach(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if notMnt {
		return nil, status.Error(codes.NotFound, "Volume not mounted")
	}

	err = m.UnmountPath(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(4).Infof("NodeStageVolume: called with args %+v", *req)

	stagingTarget := req.GetStagingTargetPath()
	volumeCapability := req.GetVolumeCapability()
	if len(stagingTarget) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target not provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Volume Capability must be provided")
	}

	devicePath, ok := req.GetPublishContext()["DevicePath"]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "Device path not provided")
	}

	m := ns.Mount

	// Device Scan
	err := m.ScanForAttach(devicePath)
	if err != nil {
		klog.V(3).Infof("Failed to ScanForAttach: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to ScanForAttach: %v", err)
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
		} else if blk := volumeCapability.GetBlock(); blk != nil {
			// TODO(#341): Block volume support
			return nil, status.Errorf(codes.Unimplemented, "Block volume support is not yet implemented")
		}
		// Mount
		err = m.FormatAndMount(devicePath, stagingTarget, fsType, options)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(4).Infof("NodeUnstageVolume: called with args %+v", *req)

	stagingTargetPath := req.GetStagingTargetPath()
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Staging Target Path must be provided")
	}

	m := ns.Mount

	notMnt, err := m.IsLikelyNotMountPointDetach(stagingTargetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if notMnt {
		return nil, status.Error(codes.NotFound, "Volume not mounted")
	}

	err = m.UnmountPath(stagingTargetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {

	nodeID, err := getNodeID(ns.Mount, ns.Metadata)
	if err != nil {
		return nil, err
	}
	zone, err := getAvailabilityZoneMetadataService(ns.Metadata)
	topology := &csi.Topology{Segments: map[string]string{topologyKey: zone}}

	return &csi.NodeGetInfoResponse{
		NodeId:             nodeID,
		AccessibleTopology: topology,
	}, nil
}

func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(5).Infof("NodeGetCapabilities called with req: %#v", req)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.Driver.nscap,
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("NodeGetVolumeStats is not yet implemented"))
}

func (ns *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("NodeExpandVolume is not yet implemented"))
}

func getNodeIDMountProvider(m mount.IMount) (string, error) {

	nodeID, err := m.GetInstanceID()
	if err != nil {
		klog.V(3).Infof("Failed to GetInstanceID: %v", err)
		return "", err
	}

	return nodeID, nil
}

func getNodeIDMetdataService(m openstack.IMetadata) (string, error) {

	nodeID, err := m.GetInstanceID()
	if err != nil {
		return "", err
	}
	return nodeID, nil
}

func getAvailabilityZoneMetadataService(m openstack.IMetadata) (string, error) {

	zone, err := m.GetAvailabilityZone()
	if err != nil {
		return "", err
	}
	return zone, nil
}

func getNodeID(mount mount.IMount, metadata openstack.IMetadata) (string, error) {
	// First try to get instance id from mount provider
	nodeID, err := getNodeIDMountProvider(mount)
	if err == nil || nodeID != "" {
		return nodeID, nil
	}

	klog.V(3).Infof("Failed to GetInstanceID from mount data: %v", err)
	klog.V(3).Info("Trying to GetInstanceID from metadata service")
	nodeID, err = getNodeIDMetdataService(metadata)
	if err != nil {
		klog.V(3).Infof("Failed to GetInstanceID from metadata service: %v", err)
		return "", err
	}
	return nodeID, nil
}
