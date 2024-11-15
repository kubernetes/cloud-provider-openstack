package barbican

import (
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/keymanager/v1/secrets"
	"k8s.io/cloud-provider-openstack/pkg/client"
)

type KMSOpts struct {
	KeyID string `gcfg:"key-id"`
}

// Config to read config options
type Config struct {
	Global     client.AuthOpts
	KeyManager KMSOpts
}

// Barbican is gophercloud service client
type Barbican struct {
	Client *gophercloud.ServiceClient
}

// NewBarbicanClient creates new BarbicanClient
func NewBarbicanClient(cfg Config) (*gophercloud.ServiceClient, error) {
	provider, err := client.NewOpenStackClient(&cfg.Global, "barbican-kms-plugin")
	if err != nil {
		return nil, err
	}

	return openstack.NewKeyManagerV1(provider, gophercloud.EndpointOpts{
		Region:       cfg.Global.Region,
		Availability: cfg.Global.EndpointType,
	})
}

// GetSecret gets unencrypted secret
func (barbican *Barbican) GetSecret(ctx context.Context, keyID string) ([]byte, error) {
	opts := secrets.GetPayloadOpts{
		PayloadContentType: "application/octet-stream",
	}

	key, err := secrets.GetPayload(ctx, barbican.Client, keyID, opts).Extract()
	if err != nil {
		return nil, err
	}

	return key, nil
}
