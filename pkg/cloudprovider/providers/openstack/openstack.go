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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/mitchellh/mapstructure"
	gcfg "gopkg.in/gcfg.v1"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
	cloudprovider "k8s.io/cloud-provider"
	v1helper "k8s.io/cloud-provider-openstack/pkg/apis/core/v1/helper"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/klog"
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
	LBVersion            string     `gcfg:"lb-version"`          // overrides autodetection. Only support v2.
	UseOctavia           bool       `gcfg:"use-octavia"`         // uses Octavia V2 service catalog endpoint
	SubnetID             string     `gcfg:"subnet-id"`           // overrides autodetection.
	FloatingNetworkID    string     `gcfg:"floating-network-id"` // If specified, will create floating ip for loadbalancer, or do not create floating ip.
	LBMethod             string     `gcfg:"lb-method"`           // default to ROUND_ROBIN.
	LBProvider           string     `gcfg:"lb-provider"`
	CreateMonitor        bool       `gcfg:"create-monitor"`
	MonitorDelay         MyDuration `gcfg:"monitor-delay"`
	MonitorTimeout       MyDuration `gcfg:"monitor-timeout"`
	MonitorMaxRetries    uint       `gcfg:"monitor-max-retries"`
	ManageSecurityGroups bool       `gcfg:"manage-security-groups"`
	NodeSecurityGroupIDs []string   // Do not specify, get it automatically when enable manage-security-groups. TODO(FengyunPan): move it into cache
	InternalLB           bool       `gcfg:"internal-lb"` // default false
}

// BlockStorageOpts is used to talk to Cinder service
type BlockStorageOpts struct {
	BSVersion       string `gcfg:"bs-version"`        // overrides autodetection. v1 or v2. Defaults to auto
	TrustDevicePath bool   `gcfg:"trust-device-path"` // See Issue #33128
	IgnoreVolumeAZ  bool   `gcfg:"ignore-volume-az"`
}

// NetworkingOpts is used for networking settings
type NetworkingOpts struct {
	IPv6SupportDisabled bool   `gcfg:"ipv6-support-disabled"`
	PublicNetworkName   string `gcfg:"public-network-name"`
	InternalNetworkName string `gcfg:"internal-network-name"`
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

// Config is used to read and store information from the cloud configuration file
type Config struct {
	Global struct {
		AuthURL          string `gcfg:"auth-url"`
		Username         string
		UserID           string `gcfg:"user-id"`
		Password         string
		TenantID         string `gcfg:"tenant-id"`
		TenantName       string `gcfg:"tenant-name"`
		TrustID          string `gcfg:"trust-id"`
		DomainID         string `gcfg:"domain-id"`
		DomainName       string `gcfg:"domain-name"`
		TenantDomainID   string `gcfg:"tenant-domain-id"`
		TenantDomainName string `gcfg:"tenant-domain-name"`
		Region           string
		CAFile           string `gcfg:"ca-file"`
		UseClouds        bool   `gcfg:"use-clouds"`
		CloudsFile       string `gcfg:"clouds-file,omitempty"`
		Cloud            string `gcfg:"cloud,omitempty"`
	}
	LoadBalancer LoadBalancerOpts
	BlockStorage BlockStorageOpts
	Route        RouterOpts
	Metadata     MetadataOpts
	Networking   NetworkingOpts
}

func logcfg(cfg Config) {
	klog.V(5).Infof("AuthURL: %s", cfg.Global.AuthURL)
	klog.V(5).Infof("Username: %s", cfg.Global.Username)
	klog.V(5).Infof("UserID: %s", cfg.Global.UserID)
	klog.V(5).Infof("TenantID: %s", cfg.Global.TenantID)
	klog.V(5).Infof("TenantName: %s", cfg.Global.TenantName)
	klog.V(5).Infof("DomainName: %s", cfg.Global.DomainName)
	klog.V(5).Infof("DomainID: %s", cfg.Global.DomainID)
	klog.V(5).Infof("TrustID: %s", cfg.Global.TrustID)
	klog.V(5).Infof("Region: %s", cfg.Global.Region)
	klog.V(5).Infof("CAFile: %s", cfg.Global.CAFile)
}

func init() {
	RegisterMetrics()

	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		cfg, err := ReadConfig(config)
		logcfg(cfg)
		if err != nil {
			return nil, err
		}
		cloud, err := NewOpenStack(cfg)
		if err != nil {
			klog.V(1).Infof("New openstack client created failed with config")
		}
		return cloud, err
	})
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

