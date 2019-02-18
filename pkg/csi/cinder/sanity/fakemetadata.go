package sanity

import "k8s.io/cloud-provider-openstack/pkg/csi/cinder"
import "k8s.io/cloud-provider-openstack/pkg/util/metadata"

type fakemetadata struct {
}

var FakeMetadata = metadata.Metadata{
	UUID:             "uuid",
	Name:             "fake-metadata",
	AvailabilityZone: cinder.FakeAvailability,
}

// fake metadata

func (m *fakemetadata) GetInstanceID(order string) (string, error) {
	return cinder.FakeInstanceID, nil
}

func (m *fakemetadata) GetAvailabilityZone(order string) (string, error) {
	return cinder.FakeAvailability, nil
}

func (m *fakemetadata) Get(order string) (*metadata.Metadata, error) {
	return &FakeMetadata, nil
}

func (m *fakemetadata) GetFromMetadataService(metadataVersion string) (*metadata.Metadata, error) {
	return &FakeMetadata, nil
}

func (m *fakemetadata) GetFromConfigDrive(metadataVersion string) (*metadata.Metadata, error) {
	return &FakeMetadata, nil
}
