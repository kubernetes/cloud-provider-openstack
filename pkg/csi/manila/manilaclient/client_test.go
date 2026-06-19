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

package manilaclient

import (
	"testing"
)

func TestAccessRulesVersionGating(t *testing.T) {
	tests := []struct {
		name                  string
		serverMaxMicroversion string
		wantNewAPI            bool
	}{
		{
			name:                  "server supports 2.45 exactly",
			serverMaxMicroversion: "2.45",
			wantNewAPI:            true,
		},
		{
			name:                  "server supports higher than 2.45",
			serverMaxMicroversion: "2.94",
			wantNewAPI:            true,
		},
		{
			name:                  "server only supports 2.44",
			serverMaxMicroversion: "2.44",
			wantNewAPI:            false,
		},
		{
			name:                  "server only supports 2.37",
			serverMaxMicroversion: "2.37",
			wantNewAPI:            false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useNewAPI := !compareManilaVersionsLessThan(tc.serverMaxMicroversion, accessRulesGETMicroversion)
			if useNewAPI != tc.wantNewAPI {
				t.Errorf("server %s: expected new API = %v, got %v", tc.serverMaxMicroversion, tc.wantNewAPI, useNewAPI)
			}
		})
	}
}