func (cfg Config) toAuth3Options() tokens3.AuthOptions {
	// Setting up a scope
	scope := tokens3.Scope{}

	// Gophercloud requires that either ProjectID or ProjectName is specified,
	// but not both.
	if cfg.Global.TenantID != "" {
		scope.ProjectID = cfg.Global.TenantID
	} else {
		scope.ProjectName = cfg.Global.TenantName

		// If Tenant Domain Name/ID was provided, then use it for the scope, otherwise
		// fall back to Domain Name/ID
		if cfg.Global.TenantDomainID != "" {
			scope.DomainID = cfg.Global.TenantDomainID
		} else {
			scope.DomainID = cfg.Global.DomainID
		}
		if cfg.Global.TenantDomainName != "" {
			scope.DomainName = cfg.Global.TenantDomainName
		} else {
			scope.DomainName = cfg.Global.DomainName
		}
	}

	return tokens3.AuthOptions{
		IdentityEndpoint: cfg.Global.AuthURL,
		Username:         cfg.Global.Username,
		UserID:           cfg.Global.UserID,
		Password:         cfg.Global.Password,
		DomainID:         cfg.Global.DomainID,
		DomainName:       cfg.Global.DomainName,
		Scope:            scope,
		AllowReauth:      true,
	}
}

// configFromEnv allows setting up credentials etc using the
// standard OS_* OpenStack client environment variables.
// TODO: Replace this with gophercloud upstream once community moves away from cloud.conf
func configFromEnv() (cfg Config, ok bool) {
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

	cfg.Global.TenantDomainID = os.Getenv("OS_PROJECT_DOMAIN_ID")
	cfg.Global.TenantDomainName = os.Getenv("OS_PROJECT_DOMAIN_NAME")

	ok = cfg.Global.AuthURL != "" &&
		cfg.Global.Username != "" &&
		cfg.Global.Password != "" &&
		(cfg.Global.TenantID != "" || cfg.Global.TenantName != "" ||
			cfg.Global.DomainID != "" || cfg.Global.DomainName != "" ||
			cfg.Global.Region != "" || cfg.Global.UserID != "" ||
			cfg.Global.TrustID != "")

	cfg.Metadata.SearchOrder = fmt.Sprintf("%s,%s", metadata.ConfigDriveID, metadata.MetadataID)
	cfg.BlockStorage.BSVersion = "auto"
	cfg.Networking.IPv6SupportDisabled = false
	cfg.Networking.PublicNetworkName = "public"

	return
}

