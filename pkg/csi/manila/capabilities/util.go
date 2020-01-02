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

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/sharetypes"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	clouderrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
)

func shareTypeGetExtraSpecs(shareType string, manilaClient manilaclient.Interface) (sharetypes.ExtraSpecs, error) {
	extraSpecs, err := manilaClient.GetExtraSpecs(shareType)

	if clouderrors.IsNotFound(err) {
		// Maybe shareType is share type name, try to get its ID

		id, err := manilaClient.GetShareTypeIDFromName(shareType)
		if err != nil {
			if clouderrors.IsNotFound(err) {
				return extraSpecs, err
			}

			return extraSpecs, fmt.Errorf("failed to get share type ID for share type %s: %v", shareType, err)
		}

		return manilaClient.GetExtraSpecs(id)
	}

	return extraSpecs, err
}
