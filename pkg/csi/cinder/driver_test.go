/*
Copyright 2017 The Kubernetes Authors.

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

package cinder

import (
	"errors"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

var (
	vendorVersion = "2.0.0"
)

func NewFakeDriver() *Driver {

	driver := NewDriver(&DriverOpts{Endpoint: FakeEndpoint, ClusterID: FakeCluster})

	return driver
}

func TestValidateControllerServiceRequest(t *testing.T) {
	d := NewFakeDriver()

	// Valid requests which require no capabilities
	err := d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_UNKNOWN)
	assert.NoError(t, err)

	// Test controller service publish/unpublish is supported
	err = d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME)
	assert.NoError(t, err)

	// Test controller service create/delete is supported
	err = d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME)
	assert.NoError(t, err)

	// Test controller service list volumes is supported
	err = d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_LIST_VOLUMES)
	assert.NoError(t, err)

	// Test controller service create/delete snapshot is supported
	err = d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT)
	assert.NoError(t, err)

	// Test controller service list snapshot is supported
	err = d.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS)
	assert.NoError(t, err)
}

func TestSetupSnapshotMetadataService(t *testing.T) {
	probe := func(supported bool, err error) *openstack.OpenStackMock {
		m := &openstack.OpenStackMock{}
		m.On("ChangedBlocksSupported").Return(supported, err)
		return m
	}

	tests := []struct {
		name    string
		enabled bool
		clouds  map[string]openstack.IOpenStack
		wantSMS bool
	}{
		{
			name:   "disabled by default",
			clouds: map[string]openstack.IOpenStack{"": probe(true, nil)},
		},
		{
			name:    "supported cloud",
			enabled: true,
			clouds:  map[string]openstack.IOpenStack{"": probe(true, nil)},
			wantSMS: true,
		},
		{
			name:    "every cloud unsupported",
			enabled: true,
			clouds:  map[string]openstack.IOpenStack{"a": probe(false, nil), "b": probe(false, nil)},
		},
		{
			name:    "one cloud supported",
			enabled: true,
			clouds:  map[string]openstack.IOpenStack{"a": probe(false, nil), "b": probe(true, nil)},
			wantSMS: true,
		},
		{
			name:    "probe error counts as unknown",
			enabled: true,
			clouds:  map[string]openstack.IOpenStack{"a": probe(false, nil), "b": probe(false, errors.New("version discovery failed"))},
			wantSMS: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDriver(&DriverOpts{Endpoint: FakeEndpoint, ClusterID: FakeCluster, WithSnapshotMetadata: tc.enabled})
			d.SetupSnapshotMetadataService(tc.clouds)

			assert.Equal(t, tc.wantSMS, d.sms != nil, "service presence")
			assert.Equal(t, tc.wantSMS, d.withSnapshotMetadata, "capability flag")

			if !tc.enabled {
				for _, cloud := range tc.clouds {
					cloud.(*openstack.OpenStackMock).AssertNotCalled(t, "ChangedBlocksSupported")
				}
			}
		})
	}
}
