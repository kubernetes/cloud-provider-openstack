/*
Copyright 2019 The Kubernetes Authors.

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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
	"k8s.io/klog"
)

// findBlockDeviceRescanPath Find the underlaying disk for a linked path such as /dev/disk/by-path/XXXX or /dev/mapper/XXXX
// will return /sys/devices/pci0000:00/0000:00:15.0/0000:03:00.0/host0/target0:0:1/0:0:1:0/rescan
func findBlockDeviceRescanPath(path string) (string, error) {
	devicePath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	// if path /dev/hdX split into "", "dev", "hdX" then we will
	// return just the last part
	parts := strings.Split(devicePath, "/")
	if len(parts) == 3 && strings.HasPrefix(parts[1], "dev") {
		return filepath.EvalSymlinks(filepath.Join("/sys/block", parts[2], "device", "rescan"))
	}
	return "", fmt.Errorf("illegal path for device " + devicePath)
}

// getBlockDeviceSize returns the size of the block device by path
func getBlockDeviceSize(path string) (int64, error) {
	fd, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer fd.Close()

	var devSize uint64
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd.Fd()), unix.BLKGETSIZE64, uintptr(unsafe.Pointer(&devSize))); errno != 0 {
		return 0, fmt.Errorf("failed to detect the %q block device size: %v", path, errno)
	}

	return int64(devSize), nil
}

func checkBlockDeviceSize(devicePath string, deviceMountPath string, newSize int64) (error, bool) {
	klog.V(4).Infof("Detecting %q volume size", deviceMountPath)
	size, err := getBlockDeviceSize(devicePath)
	if err != nil {
		return err, false
	}

	klog.V(3).Infof("Detected %q volume size: %d", deviceMountPath, size)
	// Cmp returns 0 if the quantity is equal to y, -1 if the quantity is less than y,
	// or 1 if the quantity is greater than y.
	if size < newSize {
		return fmt.Errorf("current volume size is less than expected one: %d < %d", size, newSize), true
	}

	return nil, true
}

func PollBlockGeometry(devicePath string, deviceMountPath string, newSize int64) error {
	if newSize == 0 {
		klog.Error("newSize is empty, skipping the polling")
		return nil
	}

	// when block device size corresponds expectations, return nil
	bdSizeErr, ok := checkBlockDeviceSize(devicePath, deviceMountPath, newSize)
	if bdSizeErr == nil {
		return nil
	} else if !ok {
		// no access to the blockdevice, or other fatal error
		return bdSizeErr
	}

	// don't fail if resolving doesn't work
	blockDeviceRescanPath, err := findBlockDeviceRescanPath(devicePath)
	if err != nil {
		klog.Errorf("Error resolving block device path from %q: %v", devicePath, err)
		// no need to run checkBlockDeviceSize second time here, return the saved error
		return bdSizeErr
	}

	klog.V(3).Infof("Resolved block device path from %q to %q", devicePath, blockDeviceRescanPath)
	klog.V(4).Infof("Polling %q block device geometry", devicePath)
	err = ioutil.WriteFile(blockDeviceRescanPath, []byte{'1'}, 0666)
	if err != nil {
		klog.Errorf("Error polling new block device geometry: %v", err)
		// no need to run checkBlockDeviceSize second time here, return the saved error
		return bdSizeErr
	}

	err, _ = checkBlockDeviceSize(devicePath, deviceMountPath, newSize)
	return err
}
