package sanity

import "k8s.io/cloud-provider-openstack/pkg/csi/cinder"

type fakemount struct {
}

// fake mount

func (m *fakemount) ScanForAttach(devicePath string) error {
	return nil
}

func (m *fakemount) IsLikelyNotMountPointAttach(targetpath string) (bool, error) {
	return true, nil
}

func (m *fakemount) FormatAndMount(source string, target string, fstype string, options []string) error {
	return nil
}

func (m *fakemount) IsLikelyNotMountPointDetach(targetpath string) (bool, error) {
	return false, nil
}

func (m *fakemount) Mount(source string, target string, fstype string, options []string) error {
	return nil

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
