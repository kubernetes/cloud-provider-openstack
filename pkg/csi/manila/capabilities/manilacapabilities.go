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

package capabilities

import (
	"fmt"
	"strconv"
	"sync"

	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
)

type (
	ManilaCapability   int
	ManilaCapabilities map[ManilaCapability]bool
)

const (
	ManilaCapabilityNone ManilaCapability = iota
	ManilaCapabilitySnapshot
	ManilaCapabilityShareFromSnapshot
)

var (
	manilaCapabilitiesByShareTypeIDMtx sync.RWMutex
	manilaCapabilitiesByShareTypeID    = make(map[string]ManilaCapabilities)
)

func GetManilaCapabilities(shareType string, manilaClient manilaclient.Interface) (ManilaCapabilities, error) {
	const (
		snapshotSupport                = "snapshot_support"
		createShareFromSnapshotSupport = "create_share_from_snapshot_support"
	)

	manilaCapabilitiesByShareTypeIDMtx.RLock()
	caps, ok := manilaCapabilitiesByShareTypeID[shareType]
	manilaCapabilitiesByShareTypeIDMtx.RUnlock()

	strToBool := func(ss interface{}) bool {
		var b bool
		if ss != nil {
			if str, ok := ss.(string); ok {
				b, _ = strconv.ParseBool(str)
			}
		}
		return b
	}

	if !ok {
		manilaCapabilitiesByShareTypeIDMtx.Lock()
		defer manilaCapabilitiesByShareTypeIDMtx.Unlock()

		extraSpecs, err := shareTypeGetExtraSpecs(shareType, manilaClient)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve extra specs: %v", err)
		}

		caps = ManilaCapabilities{
			ManilaCapabilitySnapshot:          strToBool(extraSpecs[snapshotSupport]),
			ManilaCapabilityShareFromSnapshot: strToBool(extraSpecs[createShareFromSnapshotSupport]),
		}

		manilaCapabilitiesByShareTypeID[shareType] = caps
	}

	return caps, nil
}
