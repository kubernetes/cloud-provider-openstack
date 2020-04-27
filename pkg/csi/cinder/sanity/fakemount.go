package sanity

import (
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	cpomount "k8s.io/cloud-provider-openstack/pkg/util/mount"
	exec "k8s.io/utils/exec/testing"
	"k8s.io/utils/mount"
)

type fakemount struct {
}

var mInstance *mount.FakeMounter

// NewFakeMounter returns fake mounter instance
func newFakeMounter() *mount.FakeMounter {
	if mInstance == nil {
		mInstance = &mount.FakeMounter{
			MountPoints: []mount.MountPoint{},
		}
	}
	return mInstance
}

// NewFakeSafeFormatAndMounter returns base Fake mounter instance
func newFakeSafeFormatAndMounter() *mount.SafeFormatAndMount {
	return &mount.SafeFormatAndMount{
		Interface: newFakeMounter(),
		Exec:      &exec.FakeExec{DisableScripts: true},
	}
}

func (m *fakemount) ScanForAttach(devicePath string) error {
	return nil
}

func (m *fakemount) IsLikelyNotMountPointAttach(targetpath string) (bool, error) {
	return true, nil
}

func (m *fakemount) IsLikelyNotMountPointDetach(targetpath string) (bool, error) {
	return false, nil
}

func (m *fakemount) UnmountPath(mountPath string) error {
	return nil
}

func (m *fakemount) GetInstanceID() (string, error) {
	return cinder.FakeInstanceID, nil
}

func (m *fakemount) GetDevicePath(volumeID string) (string, error) {
	return cinder.FakeDevicePath, nil
}

func (m *fakemount) GetBaseMounter() *mount.SafeFormatAndMount {
	return newFakeSafeFormatAndMounter()
}

func (m *fakemount) MakeDir(pathname string) error {
	return nil
}

// MakeFile creates an empty file
func (m *fakemount) MakeFile(pathname string) error {
	return nil
}

func (m *fakemount) PathExists(path string) (bool, error) {
	return false, nil
}

func (m *fakemount) GetDeviceStats(path string) (*cpomount.DeviceStats, error) {
	return cinder.FakeFsStats, nil
}
