package sanity

var FakeInstanceID = "321a8b81-3660-43e5-bab8-6470b65ee4e8"
var FakeAvailability = "fake-az"

type fakemetadata struct{}

// fake metadata

func (m *fakemetadata) GetInstanceID() (string, error) {
	return FakeInstanceID, nil
}

func (m *fakemetadata) GetAvailabilityZone() (string, error) {
	return FakeAvailability, nil
}
