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

package mount

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/mount"
	utilexec "k8s.io/utils/exec"
)

const (
	probeVolumeDuration      = 1 * time.Second
	probeVolumeTimeout       = 60 * time.Second
	operationFinishInitDelay = 1 * time.Second
	operationFinishFactor    = 1.1
	operationFinishSteps     = 15
	instanceIDFile           = "/var/lib/cloud/data/instance-id"
)

type IMount interface {
	GetBaseMounter() *mount.SafeFormatAndMount
	ScanForAttach(devicePath string) error
	GetDevicePath(volumeID string) (string, error)
	IsLikelyNotMountPointAttach(targetpath string) (bool, error)
	FormatAndMount(source string, target string, fstype string, options []string) error
	IsLikelyNotMountPointDetach(targetpath string) (bool, error)
	Mount(source string, target string, fstype string, options []string) error
	UnmountPath(mountPath string) error
	GetInstanceID() (string, error)
}

type Mount struct {
}

var MInstance IMount = nil

func GetMountProvider() (IMount, error) {

	if MInstance == nil {
		MInstance = &Mount{}
	}
	return MInstance, nil
}

// GetBaseMounter returns instance of SafeFormatAndMount
func (m *Mount) GetBaseMounter() *mount.SafeFormatAndMount {
	nMounter := mount.New("")
	nExec := mount.NewOsExec()
	return &mount.SafeFormatAndMount{
		Interface: nMounter,
		Exec:      nExec,
	}

}

// probeVolume probes volume in compute
func probeVolume() error {
	// rescan scsi bus
	scsi_path := "/sys/class/scsi_host/"
	if dirs, err := ioutil.ReadDir(scsi_path); err == nil {
		for _, f := range dirs {
			name := scsi_path + f.Name() + "/scan"
			data := []byte("- - -")
			ioutil.WriteFile(name, data, 0666)
		}
	}

	executor := utilexec.New()
	args := []string{"trigger"}
	cmd := executor.Command("udevadm", args...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		klog.V(3).Infof("error running udevadm trigger %v\n", err)
		return err
	}
	return nil
}

// GetDevicePath returns the path of an attached block storage volume, specified by its id.
func (m *Mount) GetDevicePath(volumeID string) (string, error) {
	backoff := wait.Backoff{
		Duration: operationFinishInitDelay,
		Factor:   operationFinishFactor,
		Steps:    operationFinishSteps,
	}

	var devicePath string
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		devicePath = m.getDevicePathBySerialID(volumeID)
		if devicePath != "" {
			return true, nil
		}
		return false, nil
	})

	if err == wait.ErrWaitTimeout {
		return "", fmt.Errorf("Failed to find device for the volumeID: %q within the alloted time", volumeID)
	} else if devicePath == "" {
		return "", fmt.Errorf("Device path was empty for volumeID: %q", volumeID)
	}
	return devicePath, nil
}

// GetDevicePathBySerialID returns the path of an attached block storage volume, specified by its id.
func (m *Mount) getDevicePathBySerialID(volumeID string) string {
	// Build a list of candidate device paths.
	// Certain Nova drivers will set the disk serial ID, including the Cinder volume id.
	candidateDeviceNodes := []string{
		// KVM
		fmt.Sprintf("virtio-%s", volumeID[:20]),
		// KVM virtio-scsi
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", volumeID[:20]),
		// ESXi
		fmt.Sprintf("wwn-0x%s", strings.Replace(volumeID, "-", "", -1)),
	}

	files, err := ioutil.ReadDir("/dev/disk/by-id/")
	if err != nil {
		klog.V(4).Infof("ReadDir failed with error %v", err)
	}

	for _, f := range files {
		for _, c := range candidateDeviceNodes {
			if c == f.Name() {
				klog.V(4).Infof("Found disk attached as %q; full devicepath: %s\n",
					f.Name(), path.Join("/dev/disk/by-id/", f.Name()))
				return path.Join("/dev/disk/by-id/", f.Name())
			}
		}
	}

	klog.V(4).Infof("Failed to find device for the volumeID: %q by serial ID", volumeID)
	return ""
}

// ScanForAttach
func (m *Mount) ScanForAttach(devicePath string) error {
	ticker := time.NewTicker(probeVolumeDuration)
	defer ticker.Stop()
	timer := time.NewTimer(probeVolumeTimeout)
	defer timer.Stop()

	for {
		select {
		case <-ticker.C:
			klog.V(5).Infof("Checking Cinder disk %q is attached.", devicePath)
			probeVolume()

			exists, err := mount.PathExists(devicePath)
			if exists && err == nil {
				return nil
			} else {
				klog.V(3).Infof("Could not find attached Cinder disk %s", devicePath)
			}
		case <-timer.C:
			return fmt.Errorf("Could not find attached Cinder disk %s. Timeout waiting for mount paths to be created.", devicePath)
		}
	}
}

// FormatAndMount
func (m *Mount) FormatAndMount(source string, target string, fstype string, options []string) error {
	diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: mount.NewOsExec()}
	return diskMounter.FormatAndMount(source, target, fstype, options)
}

func (m *Mount) Mount(source string, target string, fstype string, options []string) error {
	diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: mount.NewOsExec()}
	return diskMounter.Mount(source, target, fstype, options)
}

// IsLikelyNotMountPointAttach
func (m *Mount) IsLikelyNotMountPointAttach(targetpath string) (bool, error) {
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetpath)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(targetpath, 0750)
			if err == nil {
				notMnt = true
			}
		}
	}
	return notMnt, err
}

// IsLikelyNotMountPointDetach
func (m *Mount) IsLikelyNotMountPointDetach(targetpath string) (bool, error) {
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetpath)
	if err != nil {
		if os.IsNotExist(err) {
			return notMnt, fmt.Errorf("targetpath not found")
		} else {
			return notMnt, err
		}
	}
	return notMnt, nil
}

// UnmountPath
func (m *Mount) UnmountPath(mountPath string) error {
	return mount.CleanupMountPoint(mountPath, mount.New(""), false /* extensiveMountPointCheck */)
}

// GetInstanceID from file
func (m *Mount) GetInstanceID() (string, error) {
	// Try to find instance ID on the local filesystem (created by cloud-init)
	idBytes, err := ioutil.ReadFile(instanceIDFile)
	if err == nil {
		instanceID := string(idBytes)
		instanceID = strings.TrimSpace(instanceID)
		klog.V(3).Infof("Got instance id from %s: %s", instanceIDFile, instanceID)
		if instanceID != "" {
			return instanceID, nil
		}
	}
	return "", err
}

func IsCorruptedMnt(err error) bool {
	return mount.IsCorruptedMnt(err)
}
