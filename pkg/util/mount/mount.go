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

	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/exec"
	"k8s.io/utils/mount"

	"k8s.io/cloud-provider-openstack/pkg/util/blockdevice"
)

const (
	probeVolumeDuration      = 1 * time.Second
	probeVolumeTimeout       = 60 * time.Second
	operationFinishInitDelay = 1 * time.Second
	operationFinishFactor    = 1.1
	operationFinishSteps     = 15
)

type IMount interface {
	Mounter() *mount.SafeFormatAndMount
	ScanForAttach(devicePath string) error
	GetDevicePath(volumeID string) (string, error)
	IsLikelyNotMountPointAttach(targetpath string) (bool, error)
	UnmountPath(mountPath string) error
	MakeFile(pathname string) error
	MakeDir(pathname string) error
	GetDeviceStats(path string) (*DeviceStats, error)
}

type DeviceStats struct {
	Block bool

	AvailableBytes  int64
	TotalBytes      int64
	UsedBytes       int64
	AvailableInodes int64
	TotalInodes     int64
	UsedInodes      int64
}

type Mount struct {
	BaseMounter *mount.SafeFormatAndMount
}

var _ IMount = &Mount{}

var MInstance IMount = nil

func getBaseMounter() *mount.SafeFormatAndMount {
	nMounter := mount.New("")
	nExec := exec.New()
	return &mount.SafeFormatAndMount{
		Interface: nMounter,
		Exec:      nExec,
	}
}

//GetMountProvider returns instance of Mounter
func GetMountProvider() IMount {
	if MInstance == nil {
		MInstance = &Mount{BaseMounter: getBaseMounter()}
	}
	return MInstance
}

// Mounter returns instance of SafeFormatAndMount
func (m *Mount) Mounter() *mount.SafeFormatAndMount {
	return m.BaseMounter
}

// probeVolume probes volume in compute
func probeVolume() error {
	// rescan scsi bus
	scsiPath := "/sys/class/scsi_host/"
	if dirs, err := ioutil.ReadDir(scsiPath); err == nil {
		for _, f := range dirs {
			name := scsiPath + f.Name() + "/scan"
			data := []byte("- - -")
			ioutil.WriteFile(name, data, 0666)
		}
	}

	executor := exec.New()
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
		// see issue https://github.com/kubernetes/cloud-provider-openstack/issues/705
		probeVolume()
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
		// KVM #852
		fmt.Sprintf("virtio-%s", volumeID),
		// KVM virtio-scsi
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", volumeID[:20]),
		// KVM virtio-scsi #852
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", volumeID),
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
			}
			klog.V(3).Infof("Could not find attached Cinder disk %s", devicePath)
		case <-timer.C:
			return fmt.Errorf("Could not find attached Cinder disk %s. Timeout waiting for mount paths to be created.", devicePath)
		}
	}
}

// IsLikelyNotMountPointAttach
func (m *Mount) IsLikelyNotMountPointAttach(targetpath string) (bool, error) {
	notMnt, err := m.BaseMounter.IsLikelyNotMountPoint(targetpath)
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

// UnmountPath
func (m *Mount) UnmountPath(mountPath string) error {
	return mount.CleanupMountPoint(mountPath, m.BaseMounter, false /* extensiveMountPointCheck */)
}

// MakeDir creates dir
func (m *Mount) MakeDir(pathname string) error {
	err := os.MkdirAll(pathname, os.FileMode(0755))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// MakeFile creates an empty file
func (m *Mount) MakeFile(pathname string) error {
	f, err := os.OpenFile(pathname, os.O_CREATE, os.FileMode(0644))
	defer f.Close()
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func (m *Mount) GetDeviceStats(path string) (*DeviceStats, error) {
	isBlock, err := blockdevice.IsBlockDevice(path)

	if isBlock {
		size, err := blockdevice.GetBlockDeviceSize(path)
		if err != nil {
			return nil, err
		}

		return &DeviceStats{
			Block:      true,
			TotalBytes: size,
		}, nil
	}

	var statfs unix.Statfs_t
	// See http://man7.org/linux/man-pages/man2/statfs.2.html for details.
	err = unix.Statfs(path, &statfs)
	if err != nil {
		return nil, err
	}

	return &DeviceStats{
		Block: false,

		AvailableBytes: int64(statfs.Bavail) * int64(statfs.Bsize),
		TotalBytes:     int64(statfs.Blocks) * int64(statfs.Bsize),
		UsedBytes:      (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize),

		AvailableInodes: int64(statfs.Ffree),
		TotalInodes:     int64(statfs.Files),
		UsedInodes:      int64(statfs.Files) - int64(statfs.Ffree),
	}, nil
}
