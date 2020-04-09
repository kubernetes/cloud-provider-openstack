// +build !linux

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

package blockdevice

import (
	"errors"
)

func IsBlockDevice(path string) (bool, error) {
	return false, errors.New("IsBlockDevice is not implemented for this OS")
}

func GetBlockDeviceSize(path string) (int64, error) {
	return -1, errors.New("GetBlockDeviceSize is not implemented for this OS")
}

func RescanBlockDeviceGeometry(devicePath string, deviceMountPath string, newSize int64) error {
	return errors.New("RescanBlockDeviceGeometry is not implemented for this OS")
}
