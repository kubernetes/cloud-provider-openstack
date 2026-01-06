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

package shareadapters

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"k8s.io/apimachinery/pkg/util/wait"
	manilautil "k8s.io/cloud-provider-openstack/pkg/csi/manila/util"
	"k8s.io/cloud-provider-openstack/pkg/util"
	"k8s.io/klog/v2"
)

type Cephfs struct{}

var _ ShareAdapter = &Cephfs{}

func (Cephfs) GetOrGrantAccesses(ctx context.Context, args *GrantAccessArgs) ([]shares.AccessRight, error) {
	// First, check if the access right exists or needs to be created

	rights, err := args.ManilaClient.GetAccessRights(ctx, args.Share.ID)
	if err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); !ok {
			return nil, fmt.Errorf("failed to list access rights: %v", err)
		}
	}

	accessToList := []string{args.Share.Name}
	if args.Options.CephfsClientID != "" {
		accessToList = strings.Split(args.Options.CephfsClientID, ",")
	}

	// TODO: add support for getting the exact client ID that the node will use.
	// For now, we use the first client ID in the list and it should be enough,
	// considering our context with the nodes.
	accessRightClient := accessToList[0]
	var accessRight *shares.AccessRight

	// Try to find the access right.
	for _, r := range rights {
		if r.AccessTo == accessRightClient && r.AccessType == "cephx" && r.AccessLevel == "rw" {
			klog.V(4).Infof("cephx access right for share %s already exists", args.Share.Name)
			accessRight = &r
			break
		}
	}

	// Not found, create it
	if accessRight == nil {
		result, err := args.ManilaClient.GrantAccess(ctx, args.Share.ID, shares.GrantAccessOpts{
			AccessType:  "cephx",
			AccessLevel: "rw",
			AccessTo:    accessRightClient,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to grant access right: %v", err)
		}
		if result.AccessKey == "" {
			// Wait till a ceph key is assigned to the access right
			backoff := wait.Backoff{
				Duration: time.Second * 5,
				Factor:   1.2,
				Steps:    10,
			}
			wait_err := wait.ExponentialBackoff(backoff, func() (bool, error) {
				rights, err := args.ManilaClient.GetAccessRights(ctx, args.Share.ID)
				if err != nil {
					return false, fmt.Errorf("error get access rights for share %s: %v", args.Share.ID, err)
				}
				if len(rights) == 0 {
					return false, fmt.Errorf("cannot find the access right we've just created")
				}
				for _, r := range rights {
					if r.AccessTo == accessRightClient && r.AccessKey != "" {
						accessRight = &r
						return true, nil
					}
				}
				klog.V(4).Infof("Access key for %s is not set yet, retrying...", accessRightClient)
				return false, nil
			})
			if wait_err != nil {
				return nil, fmt.Errorf("timed out while attempting to get access rights for share %s: %v", args.Share.ID, err)
			}
		}
	}
	return []shares.AccessRight{*accessRight}, nil

}

func (Cephfs) BuildVolumeContext(args *VolumeContextArgs) (volumeContext map[string]string, err error) {
	chosenExportLocationIdx, err := manilautil.FindExportLocation(args.Locations, manilautil.AnyExportLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to choose an export location: %v", err)
	}

	monitors, rootPath, err := splitExportLocationPath(args.Locations[chosenExportLocationIdx].Path)

	volCtx := map[string]string{
		"monitors":        monitors,
		"rootPath":        rootPath,
		"mounter":         args.Options.CephfsMounter,
		"provisionVolume": "false",
	}

	if args.Options.CephfsKernelMountOptions != "" {
		volCtx["kernelMountOptions"] = args.Options.CephfsKernelMountOptions
	}

	if args.Options.CephfsFuseMountOptions != "" {
		volCtx["fuseMountOptions"] = args.Options.CephfsFuseMountOptions
	}

	// Extract fs_name from __mount_options metadata if available
	// This is used by the ceph-csi plugin:
	// https://github.com/ceph/ceph-csi/blob/521a90c041acbe0fc68db8ecb27ef84da5af87dc/docs/static-pvc.md?plain=1#L287
	if fsName := extractFsNameFromMountOptions(args.Share); fsName != "" {
		volCtx["fsName"] = fsName
		klog.V(4).Infof("Found fs_name in share metadata: %s", fsName)
	}

	return volCtx, err
}

// extractFsNameFromMountOptions extracts the fs from __mount_options metadata
// The __mount_options metadata contains mount options including fs for CephFS
func extractFsNameFromMountOptions(share *shares.Share) string {
	if share == nil || share.Metadata == nil {
		return ""
	}

	mountOptions, exists := share.Metadata["__mount_options"]
	if !exists {
		klog.V(4).Infof("No __mount_options metadata found in share %s", share.ID)
		return ""
	}

	// Mount options are typically comma-separated key=value pairs
	// Example: "fs=myfs,other_option=value"
	options := util.SplitTrim(mountOptions, ',')
	for _, option := range options {
		if strings.HasPrefix(option, "fs=") {
			fsName := strings.TrimPrefix(option, "fs=")
			return fsName
		}
	}

	klog.V(4).Infof("No fs found in __mount_options metadata for share %s: %s", share.ID, mountOptions)
	return ""
}

func (Cephfs) BuildNodeStageSecret(args *SecretArgs) (secret map[string]string, err error) {
	return map[string]string{
		"userID":  args.AccessRight.AccessTo,
		"userKey": args.AccessRight.AccessKey,
	}, nil
}

func (Cephfs) BuildNodePublishSecret(args *SecretArgs) (secret map[string]string, err error) {
	return nil, nil
}
