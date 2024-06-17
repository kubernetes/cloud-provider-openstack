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
	"net/http"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/backups"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/spf13/pflag"
	gcfg "gopkg.in/gcfg.v1"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
)

// userAgentData is used to add extra information to the gophercloud user-agent
var userAgentData []string

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

type IOpenStack interface {
	CreateVolume(name string, size int, vtype, availability string, snapshotID string, sourceVolID string, sourceBackupID string, tags map[string]string) (*volumes.Volume, error)
	DeleteVolume(volumeID string) error
	AttachVolume(instanceID, volumeID string) (string, error)
	ListVolumes(limit int, startingToken string) ([]volumes.Volume, string, error)
	WaitDiskAttached(instanceID string, volumeID string) error
	DetachVolume(instanceID, volumeID string) error
	WaitDiskDetached(instanceID string, volumeID string) error
	WaitVolumeTargetStatus(volumeID string, tStatus []string) error
	GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	GetVolume(volumeID string) (*volumes.Volume, error)
	GetVolumesByName(name string) ([]volumes.Volume, error)
	CreateSnapshot(name, volID string, tags map[string]string) (*snapshots.Snapshot, error)
	ListSnapshots(filters map[string]string) ([]snapshots.Snapshot, string, error)
	DeleteSnapshot(snapID string) error
	GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error)
	WaitSnapshotReady(snapshotID string) (string, error)
	CreateBackup(name, volID, snapshotID, availabilityZone string, tags map[string]string) (*backups.Backup, error)
	ListBackups(filters map[string]string) ([]backups.Backup, error)
	DeleteBackup(backupID string) error
	GetBackupByID(backupID string) (*backups.Backup, error)
	BackupsAreEnabled() (bool, error)
	WaitBackupReady(backupID string, snapshotSize int, backupMaxDurationSecondsPerGB int) (string, error)
	GetInstanceByID(instanceID string) (*servers.Server, error)
	ExpandVolume(volumeID string, status string, size int) error
	GetMaxVolLimit() int64
	GetMetadataOpts() metadata.Opts
	GetBlockStorageOpts() BlockStorageOpts
}

type OpenStack struct {
	compute      *gophercloud.ServiceClient
	blockstorage *gophercloud.ServiceClient
	bsOpts       BlockStorageOpts
	epOpts       gophercloud.EndpointOpts
	metadataOpts metadata.Opts
}

type BlockStorageOpts struct {
	NodeVolumeAttachLimit    int64 `gcfg:"node-volume-attach-limit"`
	RescanOnResize           bool  `gcfg:"rescan-on-resize"`
	IgnoreVolumeAZ           bool  `gcfg:"ignore-volume-az"`
	IgnoreVolumeMicroversion bool  `gcfg:"ignore-volume-microversion"`
}

type Config struct {
	Global       map[string]*client.AuthOpts
	Metadata     metadata.Opts
	BlockStorage BlockStorageOpts
}

func logcfg(cfg Config) {
	for cloudName, global := range cfg.Global {
		klog.V(0).Infof("Global: \"%s\"", cloudName)
		client.LogCfg(*global)
	}
	klog.Infof("Block storage opts: %v", cfg.BlockStorage)
}

// GetConfigFromFiles retrieves config options from file
func GetConfigFromFiles(configFilePaths []string) (Config, error) {
	var cfg Config

	// Read all specified config files in order. Values from later config files
	// will overwrite values from earlier ones.
	for _, configFilePath := range configFilePaths {
		config, err := os.Open(configFilePath)
		if err != nil {
			klog.Errorf("Failed to open OpenStack configuration file: %v", err)
			return cfg, err
		}
		defer config.Close()

		err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
		if err != nil {
			klog.Errorf("Failed to read OpenStack configuration file: %v", err)
			return cfg, err
		}
	}

	for _, global := range cfg.Global {
		// Update the config with data from clouds.yaml if UseClouds is enabled
		if global.UseClouds {
			if global.CloudsFile != "" {
				os.Setenv("OS_CLIENT_CONFIG_FILE", global.CloudsFile)
			}
			err := client.ReadClouds(global)
			if err != nil {
				return cfg, err
			}
			klog.V(5).Infof("Credentials are loaded from %s:", global.CloudsFile)
		}
	}

	return cfg, nil
}

const defaultMaxVolAttachLimit int64 = 256

var OsInstances map[string]IOpenStack
var configFiles = []string{"/etc/cloud.conf"}

func InitOpenStackProvider(cfgFiles []string, httpEndpoint string) {
	OsInstances = make(map[string]IOpenStack)
	metrics.RegisterMetrics("cinder-csi")
	if httpEndpoint != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", legacyregistry.HandlerWithReset())
		go func() {
			err := http.ListenAndServe(httpEndpoint, mux)
			if err != nil {
				klog.Fatalf("failed to listen & serve metrics from %q: %v", httpEndpoint, err)
			}
			klog.Infof("metrics available in %q", httpEndpoint)
		}()
	}

	configFiles = cfgFiles
	klog.V(2).Infof("InitOpenStackProvider configFiles: %s", configFiles)
}

// CreateOpenStackProvider creates Openstack Instance with custom Global config param
func CreateOpenStackProvider(cloudName string) (IOpenStack, error) {
	// Get config from file
	cfg, err := GetConfigFromFiles(configFiles)
	if err != nil {
		klog.Errorf("GetConfigFromFiles %s failed with error: %v", configFiles, err)
		return nil, err
	}
	logcfg(cfg)
	_, cloudNameDefined := cfg.Global[cloudName]
	if !cloudNameDefined {
		return nil, fmt.Errorf("GetConfigFromFiles cloud name \"%s\" not found in configuration files: %s", cloudName, configFiles)
	}

	provider, err := client.NewOpenStackClient(cfg.Global[cloudName], "cinder-csi-plugin", userAgentData...)
	if err != nil {
		return nil, err
	}

	epOpts := gophercloud.EndpointOpts{
		Region:       cfg.Global[cloudName].Region,
		Availability: cfg.Global[cloudName].EndpointType,
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
	if len(cfg.Metadata.SearchOrder) == 0 {
		cfg.Metadata.SearchOrder = fmt.Sprintf("%s,%s", metadata.ConfigDriveID, metadata.MetadataID)
	}

	// Init OpenStack
	OsInstances[cloudName] = &OpenStack{
		compute:      computeclient,
		blockstorage: blockstorageclient,
		bsOpts:       cfg.BlockStorage,
		epOpts:       epOpts,
		metadataOpts: cfg.Metadata,
	}

	return OsInstances[cloudName], nil
}

// GetOpenStackProvider returns Openstack Instance
func GetOpenStackProvider(cloudName string) (IOpenStack, error) {
	OsInstance, OsInstanceDefined := OsInstances[cloudName]
	if OsInstanceDefined {
		return OsInstance, nil
	}
	OsInstance, err := CreateOpenStackProvider(cloudName)
	if err != nil {
		return nil, err
	}

	return OsInstance, nil
}

// GetMetadataOpts returns metadataopts
func (os *OpenStack) GetMetadataOpts() metadata.Opts {
	return os.metadataOpts
}
