package sanity

import "k8s.io/cloud-provider-openstack/pkg/csi/cinder"

type fakemetadata struct {
}

// fake metadata

func (m *fakemetadata) GetInstanceID() (string, error) {
	return cinder.FakeInstanceID, nil
}

func (m *fakemetadata) GetAvailabilityZone() (string, error) {
	return cinder.FakeAvailability, nil
}
