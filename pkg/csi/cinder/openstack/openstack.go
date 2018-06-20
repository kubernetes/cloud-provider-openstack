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
	"errors"
	"fmt"
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
	GetVolumesByName(name string) ([]Volume, error)
	CreateSnapshot(name, volID, description string, tags *map[string]string) (*Snapshot, error)
	ListSnapshots(limit, offset int, filters map[string]string) ([]Snapshot, error)
	DeleteSnapshot(snapID string) error
}

type OpenStack struct {
	compute *gophercloud.ServiceClient
	volumes volumeService
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
	BlockStorage BlockStorageOpts
}

// BlockStorageOpts is used to talk to Cinder service
type BlockStorageOpts struct {
	BSVersion string `gcfg:"bs-version"`
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

func GetConfigFromFile(configFilePath string) (gophercloud.AuthOptions, gophercloud.EndpointOpts, Config, error) {
	// Get config from file
	var authOpts gophercloud.AuthOptions
	var epOpts gophercloud.EndpointOpts
	config, err := os.Open(configFilePath)
	if err != nil {
		glog.V(3).Infof("Failed to open OpenStack configuration file: %v", err)
		return authOpts, epOpts, Config{}, err
	}
	defer config.Close()

	// Read configuration
	var cfg Config
	err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if err != nil {
		glog.V(3).Infof("Failed to read OpenStack configuration file: %v", err)
		return authOpts, epOpts, Config{}, err
	}

	authOpts = cfg.toAuthOptions()
	epOpts = gophercloud.EndpointOpts{
		Region: cfg.Global.Region,
	}

	return authOpts, epOpts, cfg, nil
}

func GetConfigFromEnv() (gophercloud.AuthOptions, gophercloud.EndpointOpts, Config, error) {
	// Get config from env
	authOpts, err := openstack.AuthOptionsFromEnv()
	var epOpts gophercloud.EndpointOpts
	cfg := Config{}
	if err != nil {
		glog.V(3).Infof("Failed to read OpenStack configuration from env: %v", err)
		return authOpts, epOpts, cfg, err
	}

	epOpts = gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	}

	return authOpts, epOpts, cfg, nil
}

func GetBlockStorageClient(
	provider *gophercloud.ProviderClient,
	epOpts gophercloud.EndpointOpts,
	bsOpts BlockStorageOpts) (volumeService, error) {

	// Default value is auto
	bsVersion := bsOpts.BSVersion
	if bsVersion == "" {
		bsVersion = "auto"
	}

	switch bsVersion {
	case "v1":
		sClient, err := openstack.NewBlockStorageV1(provider, epOpts)
		if err != nil {
			return nil, err
		}
		glog.V(3).Info("Using Blockstorage API V1")
		return &VolumesV1{sClient, bsOpts}, nil
	case "v2":
		sClient, err := openstack.NewBlockStorageV2(provider, epOpts)
		if err != nil {
			return nil, err
		}
		glog.V(3).Info("Using Blockstorage API V2")
		return &VolumesV2{sClient, bsOpts}, nil
	case "v3":
		sClient, err := openstack.NewBlockStorageV3(provider, epOpts)
		if err != nil {
			return nil, err
		}
		glog.V(3).Info("Using Blockstorage API V3")
		return &VolumesV3{sClient, bsOpts}, nil
	case "auto":
		// Currently kubernetes support Cinder v1 / Cinder v2 / Cinder v3.
		// Choose Cinder v3 firstly, if kubernetes can't initialize cinder v3 client, try to initialize cinder v2 client.
		// If kubernetes can't initialize cinder v2 client, try to initialize cinder v1 client.
		// Return appropriate message when kubernetes can't initialize them.
		if sClient, err := openstack.NewBlockStorageV3(provider, epOpts); err == nil {
			glog.V(3).Info("Using Blockstorage API V3")
			return &VolumesV3{sClient, bsOpts}, nil
		}

		if sClient, err := openstack.NewBlockStorageV2(provider, epOpts); err == nil {
			glog.V(3).Info("Using Blockstorage API V2")
			return &VolumesV2{sClient, bsOpts}, nil
		}

		if sClient, err := openstack.NewBlockStorageV1(provider, epOpts); err == nil {
			glog.V(3).Info("Using Blockstorage API V1")
			return &VolumesV1{sClient, bsOpts}, nil
		}

		errTxt := "BlockStorage API version autodetection failed. " +
			"Please set it explicitly in cloud.conf in section [BlockStorage] with key `bs-version`"
		return nil, errors.New(errTxt)
	default:
		errTxt := fmt.Sprintf("Config error: unrecognised bs-version \"%v\"", bsOpts.BSVersion)
		return nil, errors.New(errTxt)
	}
}

var OsInstance IOpenStack = nil
var configFile string = "/etc/cloud.conf"

func InitOpenStackProvider(cfg string) {
	configFile = cfg
	glog.V(2).Infof("InitOpenStackProvider configFile: %s", configFile)
}

func GetOpenStackProvider() (IOpenStack, error) {

	if OsInstance == nil {
		// Get config from file
		authOpts, epOpts, cfg, err := GetConfigFromFile(configFile)
		if err != nil {
			// Get config from env
			authOpts, epOpts, cfg, err = GetConfigFromEnv()
			if err != nil {
				return nil, err
			}
		}

		// Authenticate Client
		provider, err := openstack.AuthenticatedClient(authOpts)
		if err != nil {
			return nil, err
		}

		// Init Nova ServiceClient
		computeclient, err := openstack.NewComputeV2(provider, epOpts)
		if err != nil {
			return nil, err
		}

		// Init Cinder ServiceClient
		volumes, err := GetBlockStorageClient(provider, epOpts, cfg.BlockStorage)
		if err != nil {
			return nil, err
		}

		// Init OpenStack
		OsInstance = &OpenStack{
			compute: computeclient,
			volumes: volumes,
		}
	}

	return OsInstance, nil
}
