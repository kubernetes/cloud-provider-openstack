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

package openstack

import (
	"os"

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"gopkg.in/gcfg.v1"
)

type IOpenStack interface {
	CreateVolume(name string, size int, vtype, availability string, tags *map[string]string) (string, string, error)
	DeleteVolume(volumeID string) error
	AttachVolume(instanceID, volumeID string) (string, error)
	WaitDiskAttached(instanceID string, volumeID string) error
	DetachVolume(instanceID, volumeID string) error
	WaitDiskDetached(instanceID string, volumeID string) error
	GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
}

type OpenStack struct {
	compute      *gophercloud.ServiceClient
	blockstorage *gophercloud.ServiceClient
}

type Config struct {
	Global struct {
		AuthUrl    string `gcfg:"auth-url"`
		Username   string
		UserId     string `gcfg:"user-id"`
		Password   string
		TenantId   string `gcfg:"tenant-id"`
		TenantName string `gcfg:"tenant-name"`
		DomainId   string `gcfg:"domain-id"`
		DomainName string `gcfg:"domain-name"`
		Region     string
	}
}

func (cfg Config) toAuthOptions() gophercloud.AuthOptions {
	return gophercloud.AuthOptions{
		IdentityEndpoint: cfg.Global.AuthUrl,
		Username:         cfg.Global.Username,
		UserID:           cfg.Global.UserId,
		Password:         cfg.Global.Password,
		TenantID:         cfg.Global.TenantId,
		TenantName:       cfg.Global.TenantName,
		DomainID:         cfg.Global.DomainId,
		DomainName:       cfg.Global.DomainName,

		// Persistent service, so we need to be able to renew tokens.
		AllowReauth: true,
	}
}

func getConfigFromFile(configFilePath string) (gophercloud.AuthOptions, string, error) {
	// Get config from file
	var opts gophercloud.AuthOptions
	config, err := os.Open(configFilePath)
	if err != nil {
		glog.V(3).Infof("Failed to open OpenStack configuration file: %v", err)
		return opts, "", err
	}
	defer config.Close()

	// Read configuration
	var cfg Config
	err = gcfg.ReadInto(&cfg, config)
	if err != nil {
		glog.V(3).Infof("Failed to read OpenStack configuration file: %v", err)
		return opts, "", err
	}

	opts = cfg.toAuthOptions()
	region := cfg.Global.Region

	return opts, region, nil
}

func getConfigFromEnv() (gophercloud.AuthOptions, string, error) {
	// Get config from env
	opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		glog.V(3).Infof("Failed to read OpenStack configuration from env: %v", err)
		return opts, "", err
	}

	// Get Region from env
	region := os.Getenv("OS_REGION_NAME")

	return opts, region, nil
}

var OsInstance IOpenStack = nil
var configFile string = "/etc/openstack.conf"

func InitOpenStackProvider(cfg string) {
	configFile = cfg
	glog.V(2).Infof("InitOpenStackProvider configFile: %s", configFile)
}

func GetOpenStackProvider() (IOpenStack, error) {

	if OsInstance == nil {
		// Get config from file
		opts, region, err := getConfigFromFile(configFile)
		if err != nil {
			// Get config from env
			opts, region, err = getConfigFromEnv()
			if err != nil {
				return nil, err
			}
		}

		// Authenticate Client
		provider, err := openstack.AuthenticatedClient(opts)
		if err != nil {
			return nil, err
		}

		// Init Nova ServiceClient
		computeclient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
			Region: region,
		})
		if err != nil {
			return nil, err
		}

		// Init Cinder ServiceClient
		blockstorageclient, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
			Region: region,
		})
		if err != nil {
			return nil, err
		}

		// Init OpenStack
		OsInstance = &OpenStack{
			compute:      computeclient,
			blockstorage: blockstorageclient,
		}
	}

	return OsInstance, nil
}
