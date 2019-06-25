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
	"crypto/tls"
	"net/http"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	gcfg "gopkg.in/gcfg.v1"
	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog"
)

type IOpenStack interface {
	CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (*volumes.Volume, error)
	DeleteVolume(volumeID string) error
	AttachVolume(instanceID, volumeID string) (string, error)
	ListVolumes() ([]volumes.Volume, error)
	WaitDiskAttached(instanceID string, volumeID string) error
	DetachVolume(instanceID, volumeID string) error
	WaitDiskDetached(instanceID string, volumeID string) error
	GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	GetVolumesByName(name string) ([]volumes.Volume, error)
	GetVolume(volumeID string) (*volumes.Volume, error)
	CreateSnapshot(name, volID, description string, tags *map[string]string) (*snapshots.Snapshot, error)
	ListSnapshots(limit, offset int, filters map[string]string) ([]snapshots.Snapshot, error)
	DeleteSnapshot(snapID string) error
	GetSnapshotByNameAndVolumeID(n string, volumeId string) ([]snapshots.Snapshot, error)
	GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error)
	WaitSnapshotReady(snapshotID string) error
}

type OpenStack struct {
	compute      *gophercloud.ServiceClient
	blockstorage *gophercloud.ServiceClient
}

type BlockStorageOpts struct {
	NodeVolumeAttachLimit int64 `gcfg:"node-volume-attach-limit"`
}

type Config struct {
	Global struct {
		AuthUrl    string `gcfg:"auth-url"`
		Username   string
		UserId     string `gcfg:"user-id"`
		Password   string
		TenantId   string `gcfg:"tenant-id"`
		TenantName string `gcfg:"tenant-name"`
		TrustID    string `gcfg:"trust-id"`
		DomainId   string `gcfg:"domain-id"`
		DomainName string `gcfg:"domain-name"`
		Region     string
		CAFile     string `gcfg:"ca-file"`
	}
	BlockStorage BlockStorageOpts
}

func logcfg(cfg Config) {
	klog.V(5).Infof("AuthURL: %s", cfg.Global.AuthUrl)
	klog.V(5).Infof("Username: %s", cfg.Global.Username)
	klog.V(5).Infof("UserId: %s", cfg.Global.UserId)
	klog.V(5).Infof("TenantId: %s", cfg.Global.TenantId)
	klog.V(5).Infof("TenantName: %s", cfg.Global.TenantName)
	klog.V(5).Infof("DomainName: %s", cfg.Global.DomainName)
	klog.V(5).Infof("DomainId: %s", cfg.Global.DomainId)
	klog.V(5).Infof("TrustID: %s", cfg.Global.TrustID)
	klog.V(5).Infof("Region: %s", cfg.Global.Region)
	klog.V(5).Infof("CAFile: %s", cfg.Global.CAFile)
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

func (cfg Config) toAuth3Options() tokens3.AuthOptions {
	return tokens3.AuthOptions{
		IdentityEndpoint: cfg.Global.AuthUrl,
		Username:         cfg.Global.Username,
		UserID:           cfg.Global.UserId,
		Password:         cfg.Global.Password,
		DomainID:         cfg.Global.DomainId,
		DomainName:       cfg.Global.DomainName,
		AllowReauth:      true,
	}
}

// GetConfigFromFile retrieves config options from file
func GetConfigFromFile(configFilePath string) (Config, gophercloud.EndpointOpts, error) {
	var epOpts gophercloud.EndpointOpts
	var cfg Config
	config, err := os.Open(configFilePath)
	if err != nil {
		klog.V(3).Infof("Failed to open OpenStack configuration file: %v", err)
		return cfg, epOpts, err
	}
	defer config.Close()

	err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if err != nil {
		klog.V(3).Infof("Failed to read OpenStack configuration file: %v", err)
		return cfg, epOpts, err
	}

	epOpts = gophercloud.EndpointOpts{
		Region: cfg.Global.Region,
	}

	return cfg, epOpts, nil
}

// GetConfigFromEnv retrieves config options from env
func GetConfigFromEnv() (gophercloud.AuthOptions, gophercloud.EndpointOpts, error) {
	// Get config from env
	authOpts, err := openstack.AuthOptionsFromEnv()
	var epOpts gophercloud.EndpointOpts
	if err != nil {
		klog.V(3).Infof("Failed to read OpenStack configuration from env: %v", err)
		return authOpts, epOpts, err
	}

	epOpts = gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	}

	return authOpts, epOpts, nil
}

const maxVol int64 = 256

var OsInstance IOpenStack = nil
var configFile = "/etc/cloud.conf"
var cfg Config

func InitOpenStackProvider(cfgFile string) {
	configFile = cfgFile
	klog.V(2).Infof("InitOpenStackProvider configFile: %s", configFile)
}

// CreateOpenStackProvider creates Openstack Instance
func CreateOpenStackProvider() (IOpenStack, error) {
	var authOpts gophercloud.AuthOptions
	var authURL string
	var caFile string
	// Get config from file
	cfg, epOpts, err := GetConfigFromFile(configFile)
	if err == nil {
		authOpts = cfg.toAuthOptions()
		authURL = authOpts.IdentityEndpoint
		caFile = cfg.Global.CAFile
	} else {
		// Get config from env
		authOpts, epOpts, err = GetConfigFromEnv()
		if err != nil {
			return nil, err
		}
		authURL = authOpts.IdentityEndpoint
	}
	logcfg(cfg)

	provider, err := openstack.NewClient(authURL)
	if err != nil {
		return nil, err
	}
	if caFile != "" {
		roots, err := certutil.NewPool(caFile)
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

	// Init OpenStack
	OsInstance = &OpenStack{
		compute:      computeclient,
		blockstorage: blockstorageclient,
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

//GetMaxVolLimit returns max vol limit
func GetMaxVolLimit() int64 {
	if cfg.BlockStorage.NodeVolumeAttachLimit > 0 && cfg.BlockStorage.NodeVolumeAttachLimit <= 256 {
		return cfg.BlockStorage.NodeVolumeAttachLimit
	}

	return maxVol

}
