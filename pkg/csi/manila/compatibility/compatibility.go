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

package compatibility

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/capabilities"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/csiclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
)

type Layer interface {
	SupplementCapability(compatOpts *options.CompatibilityOptions, dstShare *shares.Share, dstShareAccessRight *shares.AccessRight, req *csi.CreateVolumeRequest, fwdEndpoint string, manilaClient manilaclient.Interface, csiClientBuilder csiclient.Builder) error
}

// Certain share protocols may not support certain Manila capabilities
// in a given share type. This map forms a compatibility layer which
// fills in the feature gap with in-driver functionality.
var compatCaps = map[string]map[capabilities.ManilaCapability]Layer{}

func FindCompatibilityLayer(shareProto string, wantsCap capabilities.ManilaCapability, shareTypeCaps capabilities.ManilaCapabilities) Layer {
	if layers, ok := compatCaps[shareProto]; ok {
		if hasCapability := shareTypeCaps[wantsCap]; !hasCapability {
			if compatCapability, ok := layers[wantsCap]; ok {
				return compatCapability
			}
		}
	}

	return nil
}
