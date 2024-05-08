/*
Copyright 2022 The Kubernetes Authors.

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

package openstack

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_instanceIDFromProviderID(t *testing.T) {
	type args struct {
		providerID string
	}
	tests := []struct {
		name           string
		args           args
		wantInstanceID string
		wantRegion     string
		wantErr        bool
	}{
		{
			name: "it parses region & instanceID correctly from providerID",
			args: args{
				providerID: "openstack://us-east-1/testInstanceID",
			},
			wantInstanceID: "testInstanceID",
			wantRegion:     "us-east-1",
			wantErr:        false,
		},
		{
			name: "it parses instanceID if providerID has empty protocol & no region",
			args: args{
				providerID: "/testInstanceID",
			},
			wantInstanceID: "testInstanceID",
			wantRegion:     "",
			wantErr:        false,
		},
		{
			name: "it returns error in case of invalid providerID format with no region",
			args: args{
				providerID: "openstack://us-east-1-testInstanceID",
			},
			wantInstanceID: "",
			wantRegion:     "",
			wantErr:        true,
		},
		{
			name: "it parses correct instanceID in case the region name is the empty string",
			args: args{
				providerID: "openstack:///testInstanceID",
			},
			wantInstanceID: "testInstanceID",
			wantRegion:     "",
			wantErr:        false,
		},
		{
			name: "it appends openstack:// in case of missing protocol in providerID",
			args: args{
				providerID: "us-east-1/testInstanceID",
			},
			wantInstanceID: "testInstanceID",
			wantRegion:     "us-east-1",
			wantErr:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInstanceID, gotRegion, err := instanceIDFromProviderID(tt.args.providerID)
			assert.Equal(t, tt.wantInstanceID, gotInstanceID)
			assert.Equal(t, tt.wantRegion, gotRegion)
			if tt.wantErr == true {
				assert.ErrorContains(t, err, "didn't match expected format")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_isNodeUnmanaged(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		wantResult bool
	}{
		{
			name:       "openstack node, providerID not set yet",
			providerID: "",
			wantResult: false,
		},
		{
			name:       "openstack node in case of invalid providerID format with no region",
			providerID: "openstack://us-east-1-testInstanceID",
			wantResult: false,
		},
		{
			name:       "openstack node, it parses instanceID has empty protocol & no region",
			providerID: "/testInstanceID",
			wantResult: false,
		},
		{
			name:       "openstack node, it parses correct instanceID in case the region name is the empty string",
			providerID: "openstack:///testInstanceID",
			wantResult: false,
		},
		{
			name:       "openstack node, it parses correct instanceID with region name",
			providerID: "openstack://region/testInstanceID",
			wantResult: false,
		},
		{
			name:       "openstack node in case of missing protocol in providerID",
			providerID: "us-east-1/testInstanceID",
			wantResult: false,
		},
		{
			name:       "non openstack node, providerID has non openstack protocol",
			providerID: "provider:///us-east-1-testInstanceID",
			wantResult: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := isNodeUnmanaged(tt.providerID)
			assert.Equal(t, tt.wantResult, res)
		})
	}
}
