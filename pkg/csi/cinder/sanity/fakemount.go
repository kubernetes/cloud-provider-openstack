package sanity

import (
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	mount2 "k8s.io/cloud-provider-openstack/pkg/util/mount"
	"k8s.io/utils/mount"
)

type fakemount struct {
}

// fake mount

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
	return nil
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

func (m *fakemount) GetDeviceStats(path string) (*mount2.DeviceStats, error) {
	return nil, nil
}
