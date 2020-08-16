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
	"net"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/runtimeconfig"
	manilautil "k8s.io/cloud-provider-openstack/pkg/csi/manila/util"
	"k8s.io/klog/v2"
)

type NFS struct{}

var _ ShareAdapter = &NFS{}

func (NFS) GetOrGrantAccess(args *GrantAccessArgs) (*shares.AccessRight, error) {
	// First, check if the access right exists or needs to be created

	rights, err := args.ManilaClient.GetAccessRights(args.Share.ID)
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

	return args.ManilaClient.GrantAccess(args.Share.ID, shares.GrantAccessOpts{
		AccessType:  "ip",
		AccessLevel: "rw",
		AccessTo:    args.Options.NFSShareClient,
	})
}

func (NFS) BuildVolumeContext(args *VolumeContextArgs) (volumeContext map[string]string, err error) {
	chosenExportLocationIdx, err := nfsChooseExportLocation(args.Locations)
	if err != nil {
		return nil, fmt.Errorf("failed to choose an export location: %v", err)
	}

	server, share, err := splitExportLocationPath(args.Locations[chosenExportLocationIdx].Path)

	return map[string]string{
		"server": server,
		"share":  share,
	}, err
}

func (NFS) BuildNodeStageSecret(args *SecretArgs) (secret map[string]string, err error) {
	return nil, nil
}

func (NFS) BuildNodePublishSecret(args *SecretArgs) (secret map[string]string, err error) {
	return nil, nil
}

// Tries to choose a suitable export location from the given list.
// Returns index into `locs`.
// Runtime config for NFS is probed first to see if it contains any export location filters.
// Those are then used for selecting the location. If none are defined, the function
// falls back to using manilautil.AnyExportLocation filter.
func nfsChooseExportLocation(locs []shares.ExportLocation) (chosenExportLocationIdx int, err error) {
	var conf *runtimeconfig.RuntimeConfig

	if conf, err = runtimeconfig.Get(); err != nil {
		return -1, fmt.Errorf("failed to read runtime config file %s: %v", runtimeconfig.RuntimeConfigFilename, err)
	}

	if conf != nil {
		if chosenExportLocationIdx, err = nfsMatchExportLocationFromConfig(locs, conf); err != nil {
			return -1, err
		}

		if chosenExportLocationIdx != -1 {
			return chosenExportLocationIdx, err
		}

		// If we got here, it either means there's no NFS config,
		// or it doesn't contain any configuration for export locations.
		// Fall through and choose any suitable location.
	}

	return manilautil.FindExportLocation(locs, manilautil.AnyExportLocation)
}

func nfsMatchExportLocationFromConfig(locs []shares.ExportLocation, conf *runtimeconfig.RuntimeConfig) (idx int, err error) {
	if conf.Nfs != nil {
		if conf.Nfs.MatchExportLocationAddress != "" {
			return nfsMatchExportLocationAddress(locs, conf.Nfs.MatchExportLocationAddress)
		}
	}

	// No NFS export location filters specified
	return -1, nil
}

// Selects an export location with a matching address
func nfsMatchExportLocationAddress(locs []shares.ExportLocation, matchAddress string) (idx int, err error) {
	if ip := net.ParseIP(matchAddress); ip != nil {
		// `matchAddress` is a valid IP, but does not have a prefix.
		// This means we're looking for an exact match in export location addresses.

		// Heuristic to check whether this is an IPv4 or IPv6 address
		if strings.Contains(matchAddress, ".") {
			// IPv4
			matchAddress += "/32"
		} else {
			// IPv6
			matchAddress += "/128"
		}
	}

	_, netIP, err := net.ParseCIDR(matchAddress)
	if err != nil {
		return -1, fmt.Errorf("matchExportLocationAddress filter '%s' is not a CIDR-formatted IP address", matchAddress)
	}

	idx, err = manilautil.FindExportLocation(locs, func(i int) (bool, error) {
		addr, _, err := splitExportLocationPath(locs[i].Path)
		if err != nil {
			return false, err
		}

		hostIP := net.ParseIP(addr)
		if hostIP == nil {
			return false, fmt.Errorf("IP '%s' in export location path %s is invalid", addr, locs[i].Path)
		}

		return netIP.Contains(hostIP), nil
	})

	if err != nil {
		return -1, fmt.Errorf("matchExportLocationAddress filter '%s': %v", matchAddress, err)
	}

	return idx, nil
}
