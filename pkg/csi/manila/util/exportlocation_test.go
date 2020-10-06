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
	"testing"

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
)

// Tests FindExportLocation with AnyExportLocation predicate
func TestFindExportLocationAny(t *testing.T) {
	ts := []struct {
		locs             []shares.ExportLocation
		expectedMatchIdx int
	}{
		{
			locs: []shares.ExportLocation{
				{
					Path:        "loc-0",
					IsAdminOnly: true,
					Preferred:   true,
				},
				{
					Path:        "loc-1",
					IsAdminOnly: true,
					Preferred:   false,
				},
			},
			// Expected result `-1` because all locs are admin-only
			expectedMatchIdx: -1,
		},
		{
			locs: []shares.ExportLocation{
				{
					Path:        "loc-0",
					IsAdminOnly: true,
					Preferred:   true,
				},
				{
					Path:        "loc-1",
					IsAdminOnly: false,
					Preferred:   false,
				},
				{
					Path:        "loc-2",
					IsAdminOnly: true,
					Preferred:   true,
				},
			},
			// Expected result `1` because locs[1] is the only non-admin location
			expectedMatchIdx: 1,
		},
		{
			locs: []shares.ExportLocation{
				{
					Path:        "loc-0",
					IsAdminOnly: true,
					Preferred:   true,
				},
				{
					Path:        "loc-1",
					IsAdminOnly: false,
					Preferred:   false,
				},
				{
					Path:        "loc-2",
					IsAdminOnly: false,
					Preferred:   false,
				},
			},
			// Expected result `1` because locs[1] is the first non-admin location
			expectedMatchIdx: 1,
		},
		{
			locs: []shares.ExportLocation{
				{
					Path:        "loc-0",
					IsAdminOnly: true,
					Preferred:   true,
				},
				{
					Path:        "loc-1",
					IsAdminOnly: false,
					Preferred:   false,
				},
				{
					Path:        "loc-2",
					IsAdminOnly: false,
					Preferred:   true,
				},
				{
					Path:        "loc-3",
					IsAdminOnly: false,
					Preferred:   true,
				},
			},
			// Expected result `2` because locs[2] is both non-admin and Preferred,
			// and goes before locs[3] which is also non-admin and Preferred,
			// but its index is higher.
			expectedMatchIdx: 2,
		},
	}

	for i := range ts {
		result, err := FindExportLocation(ts[i].locs, AnyExportLocation)

		if err != nil && result != -1 {
			t.Errorf("test %d: unexpected error: %v", i, err)
		}

		if result != ts[i].expectedMatchIdx {
			t.Errorf("test %d: returned an incorrect index: got %d, expected %d", i, result, ts[i].expectedMatchIdx)
		}
	}
}
