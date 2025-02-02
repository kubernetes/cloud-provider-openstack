package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
)

// GetInstanceByID returns server with specified instanceID
func (os *OpenStack) GetInstanceByID(ctx context.Context, instanceID string) (*servers.Server, error) {
	mc := metrics.NewMetricContext("server", "get")
	server, err := servers.Get(ctx, os.compute, instanceID).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	return server, nil
}
