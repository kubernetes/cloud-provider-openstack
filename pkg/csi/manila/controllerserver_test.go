/*
Copyright 2021 The Kubernetes Authors.
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
	"fmt"
	"testing"
)

func TestPrepareShareMetadata(t *testing.T) {
	ts := []struct {
		appendShareMetadata string
		cluster             string
		expectedResult      map[string]string
		expectedError       bool
	}{
		{
			// Empty metadata and cluster
			appendShareMetadata: "",
			cluster:             "",
			expectedResult:      nil,
			expectedError:       false,
		},
		{
			// Existing metadata and empty cluster
			appendShareMetadata: "{\"keyA\": \"valueA\", \"keyB\": \"valueB\"}",
			cluster:             "",
			expectedResult:      map[string]string{"keyA": "valueA", "keyB": "valueB"},
			expectedError:       false,
		},
		{
			// Just cluster and no metadata
			appendShareMetadata: "",
			cluster:             "MyCluster",
			expectedResult:      map[string]string{clusterMetadataKey: "MyCluster"},
			expectedError:       false,
		},
		{
			// Both metadata and cluster
			appendShareMetadata: "{\"keyA\": \"valueA\", \"keyB\": \"valueB\"}",
			cluster:             "MyCluster",
			expectedResult:      map[string]string{"keyA": "valueA", "keyB": "valueB", clusterMetadataKey: "MyCluster"},
			expectedError:       false,
		},
		{
			// Overwrite cluster
			appendShareMetadata: "{\"keyA\": \"valueA\", \"" + clusterMetadataKey + "\": \"SomeValue\"}",
			cluster:             "MyCluster",
			expectedResult:      map[string]string{"keyA": "valueA", clusterMetadataKey: "SomeValue"},
			expectedError:       false,
		},
		{
			// Incorrect metadata
			appendShareMetadata: "INVALID",
			cluster:             "MyCluster",
			expectedResult:      nil,
			expectedError:       true,
		},
	}

	for i := range ts {
		result, err := prepareShareMetadata(ts[i].appendShareMetadata, ts[i].cluster)

		if err != nil && !ts[i].expectedError {
			t.Errorf("test %d: unexpected error: %v", i, err)
		}

		if fmt.Sprint(result) != fmt.Sprint(ts[i].expectedResult) {
			t.Errorf("test %d: returned an incorrect result: got %#v, expected %#v", i, result, ts[i].expectedResult)
		}
	}
}
