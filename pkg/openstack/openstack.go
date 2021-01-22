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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/pflag"
	gcfg "gopkg.in/gcfg.v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/cloud-provider-openstack/pkg/util"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
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

// supportedLBProvider map is used to define LoadBalancer providers that we support
var supportedLBProvider = []string{"amphora", "octavia"}

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
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
	LBVersion            string              `gcfg:"lb-version"`           // overrides autodetection. Only support v2.
	UseOctavia           bool                `gcfg:"use-octavia"`          // uses Octavia V2 service catalog endpoint
	SubnetID             string              `gcfg:"subnet-id"`            // overrides autodetection.
	NetworkID            string              `gcfg:"network-id"`           // If specified, will create virtual ip from a subnet in network which has available IP addresses
	FloatingNetworkID    string              `gcfg:"floating-network-id"`  // If specified, will create floating ip for loadbalancer, or do not create floating ip.
	FloatingSubnetID     string              `gcfg:"floating-subnet-id"`   // If specified, will create floating ip for loadbalancer in this particular floating pool subnetwork.
	FloatingSubnet       string              `gcfg:"floating-subnet"`      // If specified, will create floating ip for loadbalancer in one of the matching floating pool subnetworks.
	FloatingSubnetTags   string              `gcfg:"floating-subnet-tags"` // If specified, will create floating ip for loadbalancer in one of the matching floating pool subnetworks.
	LBClasses            map[string]*LBClass // Predefined named Floating networks and subnets
	LBMethod             string              `gcfg:"lb-method"` // default to ROUND_ROBIN.
	LBProvider           string              `gcfg:"lb-provider"`
	CreateMonitor        bool                `gcfg:"create-monitor"`
	MonitorDelay         util.MyDuration     `gcfg:"monitor-delay"`
	MonitorTimeout       util.MyDuration     `gcfg:"monitor-timeout"`
	MonitorMaxRetries    uint                `gcfg:"monitor-max-retries"`
	ManageSecurityGroups bool                `gcfg:"manage-security-groups"`
	NodeSecurityGroupIDs []string            // Do not specify, get it automatically when enable manage-security-groups. TODO(FengyunPan): move it into cache
	InternalLB           bool                `gcfg:"internal-lb"`    // default false
	CascadeDelete        bool                `gcfg:"cascade-delete"` // applicable only if use-octavia is set to True
	FlavorID             string              `gcfg:"flavor-id"`
	AvailabilityZone     string              `gcfg:"availability-zone"`
}

