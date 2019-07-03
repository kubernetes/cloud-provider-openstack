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
	"fmt"
	"net/http"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/spf13/pflag"
	gcfg "gopkg.in/gcfg.v1"
	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/klog"
)

// userAgentData is used to add extra information to the gophercloud user-agent
var userAgentData []string

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

type IOpenStack interface {
	CreateVolume(name string, size int, vtype, availability string, snapshotID string, tags *map[string]string) (*volumes.Volume, error)
	DeleteVolume(volumeID string) error
	AttachVolume(instanceID, volumeID string) (string, error)
	ListVolumes() ([]volumes.Volume, error)
	WaitDiskAttached(instanceID string, volumeID string) error
	DetachVolume(instanceID, volumeID string) error
	WaitDiskDetached(instanceID string, volumeID string) error
	GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	GetVolume(volumeID string) (*volumes.Volume, error)
	GetVolumesByName(name string) ([]volumes.Volume, error)
	CreateSnapshot(name, volID, description string, tags *map[string]string) (*snapshots.Snapshot, error)
	ListSnapshots(limit, offset int, filters map[string]string) ([]snapshots.Snapshot, error)
	DeleteSnapshot(snapID string) error
	GetSnapshotByNameAndVolumeID(n string, volumeId string) ([]snapshots.Snapshot, error)
	GetSnapshotByID(snapshotID string) (*snapshots.Snapshot, error)
	WaitSnapshotReady(snapshotID string) error
	GetInstanceByID(instanceID string) (*servers.Server, error)
	ExpandVolume(volumeID string, size int) error
	GetMaxVolLimit() int64
}

type OpenStack struct {
	compute      *gophercloud.ServiceClient
	blockstorage *gophercloud.ServiceClient
	bsOpts       BlockStorageOpts
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
	klog.Infof("AuthURL: %s", cfg.Global.AuthUrl)
	klog.Infof("Username: %s", cfg.Global.Username)
	klog.Infof("UserId: %s", cfg.Global.UserId)
	klog.Infof("TenantId: %s", cfg.Global.TenantId)
	klog.Infof("TenantName: %s", cfg.Global.TenantName)
	klog.Infof("DomainName: %s", cfg.Global.DomainName)
	klog.Infof("DomainId: %s", cfg.Global.DomainId)
	klog.Infof("TrustID: %s", cfg.Global.TrustID)
	klog.Infof("Region: %s", cfg.Global.Region)
	klog.Infof("CAFile: %s", cfg.Global.CAFile)
	klog.Infof("Block storage opts: %v", cfg.BlockStorage)
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

	userAgent := gophercloud.UserAgent{}
	userAgent.Prepend(fmt.Sprintf("cinder-csi-plugin/%s", version.Version))
	for _, data := range userAgentData {
		userAgent.Prepend(data)
	}
	provider.UserAgent = userAgent

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
		bsOpts:       cfg.BlockStorage,
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
