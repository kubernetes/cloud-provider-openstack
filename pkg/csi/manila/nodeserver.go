/*
Copyright 2019 The Kubernetes Authors.

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

package manila

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
	"k8s.io/klog"
)

type nodeServer struct {
	d *Driver

	supportsNodeStage bool
	// The result of NodeStageVolume is stashed away for NodePublishVolume(s) that will follow
	nodeStageCache    map[volumeID]stageCacheEntry
	nodeStageCacheMtx sync.RWMutex
}

type stageCacheEntry struct {
	volumeContext map[string]string
	stageSecret   map[string]string
	publishSecret map[string]string
}

func (ns *nodeServer) buildVolumeContext(volID volumeID, shareOpts *options.NodeVolumeContext, osOpts *options.OpenstackOptions) (
	volumeContext map[string]string, accessRight *shares.AccessRight, err error,
) {
	manilaClient, err := newManilaV2Client(osOpts)
	if err != nil {
		return nil, nil, status.Errorf(codes.Unauthenticated, "failed to create Manila v2 client: %v", err)
	}

	// Retrieve the share by its ID or name

	var share *shares.Share

	if shareOpts.ShareID != "" {
		share, err = getShareByID(shareOpts.ShareID, manilaClient)
		if err != nil {
			return nil, nil, status.Errorf(codes.InvalidArgument, "failed to retrieve volume %s (share ID %s): %v",
				volID, shareOpts.ShareID, err)
		}
	} else {
		share, err = getShareByName(shareOpts.ShareName, manilaClient)
		if err != nil {
			return nil, nil, status.Errorf(codes.InvalidArgument, "failed to retrieve volume %s (share name %s): %v",
				volID, shareOpts.ShareName, err)
		}
	}

	// Verify the plugin supports this share

	if strings.ToLower(share.ShareProto) != strings.ToLower(ns.d.shareProto) {
		return nil, nil, status.Errorf(codes.InvalidArgument,
			"wrong share protocol %s for volume %s (share ID %s), the plugin is set to operate in %s",
			share.ShareProto, volID, share.ID, ns.d.shareProto)
	}

	if share.Status != "available" {
		return nil, nil, status.Errorf(codes.InvalidArgument, "invalid share status for volume %s (share ID %s): expected 'available', got '%s'",
			volID, share.ID, share.Status)
	}

	// Get export locations and choose one

	availableExportLocations, err := shares.GetExportLocations(manilaClient, share.ID).Extract()
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "failed to retrieve export locations for volume %s (share ID %s): %v",
			volID, share.ID, err)
	}

	chosenExportLocation, err := chooseExportLocation(availableExportLocations)
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "failed to choose an export locations for volume %s (share ID %s): %v",
			volID, share.ID, err)
	}

	accessRights, err := shares.ListAccessRights(manilaClient, share.ID).Extract()
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "failed to list access rights for volume %s (share ID %s): %v",
			volID, share.ID, err)
	}

	// Get the access right for this share

	for i := range accessRights {
		if accessRights[i].ID == shareOpts.ShareAccessID {
			accessRight = &accessRights[i]
			break
		}
	}

	if accessRight == nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "cannot find access right %s for volume %s (share ID %s)",
			shareOpts.ShareAccessID, volID, share.ID)
	}

	// Build volume context for fwd plugin

	sa := getShareAdapter(ns.d.shareProto)

	volumeContext, err = sa.BuildVolumeContext(&shareadapters.VolumeContextArgs{Location: &chosenExportLocation, Options: shareOpts})
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "failed to build volume context for volume %s: %v", volID, err)
	}

	return
}

func buildNodePublishSecret(accessRight *shares.AccessRight, sa shareadapters.ShareAdapter, volID volumeID) (map[string]string, error) {
	secret, err := sa.BuildNodePublishSecret(&shareadapters.SecretArgs{AccessRight: accessRight})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to build publish secret for volume %s: %v", volID, err)
	}

	return secret, nil
}

func buildNodeStageSecret(accessRight *shares.AccessRight, sa shareadapters.ShareAdapter, volID volumeID) (map[string]string, error) {
	secret, err := sa.BuildNodeStageSecret(&shareadapters.SecretArgs{AccessRight: accessRight})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to build stage secret for volume %s: %v", volID, err)
	}

	return secret, nil
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if err := validateNodePublishVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Configuration

	shareOpts, err := options.NewNodeVolumeContext(req.GetVolumeContext())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid volume parameters: %v", err)
	}

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid volume secret: %v", err)
	}

	volID := volumeID(req.GetVolumeId())

	var (
		accessRight       *shares.AccessRight
		volumeCtx, secret map[string]string
	)

	if ns.supportsNodeStage {
		// STAGE_UNSTAGE_VOLUME capability is enabled, NodeStageVolume should've already built the staging data

		ns.nodeStageCacheMtx.RLock()
		cacheEntry, ok := ns.nodeStageCache[volID]
		ns.nodeStageCacheMtx.RUnlock()

		if ok {
			volumeCtx, secret = cacheEntry.volumeContext, cacheEntry.publishSecret
		} else {
			klog.Warningf("STAGE_UNSTAGE_VOLUME capability is enabled, but node stage cache doesn't contain an entry for %s - this is most likely a bug! Rebuilding staging data anyway...", volID)
			volumeCtx, accessRight, err = ns.buildVolumeContext(volID, shareOpts, osOpts)
			if err == nil {
				secret, err = buildNodePublishSecret(accessRight, getShareAdapter(ns.d.shareProto), volID)
			}
		}
	} else {
		volumeCtx, accessRight, err = ns.buildVolumeContext(volID, shareOpts, osOpts)
		if err == nil {
			secret, err = buildNodePublishSecret(accessRight, getShareAdapter(ns.d.shareProto), volID)
		}
	}

	if err != nil {
		return nil, err
	}

	// Forward the RPC

	csiConn, err := grpcConnect(ctx, ns.d.fwdEndpoint)
	if err != nil {
		return nil, status.Error(codes.Unavailable, fmtGrpcConnError(ns.d.fwdEndpoint, err))
	}

	req.Secrets = secret
	req.VolumeContext = volumeCtx

	return csiNodePublishVolume(ctx, csiConn, req)
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if err := validateNodeUnpublishVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	csiConn, err := grpcConnect(ctx, ns.d.fwdEndpoint)
	if err != nil {
		return nil, status.Error(codes.Unavailable, fmtGrpcConnError(ns.d.fwdEndpoint, err))
	}

	return csiNodeUnpublishVolume(ctx, csiConn, req)
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if err := validateNodeStageVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Configuration

	var (
		accessRight                *shares.AccessRight
		volumeCtx                  map[string]string
		stageSecret, publishSecret map[string]string
		err                        error
	)

	shareOpts, err := options.NewNodeVolumeContext(req.GetVolumeContext())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid volume parameters: %v", err)
	}

	osOpts, err := options.NewOpenstackOptions(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid volume secret: %v", err)
	}

	volID := volumeID(req.GetVolumeId())

	ns.nodeStageCacheMtx.Lock()
	if cacheEntry, ok := ns.nodeStageCache[volID]; ok {
		volumeCtx, stageSecret = cacheEntry.volumeContext, cacheEntry.stageSecret
	} else {
		volumeCtx, accessRight, err = ns.buildVolumeContext(volID, shareOpts, osOpts)

		if err == nil {
			stageSecret, err = buildNodeStageSecret(accessRight, getShareAdapter(ns.d.shareProto), volID)
		}

		if err == nil {
			publishSecret, err = buildNodePublishSecret(accessRight, getShareAdapter(ns.d.shareProto), volID)
		}

		ns.nodeStageCache[volID] = stageCacheEntry{volumeContext: volumeCtx, stageSecret: stageSecret, publishSecret: publishSecret}
	}
	ns.nodeStageCacheMtx.Unlock()

	if err != nil {
		return nil, err
	}

	// Forward the RPC

	csiConn, err := grpcConnect(ctx, ns.d.fwdEndpoint)
	if err != nil {
		return nil, status.Error(codes.Unavailable, fmtGrpcConnError(ns.d.fwdEndpoint, err))
	}

	req.Secrets = stageSecret
	req.VolumeContext = volumeCtx

	return csiNodeStageVolume(ctx, csiConn, req)
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if err := validateNodeUnstageVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ns.nodeStageCacheMtx.Lock()
	delete(ns.nodeStageCache, volumeID(req.VolumeId))
	ns.nodeStageCacheMtx.Unlock()

	csiConn, err := grpcConnect(ctx, ns.d.fwdEndpoint)
	if err != nil {
		return nil, status.Error(codes.Unavailable, fmtGrpcConnError(ns.d.fwdEndpoint, err))
	}

	return csiNodeUnstageVolume(ctx, csiConn, req)
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: ns.d.nodeID,
	}, nil
}

func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.d.nscaps,
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (ns *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// Chooses one ExportLocation according to the below rules:
// 1. Path is not empty
// 2. IsAdminOnly == false
// 3. Preferred == true are preferred over Preferred == false
// 4. Locations with lower slice index are preferred over locations with higher slice index
func chooseExportLocation(locs []shares.ExportLocation) (shares.ExportLocation, error) {
	if len(locs) == 0 {
		return shares.ExportLocation{}, fmt.Errorf("export locations list is empty")
	}

	var (
		foundMatchingNotPreferred = false
		matchingNotPreferred      shares.ExportLocation
	)

	for _, loc := range locs {
		if loc.IsAdminOnly || strings.TrimSpace(loc.Path) == "" {
			continue
		}

		if loc.Preferred {
			return loc, nil
		}

		if !foundMatchingNotPreferred {
			matchingNotPreferred = loc
			foundMatchingNotPreferred = true
		}
	}

	if foundMatchingNotPreferred {
		return matchingNotPreferred, nil
	}

	return shares.ExportLocation{}, fmt.Errorf("cannot find any non-admin export location")
}
