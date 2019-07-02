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
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/klog"
)

type NFS struct{}

var _ ShareAdapter = &NFS{}

func (NFS) GetOrGrantAccess(args *GrantAccessArgs) (*shares.AccessRight, error) {
	// First, check if the access right exists or needs to be created

	rights, err := shares.ListAccessRights(args.ManilaClient, args.Share.ID).Extract()
	if err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); !ok {
			return nil, fmt.Errorf("failed to list access rights: %v", err)
		}
	}

	// Try to find the access right

	for _, r := range rights {
		if r.AccessTo == args.Options.NFSShareClient && r.AccessType == "ip" && r.AccessLevel == "rw" {
			klog.V(4).Infof("IP access right for share %s already exists", args.Share.Name)
			return &r, nil
		}
	}

	// Not found, create it

	return shares.GrantAccess(args.ManilaClient, args.Share.ID, shares.GrantAccessOpts{
		AccessType:  "ip",
		AccessLevel: "rw",
		AccessTo:    args.Options.NFSShareClient,
	}).Extract()
}

func (NFS) BuildVolumeContext(args *VolumeContextArgs) (volumeContext map[string]string, err error) {
	server, share, err := splitExportLocation(args.Location)

	return map[string]string{
		"server": server,
		"share":  share,
	}, nil
}

func (NFS) BuildNodeStageSecret(args *SecretArgs) (secret map[string]string, err error) {
	return nil, nil
}

func (NFS) BuildNodePublishSecret(args *SecretArgs) (secret map[string]string, err error) {
	return nil, nil
}
