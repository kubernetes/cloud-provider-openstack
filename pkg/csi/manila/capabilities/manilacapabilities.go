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

	extraSpecSnapshotSupport                = "snapshot_support"
	extraSpecCreateShareFromSnapshotSupport = "create_share_from_snapshot_support"
)

func GetManilaCapabilities(shareType string, manilaClient manilaclient.Interface) (ManilaCapabilities, error) {
	shareTypes, err := manilaClient.GetShareTypes()
	if err != nil {
		return nil, err
	}

	for _, t := range shareTypes {
		if t.Name == shareType || t.ID == shareType {
			return readManilaCaps(t.ExtraSpecs), nil
		}
	}

	return nil, fmt.Errorf("unknown share type %s", shareType)
}

func readManilaCaps(extraSpecs map[string]interface{}) ManilaCapabilities {
	strToBool := func(ss interface{}) bool {
		var b bool
		if ss != nil {
			if str, ok := ss.(string); ok {
				b, _ = strconv.ParseBool(str)
			}
		}
		return b
	}

	return ManilaCapabilities{
		ManilaCapabilitySnapshot:          strToBool(extraSpecs[extraSpecSnapshotSupport]),
		ManilaCapabilityShareFromSnapshot: strToBool(extraSpecs[extraSpecCreateShareFromSnapshotSupport]),
	}
}