// ReadConfig reads values from environment variables and the cloud.conf, prioritizing cloud-config
func ReadConfig(config io.Reader) (Config, error) {
	if config == nil {
		return Config{}, fmt.Errorf("no OpenStack cloud provider config file given")
	}

	cfg, _ := configFromEnv()

	// Set default values for config params
	cfg.BlockStorage.BSVersion = "auto"
	cfg.BlockStorage.TrustDevicePath = false
	cfg.BlockStorage.IgnoreVolumeAZ = false
	cfg.Metadata.SearchOrder = fmt.Sprintf("%s,%s", metadata.ConfigDriveID, metadata.MetadataID)
	cfg.Networking.IPv6SupportDisabled = false
	cfg.Networking.PublicNetworkName = "public"
	cfg.LoadBalancer.InternalLB = false

	err := gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if cfg.Global.UseClouds {
		if cfg.Global.CloudsFile != "" {
			os.Setenv("OS_CLIENT_CONFIG_FILE", cfg.Global.CloudsFile)
		}
		err = ReadClouds(&cfg)
		if err != nil {
			return Config{}, err
		}
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
	cfg.Global.Username = replaceEmpty(cfg.Global.Username, cloud.AuthInfo.Username)
	cfg.Global.UserID = replaceEmpty(cfg.Global.UserID, cloud.AuthInfo.UserID)
	cfg.Global.Password = replaceEmpty(cfg.Global.Password, cloud.AuthInfo.Password)
	cfg.Global.TenantID = replaceEmpty(cfg.Global.TenantID, cloud.AuthInfo.ProjectID)
	cfg.Global.TenantName = replaceEmpty(cfg.Global.TenantName, cloud.AuthInfo.ProjectName)
	cfg.Global.DomainID = replaceEmpty(cfg.Global.DomainID, cloud.AuthInfo.UserDomainID)
	cfg.Global.DomainName = replaceEmpty(cfg.Global.DomainName, cloud.AuthInfo.UserDomainName)
	cfg.Global.Region = replaceEmpty(cfg.Global.Region, cloud.RegionName)
	cfg.Global.CAFile = replaceEmpty(cfg.Global.CAFile, cloud.CACertFile)

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
	lbOpts := openstackOpts.lbOpts

	// if need to create health monitor for Neutron LB,
	// monitor-delay, monitor-timeout and monitor-max-retries should be set.
	emptyDuration := MyDuration{}
	if lbOpts.CreateMonitor {
		if lbOpts.MonitorDelay == emptyDuration {
			return fmt.Errorf("monitor-delay not set in cloud provider config")
		}
		if lbOpts.MonitorTimeout == emptyDuration {
			return fmt.Errorf("monitor-timeout not set in cloud provider config")
		}
		if lbOpts.MonitorMaxRetries == uint(0) {
			return fmt.Errorf("monitor-max-retries not set in cloud provider config")
		}
	}
	return checkMetadataSearchOrder(openstackOpts.metadataOpts.SearchOrder)
}

// NewOpenStack creates a new new instance of the openstack struct from a config struct
func NewOpenStack(cfg Config) (*OpenStack, error) {
	provider, err := openstack.NewClient(cfg.Global.AuthURL)
	if err != nil {
		return nil, err
	}
	if cfg.Global.CAFile != "" {
		roots, err := certutil.NewPool(cfg.Global.CAFile)
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
	} else if cfg.Global.TenantDomainID != "" || cfg.Global.TenantDomainName != "" {
		opts := cfg.toAuth3Options()
		err = openstack.AuthenticateV3(provider, &opts, gophercloud.EndpointOpts{})
	} else {
		err = openstack.Authenticate(provider, cfg.toAuthOptions())
	}

	if err != nil {
		return nil, err
	}

	emptyDuration := MyDuration{}
	if cfg.Metadata.RequestTimeout == emptyDuration {
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

func nodeAddresses(srv *servers.Server, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	addrs := []v1.NodeAddress{}

	type Address struct {
		IPType string `mapstructure:"OS-EXT-IPS:type"`
		Addr   string
	}

	var addresses map[string][]Address
	err := mapstructure.Decode(srv.Addresses, &addresses)
	if err != nil {
		return nil, err
	}

	for network, addrList := range addresses {
		for _, props := range addrList {
			var addressType v1.NodeAddressType
			if props.IPType == "floating" || network == networkingOpts.PublicNetworkName {
				addressType = v1.NodeExternalIP
			} else {
				if networkingOpts.InternalNetworkName == "" || network == networkingOpts.InternalNetworkName {
					addressType = v1.NodeInternalIP
				} else {
					klog.V(5).Infof("Node '%s' address '%s' ignored due to 'internal-network-name' option", srv.Name, props.Addr)
					continue
				}
			}

			isIPv6 := net.ParseIP(props.Addr).To4() == nil
			if !(isIPv6 && networkingOpts.IPv6SupportDisabled) {
				v1helper.AddToNodeAddresses(&addrs,
					v1.NodeAddress{
						Type:    addressType,
						Address: props.Addr,
					},
				)
			}
		}
	}

	// AccessIPs are usually duplicates of "public" addresses.
	if srv.AccessIPv4 != "" {
		v1helper.AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv4,
			},
		)
	}

	if srv.AccessIPv6 != "" && !networkingOpts.IPv6SupportDisabled {
		v1helper.AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv6,
			},
		)
	}

	if srv.Metadata[TypeHostName] != "" {
		v1helper.AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeHostName,
				Address: srv.Metadata[TypeHostName],
			},
		)
	}

	return addrs, nil
}

