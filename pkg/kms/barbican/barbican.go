package barbican

import (
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/keymanager/v1/secrets"
	"k8s.io/cloud-provider-openstack/pkg/version"
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
	Global struct {
		AuthURL    string `gcfg:"auth-url"`
		Username   string
		UserID     string `gcfg:"user-id"`
		Password   string
		TenantID   string `gcfg:"tenant-id"`
		TenantName string `gcfg:"tenant-name"`
		DomainID   string `gcfg:"domain-id"`
		DomainName string `gcfg:"domain-name"`
		Region     string
	}
	KeyManager KMSOpts
}

// Barbican is gophercloud service client
type Barbican struct {
}

func (cfg Config) toAuthOptions() gophercloud.AuthOptions {
	return gophercloud.AuthOptions{
		IdentityEndpoint: cfg.Global.AuthURL,
		Username:         cfg.Global.Username,
		UserID:           cfg.Global.UserID,
		Password:         cfg.Global.Password,
		TenantID:         cfg.Global.TenantID,
		TenantName:       cfg.Global.TenantName,
		DomainID:         cfg.Global.DomainID,
		DomainName:       cfg.Global.DomainName,

		// Persistent service, so we need to be able to renew tokens.
		AllowReauth: true,
	}
}

// NewBarbicanClient creates new BarbicanClient
func newBarbicanClient(cfg Config) (client *gophercloud.ServiceClient, err error) {

	provider, err := openstack.AuthenticatedClient(cfg.toAuthOptions())

	if err != nil {
		return nil, err
	}

	userAgent := gophercloud.UserAgent{}
	userAgent.Prepend(fmt.Sprintf("barbican-kms-plugin/%s", version.Version))
	provider.UserAgent = userAgent

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
