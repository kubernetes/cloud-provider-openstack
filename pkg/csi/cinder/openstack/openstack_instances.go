package openstack

import (
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
)

// GetInstanceByID returns server with specified instanceID
func (os *OpenStack) GetInstanceByID(instanceID string) (*servers.Server, error) {
	server, err := servers.Get(os.compute, instanceID).Extract()
	if err != nil {
		return nil, err
	}
	return server, nil
}