func getAddressesByName(client *gophercloud.ServiceClient, name types.NodeName, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	srv, err := getServerByName(client, name)
	if err != nil {
		return nil, err
	}

	return nodeAddresses(&srv.Server, networkingOpts)
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

// ScrubDNS filters DNS settings for pods.
func (os *OpenStack) ScrubDNS(nameServers, searches []string) ([]string, []string) {
	return nameServers, searches
}

// HasClusterID returns true if the cluster has a clusterID
func (os *OpenStack) HasClusterID() bool {
	return true
}

// LoadBalancer initializes a LbaasV2 object
func (os *OpenStack) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	klog.V(4).Info("openstack.LoadBalancer() called")

	if reflect.DeepEqual(os.lbOpts, LoadBalancerOpts{}) {
		klog.V(4).Info("LoadBalancer section is empty/not defined in cloud-config")
		return nil, false
	}

	network, err := os.NewNetworkV2()
	if err != nil {
		return nil, false
	}

	compute, err := os.NewComputeV2()
	if err != nil {
		return nil, false
	}

	lb, err := os.NewLoadBalancerV2()
	if err != nil {
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

func (os *OpenStack) volumeService(forceVersion string) (volumeService, error) {
	bsVersion := ""
	if forceVersion == "" {
		bsVersion = os.bsOpts.BSVersion
	} else {
		bsVersion = forceVersion
	}

	switch bsVersion {
	case "v1":
		sClient, err := os.NewBlockStorageV1()
		if err != nil {
			return nil, err
		}
		klog.V(3).Info("Using Blockstorage API V1")
		return &VolumesV1{sClient, os.bsOpts}, nil
	case "v2":
		sClient, err := os.NewBlockStorageV2()
		if err != nil {
			return nil, err
		}
		klog.V(3).Info("Using Blockstorage API V2")
		return &VolumesV2{sClient, os.bsOpts}, nil
	case "v3":
		sClient, err := os.NewBlockStorageV3()
		if err != nil {
			return nil, err
		}
		klog.V(3).Info("Using Blockstorage API V3")
		return &VolumesV3{sClient, os.bsOpts}, nil
	case "auto":
		// Currently kubernetes support Cinder v1 / Cinder v2 / Cinder v3.
		// Choose Cinder v3 firstly, if kubernetes can't initialize cinder v3 client, try to initialize cinder v2 client.
		// If kubernetes can't initialize cinder v2 client, try to initialize cinder v1 client.
		// Return appropriate message when kubernetes can't initialize them.
		if sClient, err := os.NewBlockStorageV3(); err == nil {
			klog.V(3).Info("Using Blockstorage API V3")
			return &VolumesV3{sClient, os.bsOpts}, nil
		}

		if sClient, err := os.NewBlockStorageV2(); err == nil {
			klog.V(3).Info("Using Blockstorage API V2")
			return &VolumesV2{sClient, os.bsOpts}, nil
		}

		if sClient, err := os.NewBlockStorageV1(); err == nil {
			klog.V(3).Info("Using Blockstorage API V1")
			return &VolumesV1{sClient, os.bsOpts}, nil
		}

		errTxt := "BlockStorage API version autodetection failed. " +
			"Please set it explicitly in cloud.conf in section [BlockStorage] with key `bs-version`"
		return nil, errors.New(errTxt)
	default:
		errTxt := fmt.Sprintf("Config error: unrecognised bs-version \"%v\"", os.bsOpts.BSVersion)
		return nil, errors.New(errTxt)
	}
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
