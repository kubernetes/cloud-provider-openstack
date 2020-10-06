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
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
)

type GrantAccessArgs struct {
	ManilaClient manilaclient.Interface
	Share        *shares.Share
	Options      *options.ControllerVolumeContext
}

type VolumeContextArgs struct {
	// Share adapters are responsible for choosing
	// an export location when building a volume context.
	Locations []shares.ExportLocation

	Options *options.NodeVolumeContext
}

type SecretArgs struct {
	AccessRight *shares.AccessRight
}

type ShareAdapter interface {
	// GetOrGrantAccess first tries to retrieve an access right for args.Share.
	// An access right is created for the share in case it doesn't exist yet.
	// Returns an existing or new access right for args.Share.
	GetOrGrantAccess(args *GrantAccessArgs) (accessRight *shares.AccessRight, err error)

	// BuildVolumeContext builds a volume context map that's passed to NodeStageVolumeRequest and NodePublishVolumeRequest
	BuildVolumeContext(args *VolumeContextArgs) (volumeContext map[string]string, err error)

	// Builds secret map for NodeStageVolumeRequest
	BuildNodeStageSecret(args *SecretArgs) (secret map[string]string, err error)

	// Builds secret map for NodePublishVolumeRequest
	BuildNodePublishSecret(args *SecretArgs) (secret map[string]string, err error)
}
