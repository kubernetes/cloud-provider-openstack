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
	"strings"

	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
	"k8s.io/klog/v2"
)

func getShareAdapter(proto string) shareadapters.ShareAdapter {
	switch strings.ToUpper(proto) {
	case "CEPHFS":
		return &shareadapters.Cephfs{}
	case "NFS":
		return &shareadapters.NFS{}
	default:
		klog.Fatalf("unknown share adapter %s", proto)
	}

	return nil
}

func getAccessIDs(shareOpts *options.NodeVolumeContext) []string {
	if shareOpts.ShareAccessIDs != "" {
		// Split by comma if multiple
		return strings.Split(shareOpts.ShareAccessIDs, ",")
	} else if shareOpts.ShareAccessID != "" {
		// Backwards compatibility: treat as single-element list
		return []string{shareOpts.ShareAccessID}
	}
	return nil
}

func getAccessRightBasedOnShareAdapter(shareAdapter shareadapters.ShareAdapter, accessRights []shares.AccessRight, shareOpts *options.NodeVolumeContext) (accessRight *shares.AccessRight) {
	switch shareAdapter.(type) {
	case *shareadapters.Cephfs:
		shareAccessIDs := getAccessIDs(shareOpts)
		for _, accessRightID := range shareAccessIDs {
			for _, accessRight := range accessRights {
				if accessRight.ID == accessRightID {
					// TODO: we should add support for getting the node's own IP or Ceph
					// user to avoid unnecessary access rights processing. All the node
					// needs is one cephx user/key to mount the share, so we can return
					// the first access right that matches the share access IDs list.
					return &accessRight
				}
			}
		}
		klog.Fatalf("failed to find access rights %s for cephfs share", shareAccessIDs)
	case *shareadapters.NFS:
		// For NFS, we don't need to use an access right specifically. The controller is
		// already making sure the access rules are properly created.
		return nil
	default:
		klog.Fatalf("unknown share adapter type %T", shareAdapter)
	}
	return nil
}
