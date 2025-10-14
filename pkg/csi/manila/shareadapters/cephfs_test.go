/*
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
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
)

func TestExtractFsNameFromMountOptions(t *testing.T) {
	testCases := []struct {
		name     string
		share    *shares.Share
		expected string
	}{
		{
			name: "Valid fs in mount options",
			share: &shares.Share{
				ID: "test-share-1",
				Metadata: map[string]string{
					"__mount_options": "fs=my_cephfs,rw,relatime",
				},
			},
			expected: "my_cephfs",
		},
		{
			name: "fs with spaces around comma",
			share: &shares.Share{
				ID: "test-share-2",
				Metadata: map[string]string{
					"__mount_options": "rw, fs=production_fs , relatime",
				},
			},
			expected: "production_fs",
		},
		{
			name: "fs at the end",
			share: &shares.Share{
				ID: "test-share-4",
				Metadata: map[string]string{
					"__mount_options": "rw,relatime,fs=end_fs",
				},
			},
			expected: "end_fs",
		},
		{
			name: "fs with underscores and numbers",
			share: &shares.Share{
				ID: "test-share-5",
				Metadata: map[string]string{
					"__mount_options": "fs=cephfs_vol_123,rw",
				},
			},
			expected: "cephfs_vol_123",
		},
		{
			name: "fs with hyphens",
			share: &shares.Share{
				ID: "test-share-6",
				Metadata: map[string]string{
					"__mount_options": "fs=my-ceph-fs,rw,relatime",
				},
			},
			expected: "my-ceph-fs",
		},
		{
			name: "fs with dots",
			share: &shares.Share{
				ID: "test-share-7",
				Metadata: map[string]string{
					"__mount_options": "fs=ceph.filesystem.name,rw",
				},
			},
			expected: "ceph.filesystem.name",
		},
		{
			name: "fs with mixed whitespace",
			share: &shares.Share{
				ID: "test-share-8",
				Metadata: map[string]string{
					"__mount_options": "  rw  ,   fs=whitespace_test   ,  relatime  ",
				},
			},
			expected: "whitespace_test",
		},
		{
			name: "fs with empty value",
			share: &shares.Share{
				ID: "test-share-9",
				Metadata: map[string]string{
					"__mount_options": "rw,fs=,relatime",
				},
			},
			expected: "",
		},
		{
			name: "fs with complex filesystem name",
			share: &shares.Share{
				ID: "test-share-10",
				Metadata: map[string]string{
					"__mount_options": "fs=production_cluster_01.cephfs_vol,rw,relatime",
				},
			},
			expected: "production_cluster_01.cephfs_vol",
		},
		{
			name: "No fs in mount options",
			share: &shares.Share{
				ID: "test-share-11",
				Metadata: map[string]string{
					"__mount_options": "rw,relatime,noatime",
				},
			},
			expected: "",
		},
		{
			name: "No __mount_options metadata",
			share: &shares.Share{
				ID: "test-share-14",
				Metadata: map[string]string{
					"other_key": "other_value",
				},
			},
			expected: "",
		},
		{
			name: "Empty __mount_options",
			share: &shares.Share{
				ID: "test-share-15",
				Metadata: map[string]string{
					"__mount_options": "",
				},
			},
			expected: "",
		},
		{
			name:     "Nil share",
			share:    nil,
			expected: "",
		},
		{
			name: "Nil metadata",
			share: &shares.Share{
				ID:       "test-share-18",
				Metadata: nil,
			},
			expected: "",
		},
		{
			name: "fs with special characters",
			share: &shares.Share{
				ID: "test-share-19",
				Metadata: map[string]string{
					"__mount_options": "fs=fs@cluster#1,rw",
				},
			},
			expected: "fs@cluster#1",
		},
		{
			name: "fs with equals in value",
			share: &shares.Share{
				ID: "test-share-20",
				Metadata: map[string]string{
					"__mount_options": "fs=fs=with=equals,rw",
				},
			},
			expected: "fs=with=equals",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractFsNameFromMountOptions(tc.share)
			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestCephfsBuildVolumeContextWithFsName(t *testing.T) {
	adapter := &Cephfs{}

	// Test share with fs in metadata
	shareWithFsName := &shares.Share{
		ID: "test-share-with-fsname",
		Metadata: map[string]string{
			"__mount_options": "fs=test_cephfs,rw,relatime",
		},
	}

	// Test share without fs
	shareWithoutFsName := &shares.Share{
		ID: "test-share-without-fsname",
		Metadata: map[string]string{
			"other_metadata": "value",
		},
	}

	exportLocations := []shares.ExportLocation{
		{
			Path: "10.0.0.1:6789,10.0.0.2:6789:/volumes/_nogroup/test-volume-id",
		},
	}

	testCases := []struct {
		name           string
		share          *shares.Share
		expectFsName   bool
		expectedFsName string
	}{
		{
			name:           "Share with fs",
			share:          shareWithFsName,
			expectFsName:   true,
			expectedFsName: "test_cephfs",
		},
		{
			name:         "Share without fs",
			share:        shareWithoutFsName,
			expectFsName: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args := &VolumeContextArgs{
				Locations: exportLocations,
				Share:     tc.share,
				Options:   &options.NodeVolumeContext{},
			}

			volCtx, err := adapter.BuildVolumeContext(args)
			if err != nil {
				t.Errorf("BuildVolumeContext failed: %v", err)
				return
			}

			fsName, exists := volCtx["fsName"]
			if tc.expectFsName {
				if !exists {
					t.Error("Expected fsName in volume context, but it was not found")
				} else if fsName != tc.expectedFsName {
					t.Errorf("Expected fsName '%s', got '%s'", tc.expectedFsName, fsName)
				}
			} else {
				if exists {
					t.Errorf("Did not expect fsName in volume context, but found '%s'", fsName)
				}
			}

			// Verify other expected fields are still present
			expectedFields := []string{"monitors", "rootPath", "mounter", "provisionVolume"}
			for _, field := range expectedFields {
				if _, exists := volCtx[field]; !exists {
					t.Errorf("Expected field '%s' not found in volume context", field)
				}
			}
		})
	}
}