// LBClass defines the corresponding floating network, floating subnet or internal subnet ID
type LBClass struct {
	FloatingNetworkID string `gcfg:"floating-network-id,omitempty"`
	FloatingSubnetID  string `gcfg:"floating-subnet-id,omitempty"`
	FloatingSubnet    string `gcfg:"floating-subnet,omitempty"`
	FloatingSubnetTag string `gcfg:"floating-subnet-tag"`
	NetworkID         string `gcfg:"network-id,omitempty"`
	SubnetID          string `gcfg:"sunet-id,omitempty"`
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

type ServerAttributesExt struct {
	servers.Server
	availabilityzones.ServerAvailabilityZoneExt
}

// OpenStack is an implementation of cloud provider Interface for OpenStack.
type OpenStack struct {
	provider       *gophercloud.ProviderClient
	epOpts         *gophercloud.EndpointOpts
	lbOpts         LoadBalancerOpts
	bsOpts         BlockStorageOpts
	routeOpts      RouterOpts
	metadataOpts   metadata.MetadataOpts
	networkingOpts NetworkingOpts
	// InstanceID of the server where this OpenStack object is instantiated.
	localInstanceID string
}

// Config is used to read and store information from the cloud configuration file
type Config struct {
	Global            client.AuthOpts
	LoadBalancer      LoadBalancerOpts
	LoadBalancerClass map[string]*LBClass
	BlockStorage      BlockStorageOpts
	Route             RouterOpts
	Metadata          metadata.MetadataOpts
	Networking        NetworkingOpts
}

func init() {
	metrics.RegisterMetrics()

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
	cfg.LoadBalancer.MonitorDelay = util.MyDuration{Duration: 5 * time.Second}
	cfg.LoadBalancer.MonitorTimeout = util.MyDuration{Duration: 3 * time.Second}
	cfg.LoadBalancer.MonitorMaxRetries = 1
	cfg.LoadBalancer.CascadeDelete = true

	err := gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if err != nil {
		return Config{}, err
	}

	klog.V(5).Infof("Config, loaded from the config file:")
	client.LogCfg(cfg.Global)

	if cfg.Global.UseClouds {
		if cfg.Global.CloudsFile != "" {
			os.Setenv("OS_CLIENT_CONFIG_FILE", cfg.Global.CloudsFile)
		}
		err = client.ReadClouds(&cfg.Global)
		if err != nil {
			return Config{}, err
		}
		klog.V(5).Infof("Config, loaded from the %s:", cfg.Global.CloudsFile)
		client.LogCfg(cfg.Global)
	}
	// Set the default values for search order if not set
	if cfg.Metadata.SearchOrder == "" {
		cfg.Metadata.SearchOrder = fmt.Sprintf("%s,%s", metadata.ConfigDriveID, metadata.MetadataID)
	}

	if !util.Contains(supportedLBProvider, cfg.LoadBalancer.LBProvider) {
		klog.Warningf("Unsupported LoadBalancer Provider: %s", cfg.LoadBalancer.LBProvider)
	}

	return cfg, err
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
	return metadata.CheckMetadataSearchOrder(openstackOpts.metadataOpts.SearchOrder)
}

// NewOpenStack creates a new new instance of the openstack struct from a config struct
func NewOpenStack(cfg Config) (*OpenStack, error) {
	provider, err := client.NewOpenStackClient(&cfg.Global, "openstack-cloud-controller-manager", userAgentData...)
	if err != nil {
		return nil, err
	}

	if cfg.Metadata.RequestTimeout == (util.MyDuration{}) {
		cfg.Metadata.RequestTimeout.Duration = time.Duration(defaultTimeOut)
	}
	provider.HTTPClient.Timeout = cfg.Metadata.RequestTimeout.Duration

	os := OpenStack{
		provider: provider,
		epOpts: &gophercloud.EndpointOpts{
			Region:       cfg.Global.Region,
			Availability: cfg.Global.EndpointType,
		},
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
	client, err := client.NewComputeV2(os.provider, os.epOpts)
	var nodeName types.NodeName
	if err != nil {
		return nodeName, err
	}

	mc := metrics.NewMetricContext("server", "get")
	server, err := servers.Get(client, instanceID).Extract()
	if mc.ObserveRequest(err) != nil {
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
	mc := metrics.NewMetricContext("server", "list")
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
	return mc.ObserveRequest(err)
}

func getServerByName(client *gophercloud.ServiceClient, name types.NodeName) (*ServerAttributesExt, error) {
	opts := servers.ListOpts{
		Name: fmt.Sprintf("^%s$", regexp.QuoteMeta(mapNodeNameToServerName(name))),
	}

	var s []ServerAttributesExt
	serverList := make([]ServerAttributesExt, 0, 1)

	mc := metrics.NewMetricContext("server", "list")
	pager := servers.List(client, opts)

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
	if mc.ObserveRequest(err) != nil {
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

	mc := metrics.NewMetricContext("server_os_interface", "list")
	pager := attachinterfaces.List(client, serviceID)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		s, err := attachinterfaces.ExtractInterfaces(page)
		if err != nil {
			return false, err
		}
		interfaces = append(interfaces, s...)
		return true, nil
	})
	if mc.ObserveRequest(err) != nil {
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

	network, err := client.NewNetworkV2(os.provider, os.epOpts)
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Network client: %v", err)
		return nil, false
	}

	compute, err := client.NewComputeV2(os.provider, os.epOpts)
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Compute client: %v", err)
		return nil, false
	}

	lb, err := client.NewLoadBalancerV2(os.provider, os.epOpts, os.lbOpts.UseOctavia)
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
		Region:        os.epOpts.Region,
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

	compute, err := client.NewComputeV2(os.provider, os.epOpts)
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	var serverWithAttributesExt ServerAttributesExt
	mc := metrics.NewMetricContext("server", "get")
	err = servers.Get(compute, instanceID).ExtractInto(&serverWithAttributesExt)
	if mc.ObserveRequest(err) != nil {
		return cloudprovider.Zone{}, err
	}

	zone := cloudprovider.Zone{
		FailureDomain: serverWithAttributesExt.AvailabilityZone,
		Region:        os.epOpts.Region,
	}
	klog.V(4).Infof("The instance %s in zone %v", serverWithAttributesExt.Name, zone)
	return zone, nil
}

// GetZoneByNodeName implements Zones.GetZoneByNodeName
// This is particularly useful in external cloud providers where the kubelet
// does not initialize node data.
func (os *OpenStack) GetZoneByNodeName(ctx context.Context, nodeName types.NodeName) (cloudprovider.Zone, error) {
	compute, err := client.NewComputeV2(os.provider, os.epOpts)
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
		Region:        os.epOpts.Region,
	}
	klog.V(4).Infof("The instance %s in zone %v", srv.Name, zone)
	return zone, nil
}

// Routes initializes routes support
func (os *OpenStack) Routes() (cloudprovider.Routes, bool) {
	klog.V(4).Info("openstack.Routes() called")

	network, err := client.NewNetworkV2(os.provider, os.epOpts)
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

	compute, err := client.NewComputeV2(os.provider, os.epOpts)
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
