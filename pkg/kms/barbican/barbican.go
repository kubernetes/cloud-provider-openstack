package barbican

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/keymanager/v1/secrets"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
	"k8s.io/klog"
)

type BarbicanService interface {
	GetSecret(cfg Config) ([]byte, error)
}

type KMSOpts struct {
	KeyID string `gcfg:"key-id"`
}

//Config to read config options
type Config struct {
	Global     openstack_provider.AuthOpts
	KeyManager KMSOpts
}

// Barbican is gophercloud service client
type Barbican struct {
}

// NewBarbicanClient creates new BarbicanClient
func newBarbicanClient(cfg Config) (client *gophercloud.ServiceClient, err error) {
	provider, err := openstack_provider.NewOpenStackClient(&cfg.Global, "barbican-kms-plugin")
	if err != nil {
		return nil, err
	}

	client, err = openstack.NewKeyManagerV1(provider, gophercloud.EndpointOpts{
		Region: cfg.Global.Region,
	})
	if err != nil {
		return nil, err
	}

	return client, nil
}

// GetSecret gets unencrypted secret
func (barbican *Barbican) GetSecret(cfg Config) ([]byte, error) {
	client, err := newBarbicanClient(cfg)

	keyID := cfg.KeyManager.KeyID

	if err != nil {
		klog.V(4).Infof("Failed to get Barbican client %v: ", err)
		return nil, err
	}

	opts := secrets.GetPayloadOpts{
		PayloadContentType: "application/octet-stream",
	}

	key, err := secrets.GetPayload(client, keyID, opts).Extract()
	if err != nil {
		return nil, err
	}

	return key, nil
}
