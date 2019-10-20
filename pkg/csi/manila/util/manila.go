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

package util

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
)

func GetChosenExportLocation(shareID string, manilaClient manilaclient.Interface) (*shares.ExportLocation, error) {
	availableExportLocations, err := manilaClient.GetExportLocations(shareID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve export locations: %v", err)
	}

	return ChooseExportLocation(availableExportLocations)
}

// Chooses one ExportLocation according to the below rules:
// 1. Path is not empty
// 2. IsAdminOnly == false
// 3. Preferred == true are preferred over Preferred == false
// 4. Locations with lower slice index are preferred over locations with higher slice index
func ChooseExportLocation(locs []shares.ExportLocation) (*shares.ExportLocation, error) {
	var (
		foundMatchingNotPreferred = false
		matchingNotPreferred      *shares.ExportLocation
	)

	for _, loc := range locs {
		if loc.IsAdminOnly || strings.TrimSpace(loc.Path) == "" {
			continue
		}

		if loc.Preferred {
			return &loc, nil
		}

		if !foundMatchingNotPreferred {
			matchingNotPreferred = &loc
			foundMatchingNotPreferred = true
		}
	}

	if foundMatchingNotPreferred {
		return matchingNotPreferred, nil
	}

	return nil, errors.New("cannot find any non-admin export location")
}
