package openstack

import (
	"errors"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"k8s.io/klog"
)

// GetInstanceByID returns server with specified instanceID
func (os *OpenStack) GetInstanceByID(instanceID string) (*servers.Server, error) {
	var server *servers.Server = nil

	for _, region := range regions {
		var err error
		var compute *gophercloud.ServiceClient

		compute, err = openstack.NewComputeV2(os.provider, gophercloud.EndpointOpts{
			Region: region,
		})

		klog.V(5).Infof("Trying To Find Instance %s in Region %s", instanceID, region)
		server, err = servers.Get(compute, instanceID).Extract()
		if err != nil {
			continue
		}

		server.Metadata["region"] = region
		klog.V(5).Infof("Instance %s Found in Region %s", instanceID, region)
		break
	}

	if server == nil {
		klog.V(5).Infof("Instance %s Not Found", instanceID)
		return nil, errors.New("failed to find server")
	}

	return server, nil
}
