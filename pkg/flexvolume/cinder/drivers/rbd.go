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

package drivers

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"k8s.io/klog"
)

const (
	// DriverName used in cinder-flexvolume
	DriverName = "rbd"
)

// RBDDriver is the driver of cinder-flexvolume
type RBDDriver struct {
}

// RBDVolume used by Cinder
type RBDVolume struct {
	Keyring     string   `json:"keyring"`
	AuthEnabled bool     `json:"auth_enabled"`
	AuthUser    string   `json:"auth_username"`
	Hosts       []string `json:"hosts"`
	Ports       []string `json:"ports"`
	Name        string   `json:"name"`
	AccessMode  string   `json:"access_mode"`
	VolumeType  string   `json:"volume_type"`
}

// NewRBDDriver initialize RBD driver
func NewRBDDriver() (*RBDDriver, error) {
	return &RBDDriver{}, nil
}

// ToRBDVolume converts volumeData to RBDVolume
func (d *RBDDriver) ToRBDVolume(volumeData map[string]interface{}) (*RBDVolume, error) {
	data, err := json.Marshal(volumeData)
	if err != nil {
		return nil, err
	}

	var volume RBDVolume
	err = json.Unmarshal(data, &volume)
	if err != nil {
		return nil, err
	}

	return &volume, nil
}

func (d *RBDDriver) unmapRBD(rbdPath, mappedDevice string) error {
	_, err := exec.Command(rbdPath, "unmap", mappedDevice).CombinedOutput()
	if err != nil {
		return err
	}

	return nil
}

// Format formats rbd volume into specified file format
func (d *RBDDriver) Format(volumeData map[string]interface{}, fsType string) error {
	volume, err := d.ToRBDVolume(volumeData)
	if err != nil {
		return err
	}

	klog.V(4).Infof("Format cinder rbd %v to %s", volume, fsType)

	rbdPath, err := exec.LookPath("rbd")
	if err != nil {
		return fmt.Errorf("rbd command not found")
	}

	filePath, err := exec.LookPath("file")
	if err != nil {
		return fmt.Errorf("file command not found")
	}

	mappedDeviceByte, err := exec.Command(rbdPath, "map", volume.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("rbd map failed: %v", err)
	}

	mappedDevice := strings.TrimSpace(string(mappedDeviceByte))
	defer d.unmapRBD(rbdPath, mappedDevice)

	deviceInfo, err := exec.Command(filePath, "-s", mappedDevice).CombinedOutput()
	if err != nil {
		return fmt.Errorf("rbd map failed: %v", err)
	}

	if !strings.Contains(string(deviceInfo), fmt.Sprintf("%s filesystem", fsType)) {
		mkfsPath, err := exec.LookPath(fmt.Sprintf("mkfs.%s", fsType))
		if err != nil {
			return fmt.Errorf("mkfs.%s not found", fsType)
		}

		_, err = exec.Command(mkfsPath, mappedDevice).CombinedOutput()
		if err != nil {
			return fmt.Errorf("rbd format failed: %v", err)
		}
	}

	return nil
}
