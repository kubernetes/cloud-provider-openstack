package sanity

import (
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	cpomount "k8s.io/cloud-provider-openstack/pkg/util/mount"
	exec "k8s.io/utils/exec/testing"
	"k8s.io/utils/mount"
)

type fakemount struct {
	BaseMounter *mount.SafeFormatAndMount
}

var _ cpomount.IMount = &fakemount{}
var (
	fakeMounter = &mount.FakeMounter{MountPoints: []mount.MountPoint{}}
	fakeExec    = &exec.FakeExec{DisableScripts: true}
	mounter     = &cpomount.Mount{BaseMounter: newFakeSafeFormatAndMounter()}
)

//GetFakeMountProvider returns fake instance of Mounter
func GetFakeMountProvider() cpomount.IMount {
	return &fakemount{BaseMounter: newFakeSafeFormatAndMounter()}
}

// NewFakeSafeFormatAndMounter returns base Fake mounter instance
func newFakeSafeFormatAndMounter() *mount.SafeFormatAndMount {
	return &mount.SafeFormatAndMount{
		Interface: fakeMounter,
		Exec:      fakeExec,
	}
}

func (m *fakemount) Mounter() *mount.SafeFormatAndMount {
	return m.BaseMounter
}

func (m *fakemount) ScanForAttach(devicePath string) error {
	return nil
}

func (m *fakemount) IsLikelyNotMountPointAttach(targetpath string) (bool, error) {
	return mounter.IsLikelyNotMountPointAttach(targetpath)
}

func (m *fakemount) UnmountPath(mountPath string) error {
	return mounter.UnmountPath(mountPath)
}

func (m *fakemount) GetInstanceID() (string, error) {
	return cinder.FakeInstanceID, nil
}

func (m *fakemount) GetDevicePath(volumeID string) (string, error) {
	return cinder.FakeDevicePath, nil
}

func (m *fakemount) MakeDir(pathname string) error {
	return nil
}

// MakeFile creates an empty file
func (m *fakemount) MakeFile(pathname string) error {
	return nil
}

func (m *fakemount) GetDeviceStats(path string) (*cpomount.DeviceStats, error) {
	return cinder.FakeFsStats, nil
}
