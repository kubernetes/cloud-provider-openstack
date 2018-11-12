/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package volumeservice

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"reflect"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/noauth"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"gopkg.in/gcfg.v1"

	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"

	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog"
)

type cinderConfig struct {
	openstack_provider.Config
	Cinder struct {
		Endpoint string `gcfg:"endpoint"`
	}
}

func (cfg cinderConfig) toAuthOptions() gophercloud.AuthOptions {
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

func (cfg cinderConfig) toAuth3Options() tokens3.AuthOptions {
	return tokens3.AuthOptions{
		IdentityEndpoint: cfg.Global.AuthURL,
		Username:         cfg.Global.Username,
		UserID:           cfg.Global.UserID,
		Password:         cfg.Global.Password,
		DomainID:         cfg.Global.DomainID,
		DomainName:       cfg.Global.DomainName,
		AllowReauth:      true,
	}
}

func getConfigFromEnv() cinderConfig {
	var cfg cinderConfig

	cfg.Global.AuthURL = os.Getenv("OS_AUTH_URL")
	cfg.Global.Username = os.Getenv("OS_USERNAME")
	cfg.Global.Password = os.Getenv("OS_PASSWORD")
	cfg.Global.Region = os.Getenv("OS_REGION_NAME")
	cfg.Global.UserID = os.Getenv("OS_USER_ID")
	cfg.Global.TrustID = os.Getenv("OS_TRUST_ID")

	cfg.Global.TenantID = os.Getenv("OS_TENANT_ID")
	if cfg.Global.TenantID == "" {
		cfg.Global.TenantID = os.Getenv("OS_PROJECT_ID")
	}
	cfg.Global.TenantName = os.Getenv("OS_TENANT_NAME")
	if cfg.Global.TenantName == "" {
		cfg.Global.TenantName = os.Getenv("OS_PROJECT_NAME")
	}

	cfg.Global.DomainID = os.Getenv("OS_DOMAIN_ID")
	if cfg.Global.DomainID == "" {
		cfg.Global.DomainID = os.Getenv("OS_USER_DOMAIN_ID")
	}
	cfg.Global.DomainName = os.Getenv("OS_DOMAIN_NAME")
	if cfg.Global.DomainName == "" {
		cfg.Global.DomainName = os.Getenv("OS_USER_DOMAIN_NAME")
	}

	cfg.Cinder.Endpoint = os.Getenv("OS_CINDER_ENDPOINT")
	return cfg
}

func getConfig(configFilePath string) (cinderConfig, error) {
	config := getConfigFromEnv()
	if configFilePath != "" {
		var configFile *os.File
		configFile, err := os.Open(configFilePath)
		if err != nil {
			klog.Fatalf("Couldn't open configuration %s: %#v",
				configFilePath, err)
			return cinderConfig{}, err
		}

		defer configFile.Close()

		err = gcfg.FatalOnly(gcfg.ReadInto(&config, configFile))
		if err != nil {
			klog.Fatalf("Couldn't read configuration: %#v", err)
			return cinderConfig{}, err
		}
		return config, nil
	}
	if reflect.DeepEqual(config, cinderConfig{}) {
		klog.Fatal("Configuration missing: no config file specified and " +
			"environment variables are not set.")
	}
	return config, nil
}

func getKeystoneVolumeService(cfg cinderConfig) (*gophercloud.ServiceClient, error) {
	provider, err := openstack.NewClient(cfg.Global.AuthURL)
	if err != nil {
		return nil, err
	}
	if cfg.Global.CAFile != "" {
		var roots *x509.CertPool
		roots, err = certutil.NewPool(cfg.Global.CAFile)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{}
		config.RootCAs = roots
		provider.HTTPClient.Transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: config})

	}
	if cfg.Global.TrustID != "" {
		opts := cfg.toAuth3Options()
		authOptsExt := trusts.AuthOptsExt{
			TrustID:            cfg.Global.TrustID,
			AuthOptionsBuilder: &opts,
		}
		err = openstack.AuthenticateV3(provider, authOptsExt, gophercloud.EndpointOpts{})
	} else {
		err = openstack.Authenticate(provider, cfg.toAuthOptions())
	}

	if err != nil {
		return nil, err
	}

	volumeService, err := openstack.NewBlockStorageV2(provider,
		gophercloud.EndpointOpts{
			Region: cfg.Global.Region,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to get volume service: %v", err)
	}
	return volumeService, nil
}

func getNoAuthVolumeService(cfg cinderConfig) (*gophercloud.ServiceClient, error) {
	provider, err := noauth.NewClient(gophercloud.AuthOptions{
		Username:   cfg.Global.Username,
		TenantName: cfg.Global.TenantName,
	})
	if err != nil {
		return nil, err
	}

	client, err := noauth.NewBlockStorageNoAuth(provider, noauth.EndpointOpts{
		CinderEndpoint: cfg.Cinder.Endpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get volume service: %v", err)
	}

	return client, nil
}

// GetVolumeService returns a connected cinder client based on configuration
// specified in configFilePath or the environment.
func GetVolumeService(configFilePath string) (*gophercloud.ServiceClient, error) {
	config, err := getConfig(configFilePath)
	if err != nil {
		return nil, err
	}

	if config.Cinder.Endpoint != "" {
		return getNoAuthVolumeService(config)
	}
	return getKeystoneVolumeService(config)
}
