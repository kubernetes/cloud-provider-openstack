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
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
)

const (
	fakeDriverName = "fake"
)

var (
	vendorVersion = "1.2.1"
)

func NewFakeDriver() *CinderDriver {

	driver := NewDriver(FakeNodeID, FakeEndpoint, FakeCluster)

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
