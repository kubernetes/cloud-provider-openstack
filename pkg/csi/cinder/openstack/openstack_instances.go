package openstack

import (
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	cpo "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
)

// GetInstanceByID returns server with specified instanceID
func (os *OpenStack) GetInstanceByID(instanceID string) (*servers.Server, error) {
	server, err := servers.Get(os.compute, instanceID).Extract()
	if err != nil {
		return nil, err
	}
	return server, nil
}

// GetInstanceByID returns server with specified instanceID
func (os *OpenStack) GetMetadataOpts() cpo.MetadataOpts {
	return os.metadataOpts
}
