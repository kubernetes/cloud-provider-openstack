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
	"fmt"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/spf13/pflag"
	gcfg "gopkg.in/gcfg.v1"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
	md "k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/klog/v2"
)

// userAgentData is used to add extra information to the gophercloud user-agent
var userAgentData []string

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

type IOpenStack interface {
	CheckBlockStorageAPI() error
	CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourcevolID string, tags *map[string]string) (*volumes.Volume, error)
	DeleteVolume(volumeID string) error
	AttachVolume(instanceID, volumeID string) (string, error)
	ListVolumes(limit int, startingToken string) ([]volumes.Volume, string, error)
	WaitDiskAttached(instanceID string, volumeID string) error
	DetachVolume(instanceID, volumeID string) error
	WaitDiskDetached(instanceID string, volumeID string) error
	GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	GetVolume(volumeID string) (*volumes.Volume, error)
	GetVolumesByName(name string) ([]volumes.Volume, error)
	CreateSnapshot(name, volID string, tags *map[string]string) (*snapshots.Snapshot, error)
	ListSnapshots(filters map[string]string) ([]snapshots.Snapshot, string, error)
	DeleteSnapshot(snapID string) error
	GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error)
	WaitSnapshotReady(snapshotID string) error
	GetInstanceByID(instanceID string) (*servers.Server, error)
	ExpandVolume(volumeID string, size int) error
	GetMaxVolLimit() int64
	GetMetadataOpts() openstack_provider.MetadataOpts
	GetBlockStorageOpts() BlockStorageOpts
}

type OpenStack struct {
	compute      *gophercloud.ServiceClient
	blockstorage *gophercloud.ServiceClient
	bsOpts       BlockStorageOpts
	epOpts       gophercloud.EndpointOpts
	metadataOpts openstack_provider.MetadataOpts
}

type BlockStorageOpts struct {
	NodeVolumeAttachLimit int64 `gcfg:"node-volume-attach-limit"`
	RescanOnResize        bool  `gcfg:"rescan-on-resize"`
}

type Config struct {
	openstack_provider.Config
	BlockStorage BlockStorageOpts
}

func logcfg(cfg Config) {
	openstack_provider.LogCfg(cfg.Config)
	klog.Infof("Block storage opts: %v", cfg.BlockStorage)
}

// GetConfigFromFile retrieves config options from file
func GetConfigFromFile(configFilePath string) (Config, error) {
	var cfg Config
	config, err := os.Open(configFilePath)
	if err != nil {
		klog.V(3).Infof("Failed to open OpenStack configuration file: %v", err)
		return cfg, err
	}
	defer config.Close()

	err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if err != nil {
		klog.V(3).Infof("Failed to read OpenStack configuration file: %v", err)
		return cfg, err
	}

	return cfg, nil
}

const defaultMaxVolAttachLimit int64 = 256

var OsInstance IOpenStack = nil
var configFile = "/etc/cloud.conf"
var cfg Config

func InitOpenStackProvider(cfgFile string) {
	configFile = cfgFile
	klog.V(2).Infof("InitOpenStackProvider configFile: %s", configFile)
}

// CreateOpenStackProvider creates Openstack Instance
func CreateOpenStackProvider() (IOpenStack, error) {
	// Get config from file
	cfg, err := GetConfigFromFile(configFile)
	if err != nil {
		klog.Errorf("GetConfigFromFile %s failed with error: %v", configFile, err)
		return nil, err
	}
	logcfg(cfg)

	provider, err := openstack_provider.NewOpenStackClient(&cfg.Config.Global, "cinder-csi-plugin", userAgentData...)
	if err != nil {
		return nil, err
	}

	epOpts := gophercloud.EndpointOpts{
		Region: cfg.Global.Region,
	}

	// Init Nova ServiceClient
	computeclient, err := openstack.NewComputeV2(provider, epOpts)
	if err != nil {
		return nil, err
	}

	// Init Cinder ServiceClient
	blockstorageclient, err := openstack.NewBlockStorageV3(provider, epOpts)
	if err != nil {
		return nil, err
	}

	// if no search order given, use default
	if len(cfg.Config.Metadata.SearchOrder) == 0 {
		cfg.Config.Metadata.SearchOrder = fmt.Sprintf("%s,%s", md.ConfigDriveID, md.MetadataID)
	}

	// Init OpenStack
	OsInstance = &OpenStack{
		compute:      computeclient,
		blockstorage: blockstorageclient,
		bsOpts:       cfg.BlockStorage,
		epOpts:       epOpts,
		metadataOpts: cfg.Config.Metadata,
	}

	return OsInstance, nil
}

// GetOpenStackProvider returns Openstack Instance
func GetOpenStackProvider() (IOpenStack, error) {
	if OsInstance != nil {
		return OsInstance, nil
	}
	var err error
	OsInstance, err = CreateOpenStackProvider()
	if err != nil {
		return nil, err
	}

	return OsInstance, nil
}

// GetMetadataOpts returns metadataopts
func (os *OpenStack) GetMetadataOpts() openstack_provider.MetadataOpts {
	return os.metadataOpts
}
