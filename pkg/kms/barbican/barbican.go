package barbican

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/keymanager/v1/secrets"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
)

type BarbicanService interface {
	GetSecret(keyID string) ([]byte, error)
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
	Client *gophercloud.ServiceClient
}

// NewBarbicanClient creates new BarbicanClient
func NewBarbicanClient(cfg Config) (*gophercloud.ServiceClient, error) {
	provider, err := openstack_provider.NewOpenStackClient(&cfg.Global, "barbican-kms-plugin")
	if err != nil {
		return nil, err
	}

	return openstack.NewKeyManagerV1(provider, gophercloud.EndpointOpts{
		Region: cfg.Global.Region,
	})
}

// GetSecret gets unencrypted secret
func (barbican *Barbican) GetSecret(keyID string) ([]byte, error) {
	opts := secrets.GetPayloadOpts{
		PayloadContentType: "application/octet-stream",
	}

	key, err := secrets.GetPayload(barbican.Client, keyID, opts).Extract()
	if err != nil {
		return nil, err
	}

	return key, nil
}
