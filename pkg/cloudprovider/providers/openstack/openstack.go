/*
Copyright 2014 The Kubernetes Authors.

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
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"k8s.io/cloud-provider-openstack/pkg/util"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/gophercloud/utils/client"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/pflag"
	gcfg "gopkg.in/gcfg.v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/klog/v2"
)

const (
	// ProviderName is the name of the openstack provider
	ProviderName = "openstack"

	// TypeHostName is the name type of openstack instance
	TypeHostName     = "hostname"
	availabilityZone = "availability_zone"
	defaultTimeOut   = 60 * time.Second
)

// ErrNotFound is used to inform that the object is missing
var ErrNotFound = errors.New("failed to find object")

// ErrMultipleResults is used when we unexpectedly get back multiple results
var ErrMultipleResults = errors.New("multiple results where only one expected")

// ErrNoAddressFound is used when we cannot find an ip address for the host
var ErrNoAddressFound = errors.New("no address found for host")

// ErrIPv6SupportDisabled is used when one tries to use IPv6 Addresses when
// IPv6 support is disabled by config
var ErrIPv6SupportDisabled = errors.New("IPv6 support is disabled")

// userAgentData is used to add extra information to the gophercloud user-agent
var userAgentData []string

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

// MyDuration is the encoding.TextUnmarshaler interface for time.Duration
type MyDuration struct {
	time.Duration
}

// UnmarshalText is used to convert from text to Duration
func (d *MyDuration) UnmarshalText(text []byte) error {
	res, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = res
	return nil
}

// LoadBalancer is used for creating and maintaining load balancers
type LoadBalancer struct {
	network *gophercloud.ServiceClient
	compute *gophercloud.ServiceClient
	lb      *gophercloud.ServiceClient
	opts    LoadBalancerOpts
}

// LoadBalancerOpts have the options to talk to Neutron LBaaSV2 or Octavia
type LoadBalancerOpts struct {
	LBVersion            string              `gcfg:"lb-version"`          // overrides autodetection. Only support v2.
	UseOctavia           bool                `gcfg:"use-octavia"`         // uses Octavia V2 service catalog endpoint
	SubnetID             string              `gcfg:"subnet-id"`           // overrides autodetection.
	NetworkID            string              `gcfg:"network-id"`          // If specified, will create virtual ip from a subnet in network which has available IP addresses
	FloatingNetworkID    string              `gcfg:"floating-network-id"` // If specified, will create floating ip for loadbalancer, or do not create floating ip.
	FloatingSubnetID     string              `gcfg:"floating-subnet-id"`  // If specified, will create floating ip for loadbalancer in this particular floating pool subnetwork.
	LBClasses            map[string]*LBClass // Predefined named Floating networks and subnets
	LBMethod             string              `gcfg:"lb-method"` // default to ROUND_ROBIN.
	LBProvider           string              `gcfg:"lb-provider"`
	CreateMonitor        bool                `gcfg:"create-monitor"`
	MonitorDelay         MyDuration          `gcfg:"monitor-delay"`
	MonitorTimeout       MyDuration          `gcfg:"monitor-timeout"`
	MonitorMaxRetries    uint                `gcfg:"monitor-max-retries"`
	ManageSecurityGroups bool                `gcfg:"manage-security-groups"`
	NodeSecurityGroupIDs []string            // Do not specify, get it automatically when enable manage-security-groups. TODO(FengyunPan): move it into cache
	InternalLB           bool                `gcfg:"internal-lb"`    // default false
	CascadeDelete        bool                `gcfg:"cascade-delete"` // applicable only if use-octavia is set to True
	FlavorID             string              `gcfg:"flavor-id"`
}

// LBClass defines the corresponding floating network, floating subnet or internal subnet ID
type LBClass struct {
	FloatingNetworkID string `gcfg:"floating-network-id,omitempty"`
	FloatingSubnetID  string `gcfg:"floating-subnet-id,omitempty"`
	SubnetID          string `gcfg:"subnet-id,omitempty"`
	NetworkID         string `gcfg:"network-id,omitempty"`
}

// BlockStorageOpts is used to talk to Cinder service
type BlockStorageOpts struct {
	BSVersion       string `gcfg:"bs-version"`        // overrides autodetection. v1 or v2. Defaults to auto
	TrustDevicePath bool   `gcfg:"trust-device-path"` // See Issue #33128
	IgnoreVolumeAZ  bool   `gcfg:"ignore-volume-az"`
}

// NetworkingOpts is used for networking settings
type NetworkingOpts struct {
	IPv6SupportDisabled bool     `gcfg:"ipv6-support-disabled"`
	PublicNetworkName   []string `gcfg:"public-network-name"`
	InternalNetworkName []string `gcfg:"internal-network-name"`
}

// RouterOpts is used for Neutron routes
type RouterOpts struct {
	RouterID string `gcfg:"router-id"` // required
}

// MetadataOpts is used for configuring how to talk to metadata service or config drive
type MetadataOpts struct {
	SearchOrder    string     `gcfg:"search-order"`
	RequestTimeout MyDuration `gcfg:"request-timeout"`
}

type ServerAttributesExt struct {
	servers.Server
	availabilityzones.ServerAvailabilityZoneExt
}

// OpenStack is an implementation of cloud provider Interface for OpenStack.
type OpenStack struct {
	provider       *gophercloud.ProviderClient
	region         string
	lbOpts         LoadBalancerOpts
	bsOpts         BlockStorageOpts
	routeOpts      RouterOpts
	metadataOpts   MetadataOpts
	networkingOpts NetworkingOpts
	// InstanceID of the server where this OpenStack object is instantiated.
	localInstanceID string
}

type AuthOpts struct {
	AuthURL          string `gcfg:"auth-url" mapstructure:"auth-url" name:"os-authURL" dependsOn:"os-password|os-trustID|os-applicationCredentialSecret|os-clientCertPath"`
	UserID           string `gcfg:"user-id" mapstructure:"user-id" name:"os-userID" value:"optional" dependsOn:"os-password"`
	Username         string `name:"os-userName" value:"optional" dependsOn:"os-password"`
	Password         string `name:"os-password" value:"optional" dependsOn:"os-domainID|os-domainName,os-projectID|os-projectName,os-userID|os-userName"`
	TenantID         string `gcfg:"tenant-id" mapstructure:"project-id" name:"os-projectID" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	TenantName       string `gcfg:"tenant-name" mapstructure:"project-name" name:"os-projectName" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	TrustID          string `gcfg:"trust-id" mapstructure:"trust-id" name:"os-trustID" value:"optional"`
	DomainID         string `gcfg:"domain-id" mapstructure:"domain-id" name:"os-domainID" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	DomainName       string `gcfg:"domain-name" mapstructure:"domain-name" name:"os-domainName" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	TenantDomainID   string `gcfg:"tenant-domain-id" mapstructure:"project-domain-id" name:"os-projectDomainID" value:"optional"`
	TenantDomainName string `gcfg:"tenant-domain-name" mapstructure:"project-domain-name" name:"os-projectDomainName" value:"optional"`
	UserDomainID     string `gcfg:"user-domain-id" mapstructure:"user-domain-id" name:"os-userDomainID" value:"optional"`
	UserDomainName   string `gcfg:"user-domain-name" mapstructure:"user-domain-name" name:"os-userDomainName" value:"optional"`
	Region           string `name:"os-region"`
	CAFile           string `gcfg:"ca-file" mapstructure:"ca-file" name:"os-certAuthorityPath" value:"optional"`

	// TLS client auth
	CertFile string `gcfg:"cert-file" mapstructure:"cert-file" name:"os-clientCertPath" value:"optional" dependsOn:"os-clientKeyPath"`
	KeyFile  string `gcfg:"key-file" mapstructure:"key-file" name:"os-clientKeyPath" value:"optional" dependsOn:"os-clientCertPath"`

	// Manila only options
	TLSInsecure string `name:"os-TLSInsecure" value:"optional" matches:"^true|false$"`
	// backward compatibility with the manila-csi-plugin
	CAFileContents  string `name:"os-certAuthority" value:"optional"`
	TrusteeID       string `name:"os-trusteeID" value:"optional" dependsOn:"os-trustID"`
	TrusteePassword string `name:"os-trusteePassword" value:"optional" dependsOn:"os-trustID"`

	UseClouds  bool   `gcfg:"use-clouds" mapstructure:"use-clouds" name:"os-useClouds" value:"optional"`
	CloudsFile string `gcfg:"clouds-file,omitempty" mapstructure:"clouds-file,omitempty" name:"os-cloudsFile" value:"optional"`
	Cloud      string `gcfg:"cloud,omitempty" mapstructure:"cloud,omitempty" name:"os-cloud" value:"optional"`

	ApplicationCredentialID     string `gcfg:"application-credential-id" mapstructure:"application-credential-id" name:"os-applicationCredentialID" value:"optional"`
	ApplicationCredentialName   string `gcfg:"application-credential-name" mapstructure:"application-credential-name" name:"os-applicationCredentialName" value:"optional"`
	ApplicationCredentialSecret string `gcfg:"application-credential-secret" mapstructure:"application-credential-secret" name:"os-applicationCredentialSecret" value:"optional"`
}

// Config is used to read and store information from the cloud configuration file
type Config struct {
	Global            AuthOpts
	LoadBalancer      LoadBalancerOpts
	LoadBalancerClass map[string]*LBClass
	BlockStorage      BlockStorageOpts
	Route             RouterOpts
	Metadata          MetadataOpts
	Networking        NetworkingOpts
}

func LogCfg(cfg Config) {
	klog.V(5).Infof("AuthURL: %s", cfg.Global.AuthURL)
	klog.V(5).Infof("UserID: %s", cfg.Global.UserID)
	klog.V(5).Infof("Username: %s", cfg.Global.Username)
	klog.V(5).Infof("TenantID: %s", cfg.Global.TenantID)
	klog.V(5).Infof("TenantName: %s", cfg.Global.TenantName)
	klog.V(5).Infof("TrustID: %s", cfg.Global.TrustID)
	klog.V(5).Infof("DomainID: %s", cfg.Global.DomainID)
	klog.V(5).Infof("DomainName: %s", cfg.Global.DomainName)
	klog.V(5).Infof("TenantDomainID: %s", cfg.Global.TenantDomainID)
	klog.V(5).Infof("TenantDomainName: %s", cfg.Global.TenantDomainName)
	klog.V(5).Infof("UserDomainID: %s", cfg.Global.UserDomainID)
	klog.V(5).Infof("UserDomainName: %s", cfg.Global.UserDomainName)
	klog.V(5).Infof("Region: %s", cfg.Global.Region)
	klog.V(5).Infof("CAFile: %s", cfg.Global.CAFile)
	klog.V(5).Infof("CertFile: %s", cfg.Global.CertFile)
	klog.V(5).Infof("KeyFile: %s", cfg.Global.KeyFile)
	klog.V(5).Infof("UseClouds: %t", cfg.Global.UseClouds)
	klog.V(5).Infof("CloudsFile: %s", cfg.Global.CloudsFile)
	klog.V(5).Infof("Cloud: %s", cfg.Global.Cloud)
	klog.V(5).Infof("ApplicationCredentialID: %s", cfg.Global.ApplicationCredentialID)
	klog.V(5).Infof("ApplicationCredentialName: %s", cfg.Global.ApplicationCredentialName)
}

type Logger struct{}

func (l Logger) Printf(format string, args ...interface{}) {
	debugger := klog.V(6).Enabled()

	// extra check in case, when verbosity has been changed dynamically
	if debugger {
		var skip int
		var found bool
		var gc = "/github.com/gophercloud/gophercloud"

		// detect the depth of the actual function, which calls gophercloud code
		// 10 is the common depth from the logger to "github.com/gophercloud/gophercloud"
		for i := 10; i <= 20; i++ {
			if _, file, _, ok := runtime.Caller(i); ok && !found && strings.Contains(file, gc) {
				found = true
				continue
			} else if ok && found && !strings.Contains(file, gc) {
				skip = i
				break
			} else if !ok {
				break
			}
		}

		for _, v := range strings.Split(fmt.Sprintf(format, args...), "\n") {
			klog.InfoDepth(skip, v)
		}
	}
}

func init() {
	RegisterMetrics()

	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		cfg, err := ReadConfig(config)
		if err != nil {
			klog.Warningf("failed to read config: %v", err)
			return nil, err
		}
		cloud, err := NewOpenStack(cfg)
		if err != nil {
			klog.Warningf("New openstack client created failed with config: %v", err)
		}
		return cloud, err
	})
}

func (cfg AuthOpts) ToAuthOptions() gophercloud.AuthOptions {
	opts := clientconfig.ClientOpts{
		// this is needed to disable the clientconfig.AuthOptions func env detection
		EnvPrefix: "_",
		Cloud:     cfg.Cloud,
		AuthInfo: &clientconfig.AuthInfo{
			AuthURL:                     cfg.AuthURL,
			UserID:                      cfg.UserID,
			Username:                    cfg.Username,
			Password:                    cfg.Password,
			ProjectID:                   cfg.TenantID,
			ProjectName:                 cfg.TenantName,
			DomainID:                    cfg.DomainID,
			DomainName:                  cfg.DomainName,
			ProjectDomainID:             cfg.TenantDomainID,
			ProjectDomainName:           cfg.TenantDomainName,
			UserDomainID:                cfg.UserDomainID,
			UserDomainName:              cfg.UserDomainName,
			ApplicationCredentialID:     cfg.ApplicationCredentialID,
			ApplicationCredentialName:   cfg.ApplicationCredentialName,
			ApplicationCredentialSecret: cfg.ApplicationCredentialSecret,
		},
	}

	ao, err := clientconfig.AuthOptions(&opts)
	if err != nil {
		klog.V(1).Infof("Error parsing auth: %s", err)
		return gophercloud.AuthOptions{}
	}

	// Persistent service, so we need to be able to renew tokens.
	ao.AllowReauth = true

	return *ao
}

func (cfg AuthOpts) ToAuth3Options() tokens3.AuthOptions {
	ao := cfg.ToAuthOptions()

	var scope tokens3.Scope
	if ao.Scope != nil {
		scope.ProjectID = ao.Scope.ProjectID
		scope.ProjectName = ao.Scope.ProjectName
		scope.DomainID = ao.Scope.DomainID
		scope.DomainName = ao.Scope.DomainName
	}

	return tokens3.AuthOptions{
		IdentityEndpoint:            ao.IdentityEndpoint,
		UserID:                      ao.UserID,
		Username:                    ao.Username,
		Password:                    ao.Password,
		DomainID:                    ao.DomainID,
		DomainName:                  ao.DomainName,
		ApplicationCredentialID:     ao.ApplicationCredentialID,
		ApplicationCredentialName:   ao.ApplicationCredentialName,
		ApplicationCredentialSecret: ao.ApplicationCredentialSecret,
		Scope:                       scope,
		AllowReauth:                 ao.AllowReauth,
	}
}

// ReadConfig reads values from the cloud.conf
func ReadConfig(config io.Reader) (Config, error) {
	if config == nil {
		return Config{}, fmt.Errorf("no OpenStack cloud provider config file given")
	}
	var cfg Config

	// Set default values explicitly
	cfg.LoadBalancer.UseOctavia = true
	cfg.LoadBalancer.InternalLB = false
	cfg.LoadBalancer.LBProvider = "amphora"
	cfg.LoadBalancer.LBMethod = "ROUND_ROBIN"
	cfg.LoadBalancer.CreateMonitor = false
	cfg.LoadBalancer.ManageSecurityGroups = false
	cfg.LoadBalancer.MonitorDelay = MyDuration{5 * time.Second}
	cfg.LoadBalancer.MonitorTimeout = MyDuration{3 * time.Second}
	cfg.LoadBalancer.MonitorMaxRetries = 1
	cfg.LoadBalancer.CascadeDelete = true

	err := gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if err != nil {
		return Config{}, err
	}

	klog.V(5).Infof("Config, loaded from the config file:")
	LogCfg(cfg)

	if cfg.Global.UseClouds {
		if cfg.Global.CloudsFile != "" {
			os.Setenv("OS_CLIENT_CONFIG_FILE", cfg.Global.CloudsFile)
		}
		err = ReadClouds(&cfg)
		if err != nil {
			return Config{}, err
		}
		klog.V(5).Infof("Config, loaded from the %s:", cfg.Global.CloudsFile)
		LogCfg(cfg)
	}
	// Set the default values for search order if not set
	if cfg.Metadata.SearchOrder == "" {
		cfg.Metadata.SearchOrder = fmt.Sprintf("%s,%s", metadata.ConfigDriveID, metadata.MetadataID)
	}

	return cfg, err
}

// replaceEmpty is a helper function to replace empty fields with another field
func replaceEmpty(a string, b string) string {
	if a == "" {
		return b
	}
	return a
}

// ReadClouds reads Reads clouds.yaml to generate a Config
// Allows the cloud-config to have priority
func ReadClouds(cfg *Config) error {
	co := new(clientconfig.ClientOpts)
	if cfg.Global.Cloud != "" {
		co.Cloud = cfg.Global.Cloud
	}
	cloud, err := clientconfig.GetCloudFromYAML(co)
	if err != nil && err.Error() != "unable to load clouds.yaml: no clouds.yaml file found" {
		return err
	}

	cfg.Global.AuthURL = replaceEmpty(cfg.Global.AuthURL, cloud.AuthInfo.AuthURL)
	cfg.Global.UserID = replaceEmpty(cfg.Global.UserID, cloud.AuthInfo.UserID)
	cfg.Global.Username = replaceEmpty(cfg.Global.Username, cloud.AuthInfo.Username)
	cfg.Global.Password = replaceEmpty(cfg.Global.Password, cloud.AuthInfo.Password)
	cfg.Global.TenantID = replaceEmpty(cfg.Global.TenantID, cloud.AuthInfo.ProjectID)
	cfg.Global.TenantName = replaceEmpty(cfg.Global.TenantName, cloud.AuthInfo.ProjectName)
	cfg.Global.DomainID = replaceEmpty(cfg.Global.DomainID, cloud.AuthInfo.DomainID)
	cfg.Global.DomainName = replaceEmpty(cfg.Global.DomainName, cloud.AuthInfo.DomainName)
	cfg.Global.TenantDomainID = replaceEmpty(cfg.Global.TenantDomainID, cloud.AuthInfo.ProjectDomainID)
	cfg.Global.TenantDomainName = replaceEmpty(cfg.Global.TenantDomainName, cloud.AuthInfo.ProjectDomainName)
	cfg.Global.UserDomainID = replaceEmpty(cfg.Global.UserDomainID, cloud.AuthInfo.UserDomainID)
	cfg.Global.UserDomainName = replaceEmpty(cfg.Global.UserDomainName, cloud.AuthInfo.UserDomainName)
	cfg.Global.Region = replaceEmpty(cfg.Global.Region, cloud.RegionName)
	cfg.Global.CAFile = replaceEmpty(cfg.Global.CAFile, cloud.CACertFile)
	cfg.Global.CertFile = replaceEmpty(cfg.Global.CertFile, cloud.ClientCertFile)
	cfg.Global.KeyFile = replaceEmpty(cfg.Global.KeyFile, cloud.ClientKeyFile)
	cfg.Global.ApplicationCredentialID = replaceEmpty(cfg.Global.ApplicationCredentialID, cloud.AuthInfo.ApplicationCredentialID)
	cfg.Global.ApplicationCredentialName = replaceEmpty(cfg.Global.ApplicationCredentialName, cloud.AuthInfo.ApplicationCredentialName)
	cfg.Global.ApplicationCredentialSecret = replaceEmpty(cfg.Global.ApplicationCredentialSecret, cloud.AuthInfo.ApplicationCredentialSecret)

	return nil
}

// caller is a tiny helper for conditional unwind logic
type caller bool

func newCaller() caller   { return caller(true) }
func (c *caller) disarm() { *c = false }

func (c *caller) call(f func()) {
	if *c {
		f()
	}
}

func readInstanceID(searchOrder string) (string, error) {
	// First, try to get data from metadata service because local
	// data might be changed by accident
	md, err := metadata.Get(searchOrder)
	if err == nil {
		return md.UUID, nil
	}

	// Try to find instance ID on the local filesystem (created by cloud-init)
	const instanceIDFile = "/var/lib/cloud/data/instance-id"
	idBytes, err := ioutil.ReadFile(instanceIDFile)
	if err == nil {
		instanceID := string(idBytes)
		instanceID = strings.TrimSpace(instanceID)
		klog.V(3).Infof("Got instance id from %s: %s", instanceIDFile, instanceID)
		if instanceID != "" && instanceID != "iid-datasource-none" {
			return instanceID, nil
		}
	}

	return "", err
}

// check opts for OpenStack
func checkOpenStackOpts(openstackOpts *OpenStack) error {
	return checkMetadataSearchOrder(openstackOpts.metadataOpts.SearchOrder)
}

// NewOpenStackClient creates a new instance of the openstack client
func NewOpenStackClient(cfg *AuthOpts, userAgent string, extraUserAgent ...string) (*gophercloud.ProviderClient, error) {
	provider, err := openstack.NewClient(cfg.AuthURL)
	if err != nil {
		return nil, err
	}

	ua := gophercloud.UserAgent{}
	ua.Prepend(fmt.Sprintf("%s/%s", userAgent, version.Version))
	for _, data := range extraUserAgent {
		ua.Prepend(data)
	}
	provider.UserAgent = ua
	klog.V(4).Infof("Using user-agent %s", ua.Join())

	var caPool *x509.CertPool
	if cfg.CAFile != "" {
		// read and parse CA certificate from file
		caPool, err = certutil.NewPool(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read and parse %s certificate: %s", cfg.CAFile, err)
		}
	} else if cfg.CAFileContents != "" {
		// parse CA certificate from the contents
		caPool = x509.NewCertPool()
		if ok := caPool.AppendCertsFromPEM([]byte(cfg.CAFileContents)); !ok {
			return nil, fmt.Errorf("failed to parse os-certAuthority certificate")
		}
	}

	config := &tls.Config{}
	config.InsecureSkipVerify = cfg.TLSInsecure == "true"

	if caPool != nil {
		config.RootCAs = caPool
	}

	// configure TLS client auth
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading TLS key pair: %s", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}

	provider.HTTPClient.Transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: config})

	if klog.V(6).Enabled() {
		provider.HTTPClient.Transport = &client.RoundTripper{
			Rt:     provider.HTTPClient.Transport,
			Logger: &Logger{},
		}
	}

	if cfg.TrustID != "" {
		opts := cfg.ToAuth3Options()

		// support for the legacy manila auth
		// if TrusteeID and TrusteePassword were defined, then use them
		opts.UserID = replaceEmpty(cfg.TrusteeID, opts.UserID)
		opts.Password = replaceEmpty(cfg.TrusteePassword, opts.Password)

		authOptsExt := trusts.AuthOptsExt{
			TrustID:            cfg.TrustID,
			AuthOptionsBuilder: &opts,
		}
		err = openstack.AuthenticateV3(provider, authOptsExt, gophercloud.EndpointOpts{})

		return provider, err
	}

	opts := cfg.ToAuthOptions()
	err = openstack.Authenticate(provider, opts)

	return provider, err
}

// NewOpenStack creates a new new instance of the openstack struct from a config struct
func NewOpenStack(cfg Config) (*OpenStack, error) {
	provider, err := NewOpenStackClient(&cfg.Global, "openstack-cloud-controller-manager", userAgentData...)
	if err != nil {
		return nil, err
	}

	if cfg.Metadata.RequestTimeout == (MyDuration{}) {
		cfg.Metadata.RequestTimeout.Duration = time.Duration(defaultTimeOut)
	}
	provider.HTTPClient.Timeout = cfg.Metadata.RequestTimeout.Duration

	os := OpenStack{
		provider:       provider,
		region:         cfg.Global.Region,
		lbOpts:         cfg.LoadBalancer,
		bsOpts:         cfg.BlockStorage,
		routeOpts:      cfg.Route,
		metadataOpts:   cfg.Metadata,
		networkingOpts: cfg.Networking,
	}

	// ini file doesn't support maps so we are reusing top level sub sections
	// and copy the resulting map to corresponding loadbalancer section
	os.lbOpts.LBClasses = cfg.LoadBalancerClass

	err = checkOpenStackOpts(&os)
	if err != nil {
		return nil, err
	}

	return &os, nil
}

// Initialize passes a Kubernetes clientBuilder interface to the cloud provider
func (os *OpenStack) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

// mapNodeNameToServerName maps a k8s NodeName to an OpenStack Server Name
// This is a simple string cast.
func mapNodeNameToServerName(nodeName types.NodeName) string {
	return string(nodeName)
}

// GetNodeNameByID maps instanceid to types.NodeName
func (os *OpenStack) GetNodeNameByID(instanceID string) (types.NodeName, error) {
	client, err := os.NewComputeV2()
	var nodeName types.NodeName
	if err != nil {
		return nodeName, err
	}

	server, err := servers.Get(client, instanceID).Extract()
	if err != nil {
		return nodeName, err
	}
	nodeName = mapServerToNodeName(server)
	return nodeName, nil
}

// mapServerToNodeName maps an OpenStack Server to a k8s NodeName
func mapServerToNodeName(server *servers.Server) types.NodeName {
	// Node names are always lowercase, and (at least)
	// routecontroller does case-sensitive string comparisons
	// assuming this
	return types.NodeName(strings.ToLower(server.Name))
}

func foreachServer(client *gophercloud.ServiceClient, opts servers.ListOptsBuilder, handler func(*servers.Server) (bool, error)) error {
	pager := servers.List(client, opts)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		s, err := servers.ExtractServers(page)
		if err != nil {
			return false, err
		}
		for _, server := range s {
			ok, err := handler(&server)
			if !ok || err != nil {
				return false, err
			}
		}
		return true, nil
	})
	return err
}

func getServerByName(client *gophercloud.ServiceClient, name types.NodeName) (*ServerAttributesExt, error) {
	opts := servers.ListOpts{
		Name: fmt.Sprintf("^%s$", regexp.QuoteMeta(mapNodeNameToServerName(name))),
	}

	pager := servers.List(client, opts)

	var s []ServerAttributesExt
	serverList := make([]ServerAttributesExt, 0, 1)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		if err := servers.ExtractServersInto(page, &s); err != nil {
			return false, err
		}
		serverList = append(serverList, s...)
		if len(serverList) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	if len(serverList) == 0 {
		return nil, ErrNotFound
	}

	return &serverList[0], nil
}

// IP addresses order:
// * interfaces private IPs
// * access IPs
// * metadata hostname
// * server object Addresses (floating type)
func nodeAddresses(srv *servers.Server, interfaces []attachinterfaces.Interface, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	addrs := []v1.NodeAddress{}

	// parse private IP addresses first in an ordered manner
	for _, iface := range interfaces {
		for _, fixedIP := range iface.FixedIPs {
			if iface.PortState == "ACTIVE" {
				isIPv6 := net.ParseIP(fixedIP.IPAddress).To4() == nil
				if !(isIPv6 && networkingOpts.IPv6SupportDisabled) {
					AddToNodeAddresses(&addrs,
						v1.NodeAddress{
							Type:    v1.NodeInternalIP,
							Address: fixedIP.IPAddress,
						},
					)
				}
			}
		}
	}

	// process public IP addresses
	if srv.AccessIPv4 != "" {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv4,
			},
		)
	}

	if srv.AccessIPv6 != "" && !networkingOpts.IPv6SupportDisabled {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv6,
			},
		)
	}

	if srv.Metadata[TypeHostName] != "" {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeHostName,
				Address: srv.Metadata[TypeHostName],
			},
		)
	}

	// process the rest
	type Address struct {
		IPType string `mapstructure:"OS-EXT-IPS:type"`
		Addr   string
	}

	var addresses map[string][]Address
	err := mapstructure.Decode(srv.Addresses, &addresses)
	if err != nil {
		return nil, err
	}

	var networks []string
	for k := range addresses {
		networks = append(networks, k)
	}
	sort.Strings(networks)

	for _, network := range networks {
		for _, props := range addresses[network] {
			var addressType v1.NodeAddressType
			if props.IPType == "floating" {
				addressType = v1.NodeExternalIP
			} else if util.Contains(networkingOpts.PublicNetworkName, network) {
				addressType = v1.NodeExternalIP
				// removing already added address to avoid listing it as both ExternalIP and InternalIP
				// may happen due to listing "private" network as "public" in CCM's config
				RemoveFromNodeAddresses(&addrs,
					v1.NodeAddress{
						Address: props.Addr,
					},
				)
			} else {
				if len(networkingOpts.InternalNetworkName) == 0 || util.Contains(networkingOpts.InternalNetworkName, network) {
					addressType = v1.NodeInternalIP
				} else {
					klog.V(5).Infof("Node '%s' address '%s' ignored due to 'internal-network-name' option", srv.Name, props.Addr)
					RemoveFromNodeAddresses(&addrs,
						v1.NodeAddress{
							Address: props.Addr,
						},
					)
					continue
				}
			}

			isIPv6 := net.ParseIP(props.Addr).To4() == nil
			if !(isIPv6 && networkingOpts.IPv6SupportDisabled) {
				AddToNodeAddresses(&addrs,
					v1.NodeAddress{
						Type:    addressType,
						Address: props.Addr,
					},
				)
			}
		}
	}

	return addrs, nil
}

func getAddressesByName(client *gophercloud.ServiceClient, name types.NodeName, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	srv, err := getServerByName(client, name)
	if err != nil {
		return nil, err
	}

	interfaces, err := getAttachedInterfacesByID(client, srv.ID)
	if err != nil {
		return nil, err
	}

	return nodeAddresses(&srv.Server, interfaces, networkingOpts)
}

func getAddressByName(client *gophercloud.ServiceClient, name types.NodeName, needIPv6 bool, networkingOpts NetworkingOpts) (string, error) {
	if needIPv6 && networkingOpts.IPv6SupportDisabled {
		return "", ErrIPv6SupportDisabled
	}

	addrs, err := getAddressesByName(client, name, networkingOpts)
	if err != nil {
		return "", err
	} else if len(addrs) == 0 {
		return "", ErrNoAddressFound
	}

	for _, addr := range addrs {
		isIPv6 := net.ParseIP(addr.Address).To4() == nil
		if (addr.Type == v1.NodeInternalIP) && (isIPv6 == needIPv6) {
			return addr.Address, nil
		}
	}

	for _, addr := range addrs {
		isIPv6 := net.ParseIP(addr.Address).To4() == nil
		if (addr.Type == v1.NodeExternalIP) && (isIPv6 == needIPv6) {
			return addr.Address, nil
		}
	}
	// It should never return an address from a different IP Address family than the one needed
	return "", ErrNoAddressFound
}

// getAttachedInterfacesByID returns the node interfaces of the specified instance.
func getAttachedInterfacesByID(client *gophercloud.ServiceClient, serviceID string) ([]attachinterfaces.Interface, error) {
	var interfaces []attachinterfaces.Interface

	pager := attachinterfaces.List(client, serviceID)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		s, err := attachinterfaces.ExtractInterfaces(page)
		if err != nil {
			return false, err
		}
		interfaces = append(interfaces, s...)
		return true, nil
	})
	if err != nil {
		return interfaces, err
	}

	return interfaces, nil
}

// Clusters is a no-op
func (os *OpenStack) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

// ProviderName returns the cloud provider ID.
func (os *OpenStack) ProviderName() string {
	return ProviderName
}

// HasClusterID returns true if the cluster has a clusterID
func (os *OpenStack) HasClusterID() bool {
	return true
}

// LoadBalancer initializes a LbaasV2 object
func (os *OpenStack) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	klog.V(4).Info("openstack.LoadBalancer() called")

	network, err := os.NewNetworkV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Network client: %v", err)
		return nil, false
	}

	compute, err := os.NewComputeV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Compute client: %v", err)
		return nil, false
	}

	lb, err := os.NewLoadBalancerV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack LoadBalancer client: %v", err)
		return nil, false
	}

	// LBaaS v1 is deprecated in the OpenStack Liberty release.
	// Currently kubernetes OpenStack cloud provider just support LBaaS v2.
	lbVersion := os.lbOpts.LBVersion
	if lbVersion != "" && lbVersion != "v2" {
		klog.Warningf("Config error: currently only support LBaaS v2, unrecognised lb-version \"%v\"", lbVersion)
		return nil, false
	}

	klog.V(1).Info("Claiming to support LoadBalancer")

	return &LbaasV2{LoadBalancer{network, compute, lb, os.lbOpts}}, true
}

// Zones indicates that we support zones
func (os *OpenStack) Zones() (cloudprovider.Zones, bool) {
	klog.V(1).Info("Claiming to support Zones")
	return os, true
}

// GetZone returns the current zone
func (os *OpenStack) GetZone(ctx context.Context) (cloudprovider.Zone, error) {
	md, err := metadata.Get(os.metadataOpts.SearchOrder)
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	zone := cloudprovider.Zone{
		FailureDomain: md.AvailabilityZone,
		Region:        os.region,
	}
	klog.V(4).Infof("Current zone is %v", zone)
	return zone, nil
}

// GetZoneByProviderID implements Zones.GetZoneByProviderID
// This is particularly useful in external cloud providers where the kubelet
// does not initialize node data.
func (os *OpenStack) GetZoneByProviderID(ctx context.Context, providerID string) (cloudprovider.Zone, error) {
	instanceID, err := instanceIDFromProviderID(providerID)
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	compute, err := os.NewComputeV2()
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	var serverWithAttributesExt ServerAttributesExt
	if err := servers.Get(compute, instanceID).ExtractInto(&serverWithAttributesExt); err != nil {
		return cloudprovider.Zone{}, err
	}

	zone := cloudprovider.Zone{
		FailureDomain: serverWithAttributesExt.AvailabilityZone,
		Region:        os.region,
	}
	klog.V(4).Infof("The instance %s in zone %v", serverWithAttributesExt.Name, zone)
	return zone, nil
}

// GetZoneByNodeName implements Zones.GetZoneByNodeName
// This is particularly useful in external cloud providers where the kubelet
// does not initialize node data.
func (os *OpenStack) GetZoneByNodeName(ctx context.Context, nodeName types.NodeName) (cloudprovider.Zone, error) {
	compute, err := os.NewComputeV2()
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	srv, err := getServerByName(compute, nodeName)
	if err != nil {
		if err == ErrNotFound {
			return cloudprovider.Zone{}, cloudprovider.InstanceNotFound
		}
		return cloudprovider.Zone{}, err
	}

	zone := cloudprovider.Zone{
		FailureDomain: srv.AvailabilityZone,
		Region:        os.region,
	}
	klog.V(4).Infof("The instance %s in zone %v", srv.Name, zone)
	return zone, nil
}

// Routes initializes routes support
func (os *OpenStack) Routes() (cloudprovider.Routes, bool) {
	klog.V(4).Info("openstack.Routes() called")

	network, err := os.NewNetworkV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Network client: %v", err)
		return nil, false
	}

	netExts, err := networkExtensions(network)
	if err != nil {
		klog.Warningf("Failed to list neutron extensions: %v", err)
		return nil, false
	}

	if !netExts["extraroute"] {
		klog.V(3).Info("Neutron extraroute extension not found, required for Routes support")
		return nil, false
	}

	compute, err := os.NewComputeV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Compute client: %v", err)
		return nil, false
	}

	r, err := NewRoutes(compute, network, os.routeOpts, os.networkingOpts)
	if err != nil {
		klog.Warningf("Error initialising Routes support: %v", err)
		return nil, false
	}

	klog.V(1).Info("Claiming to support Routes")
	return r, true
}

func checkMetadataSearchOrder(order string) error {
	if order == "" {
		return errors.New("invalid value in section [Metadata] with key `search-order`. Value cannot be empty")
	}

	elements := strings.Split(order, ",")
	if len(elements) > 2 {
		return errors.New("invalid value in section [Metadata] with key `search-order`. Value cannot contain more than 2 elements")
	}

	for _, id := range elements {
		id = strings.TrimSpace(id)
		switch id {
		case metadata.ConfigDriveID:
		case metadata.MetadataID:
		default:
			return fmt.Errorf("invalid element %q found in section [Metadata] with key `search-order`."+
				"Supported elements include %q and %q", id, metadata.ConfigDriveID, metadata.MetadataID)
		}
	}

	return nil
}
