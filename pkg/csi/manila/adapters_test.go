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

package manila

import (
	"testing"

	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
)

func TestGetAccessIDs(t *testing.T) {
	tests := []struct {
		name     string
		opts     *options.NodeVolumeContext
		expected []string
	}{
		{
			name:     "shareAccessIDs takes precedence over shareAccessID",
			opts:     &options.NodeVolumeContext{ShareAccessIDs: "new-1,new-2", ShareAccessID: "old-1"},
			expected: []string{"new-1", "new-2"},
		},
		{
			name:     "only shareAccessIDs",
			opts:     &options.NodeVolumeContext{ShareAccessIDs: "id-1,id-2,id-3"},
			expected: []string{"id-1", "id-2", "id-3"},
		},
		{
			name:     "only shareAccessID (deprecated)",
			opts:     &options.NodeVolumeContext{ShareAccessID: "id-1"},
			expected: []string{"id-1"},
		},
		{
			name:     "neither set",
			opts:     &options.NodeVolumeContext{},
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getAccessIDs(tc.opts)
			if len(result) != len(tc.expected) {
				t.Fatalf("got %v, want %v", result, tc.expected)
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("index %d: got %q, want %q", i, result[i], tc.expected[i])
				}
			}
		})
	}
}
