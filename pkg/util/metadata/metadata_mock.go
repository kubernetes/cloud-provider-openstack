/*
Copyright 2016 The Kubernetes Authors.

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

package metadata

import (
	"fmt"

	"github.com/stretchr/testify/mock"
)

type MetadataMock struct {
	mock.Mock
}

var fakeMetadata = Metadata{
	UUID:             "83679162-1378-4288-a2d4-70e13ec132aa",
	Name:             "test",
	AvailabilityZone: "testaz",
}

func (_m *MetadataMock) GetFromConfigDrive(metadataVersion string) (*Metadata, error) {
	ret := _m.Called(metadataVersion)

	var r0 *Metadata
	if rf, ok := ret.Get(0).(func(string) *Metadata); ok {
		r0 = rf(metadataVersion)
	} else {
		r0 = &fakeMetadata
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(metadataVersion)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func (_m *MetadataMock) GetFromMetadataService(metadataVersion string) (*Metadata, error) {
	ret := _m.Called(metadataVersion)

	var r0 *Metadata
	if rf, ok := ret.Get(0).(func(string) *Metadata); ok {
		r0 = rf(metadataVersion)
	} else {
		r0 = &fakeMetadata
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(metadataVersion)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func (_m *MetadataMock) Get(order string) (*Metadata, error) {
	ret := _m.Called(order)

	var r0 *Metadata
	if rf, ok := ret.Get(0).(func(string) *Metadata); ok {
		r0 = rf(order)
	} else {
		r0 = &fakeMetadata
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(order)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetInstanceID from metadata service and config drive
func (_m *MetadataMock) GetInstanceID(order string) (string, error) {
	ret := _m.Called(order)

	var r0 string
	if rf, ok := ret.Get(0).(func(string) string); ok {
		r0 = rf(order)
	} else {
		r0 = fakeMetadata.UUID
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(order)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetAvailabilityZone returns zone from metadata service and config drive
func (_m *MetadataMock) GetAvailabilityZone(order string) (string, error) {
	ret := _m.Called(order)

	var r0 string
	if rf, ok := ret.Get(0).(func(string) string); ok {
		r0 = rf(order)
	} else {
		r0 = fakeMetadata.AvailabilityZone
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(order)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

func (_m *MetadataMock) GetDefaultMetadataSearchOrder() string {
	return fmt.Sprintf("%s,%s", MetadataID, ConfigDriveID)
}
