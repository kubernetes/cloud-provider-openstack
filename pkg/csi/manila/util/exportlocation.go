/*
Copyright 2020 The Kubernetes Authors.
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
	"strings"

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
)

// Predicate type for filtering out export locations.
// It's supplied with the index into the export location slice that's being scanned.
// Should return (true, nil) when search conditions are satisfied,
// and (false, nil) when they are not.
type ExportLocationPredicate func(index int) (match bool, err error)

// Predicate matching any export location
func AnyExportLocation(int) (bool, error) { return true, nil }

// Searches for an export location.
// Returns index of an export location from the `locs` slice that satisfies following rules:
// 1. Location is not admin-only and is not empty
// 2. Location satisfies ExportLocationPredicate
// Search is biased:
// 1. Location.Preferred == true is preferred over Location.Preferred == false
// 2. Locations with lower index are preferred over those with higher index
func FindExportLocation(locs []shares.ExportLocation, pred ExportLocationPredicate) (index int, err error) {
	const invalidIdx = -1
	firstMatchNotPreferred := invalidIdx

	for i := range locs {
		if locs[i].IsAdminOnly || strings.TrimSpace(locs[i].Path) == "" {
			continue
		}

		if hasMatch, err := pred(i); err != nil {
			return i, err
		} else if hasMatch {
			if locs[i].Preferred {
				// This export location is a match and it is Preferred.
				// Can't get better than that!
				return i, nil
			}

			if firstMatchNotPreferred == invalidIdx {
				firstMatchNotPreferred = i
			}
		}
	}

	if firstMatchNotPreferred == invalidIdx {
		err = errors.New("no match, or no suitable non-admin export locations available")
	}

	return firstMatchNotPreferred, err
}
