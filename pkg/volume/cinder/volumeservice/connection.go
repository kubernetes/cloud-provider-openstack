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
	"fmt"
	"os"
	"reflect"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/noauth"
	"github.com/spf13/pflag"
	"gopkg.in/gcfg.v1"

	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
	"k8s.io/cloud-provider-openstack/pkg/version"

	"k8s.io/klog"
)

type cinderConfig struct {
	openstack_provider.Config
	Cinder struct {
		Endpoint string `gcfg:"endpoint"`
	}
}

// userAgentData is used to add extra information to the gophercloud user-agent
var userAgentData []string

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

func getConfigFromEnv() cinderConfig {
	cfg := cinderConfig{Config: openstack_provider.ConfigFromEnv()}

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
	provider, err := openstack_provider.NewOpenStackClient(&cfg.Config.Global, "cinder-provisioner", userAgentData...)
	if err != nil {
		return nil, err
	}

	volumeService, err := openstack.NewBlockStorageV2(provider, gophercloud.EndpointOpts{
		Region:       cfg.Global.Region,
		Availability: cfg.Global.EndpointType,
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

	userAgent := gophercloud.UserAgent{}
	userAgent.Prepend(fmt.Sprintf("cinder-provisioner/%s", version.Version))
	for _, data := range userAgentData {
		userAgent.Prepend(data)
	}
	provider.UserAgent = userAgent

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
