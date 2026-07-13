/*
Copyright 2026 The Kubernetes Authors.

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

package options

import (
	"testing"
)

func TestNewNodeVolumeContextShareAccessIDBackwardsCompat(t *testing.T) {
	tests := []struct {
		name          string
		data          map[string]string
		wantAccessID  string
		wantAccessIDs string
		wantErr       bool
	}{
		{
			name: "only shareAccessIDs set",
			data: map[string]string{
				"shareID":        "share-1",
				"shareAccessIDs": "access-1,access-2",
			},
			wantAccessIDs: "access-1,access-2",
		},
		{
			name: "only shareAccessID set (deprecated)",
			data: map[string]string{
				"shareID":       "share-1",
				"shareAccessID": "access-1",
			},
			wantAccessID: "access-1",
		},
		{
			name: "both set, should not error",
			data: map[string]string{
				"shareID":        "share-1",
				"shareAccessID":  "access-old",
				"shareAccessIDs": "access-new-1,access-new-2",
			},
			wantAccessID:  "access-old",
			wantAccessIDs: "access-new-1,access-new-2",
		},
		{
			name: "neither set",
			data: map[string]string{
				"shareID": "share-1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := NewNodeVolumeContext(tc.data)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr {
				return
			}
			if opts.ShareAccessID != tc.wantAccessID {
				t.Errorf("ShareAccessID = %q, want %q", opts.ShareAccessID, tc.wantAccessID)
			}
			if opts.ShareAccessIDs != tc.wantAccessIDs {
				t.Errorf("ShareAccessIDs = %q, want %q", opts.ShareAccessIDs, tc.wantAccessIDs)
			}
		})
	}
}
