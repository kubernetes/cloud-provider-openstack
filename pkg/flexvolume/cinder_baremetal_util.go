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

package flexvolume

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/volumeactions"

	"k8s.io/cloud-provider-openstack/pkg/flexvolume/cinder/drivers"
)

// CinderBaremetalUtil is a tool to operate Cinder volume without cloud provider
type CinderBaremetalUtil struct {
	client   *cinderClient
	hostname string
}

// AttachDiskBaremetal mounts the device and detaches the disk from the kubelet's host machine.
func (cb *CinderBaremetalUtil) AttachDiskBaremetal(b *FlexVolumeDriver, targetMountDir string) error {
	glog.V(4).Infof("Begin to attach volume %v", b.volId)
	volume, err := cb.client.getVolume(b.volId)
	if err != nil {
		glog.Errorf("Get volume %s error: %v", b.volId, err)
		return err
	}

	var attached bool
	if len(volume.Attachments) > 0 || volume.Status != "available" {
		for _, att := range volume.Attachments {
			if att.HostName == cb.hostname && att.Device == targetMountDir {
				glog.V(5).Infof("Volume %s is already attached", b.volId)
				attached = true
				break
			}
		}

		if !attached {
			return fmt.Errorf("Volume %s is not available", b.volId)
		}
	}

	connectionInfo, err := cb.client.getConnectionInfo(volume.ID, cb.getConnectionOptions())
	if err != nil {
		return err
	}

	volumeType := connectionInfo["driver_volume_type"].(string)
	data := connectionInfo["data"].(map[string]interface{})
	data["volume_type"] = volumeType
	if volumeType == "rbd" {
		data["keyring"] = cb.client.keyring
	}
	b.metadata = data

	// already attached, just return
	if attached {
		return nil
	}

	mountMode := volumeactions.ReadWrite
	if b.readOnly {
		mountMode = volumeactions.ReadOnly
	}

	// attach volume
	attachOpts := volumeactions.AttachOpts{
		MountPoint: targetMountDir,
		Mode:       mountMode,
		HostName:   cb.hostname,
	}

	err = cb.client.attach(volume.ID, attachOpts)
	if err != nil && err.Error() != "EOF" {
		return err
	}

	rbdDriver, err := drivers.NewRBDDriver()
	if err != nil {
		glog.Warningf("Get cinder driver RBD failed: %v", err)
		cb.client.detach(volume.ID)
		return err
	}

	err = rbdDriver.Format(data, b.fsType)
	if err != nil {
		glog.Warningf("Format cinder volume %s failed: %v", b.volId, err)
		cb.client.detach(volume.ID)
		return err
	}

	return nil
}

// DetachDiskBaremetal unmounts the device and detaches the disk from the kubelet's host machine.
func (cb *CinderBaremetalUtil) DetachDiskBaremetal(d *FlexVolumeDriver) error {
	volume, err := cb.client.getVolume(d.volId)
	if err != nil {
		return err
	}

	connectionInfo, err := cb.client.getConnectionInfo(volume.ID, cb.getConnectionOptions())
	if err != nil {
		return err
	}

	volumeType := connectionInfo["driver_volume_type"].(string)

	data := connectionInfo["data"].(map[string]interface{})
	if volumeType == "rbd" {
		data["keyring"] = cb.client.keyring
	}

	err = cb.client.terminateConnection(volume.ID, cb.getTerminationOptions())
	if err != nil {
		return err
	}

	if volume.Status == "available" {
		return nil
	}

	err = cb.client.detach(volume.ID)
	if err != nil {
		return err
	}

	return nil
}

// Get iscsi initiator
func (cb *CinderBaremetalUtil) getIscsiInitiator() string {
	contents, err := ioutil.ReadFile("/etc/iscsi/initiatorname.iscsi")
	if err != nil {
		return ""
	}

	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "InitiatorName=") {
			return strings.Split(line, "=")[1]
		}
	}

	return ""
}

// Get cinder connections options
func (cb *CinderBaremetalUtil) getConnectionOptions() *volumeactions.InitializeConnectionOpts {
	connector := volumeactions.InitializeConnectionOpts{
		Host:      cb.hostname,
		Initiator: cb.getIscsiInitiator(),
	}

	return &connector
}

// Get cinder termination options
func (cb *CinderBaremetalUtil) getTerminationOptions() *volumeactions.TerminateConnectionOpts {
	connector := volumeactions.TerminateConnectionOpts{
		Host:      cb.hostname,
		Initiator: cb.getIscsiInitiator(),
	}

	return &connector
}
