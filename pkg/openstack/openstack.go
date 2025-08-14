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
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/portsecurity"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/trunk_details"
	neutronports "github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/spf13/pflag"
	gcfg "gopkg.in/gcfg.v1"
	"k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/cloud-provider-openstack/pkg/util"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

const (
	// ProviderName is the name of the openstack provider
	ProviderName = "openstack"

	// TypeHostName is the name type of openstack instance
	TypeHostName   = "hostname"
	defaultTimeOut = 60 * time.Second
)

// userAgentData is used to add extra information to the gophercloud user-agent
var userAgentData []string

// supportedLBProvider map is used to define LoadBalancer providers that we support
var supportedLBProvider = []string{"amphora", "octavia", "ovn", "f5", "amphorav2"}

// supportedContainerStore map is used to define supported tls-container-ref store
var supportedContainerStore = []string{"barbican", "external"}

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

type PortWithTrunkDetails struct {
	neutronports.Port
	trunk_details.TrunkDetailsExt
}

type PortWithPortSecurity struct {
	neutronports.Port
	portsecurity.PortSecurityExt
}

// LoadBalancer is used for creating and maintaining load balancers
type LoadBalancer struct {
	secret        *gophercloud.ServiceClient
	network       *gophercloud.ServiceClient
	lb            *gophercloud.ServiceClient
	opts          LoadBalancerOpts
	kclient       kubernetes.Interface
	eventRecorder record.EventRecorder
}

// LoadBalancerOpts have the options to talk to Neutron LBaaSV2 or Octavia
type LoadBalancerOpts struct {
	Enabled                        bool                `gcfg:"enabled"`              // if false, disables the controller
	LBVersion                      string              `gcfg:"lb-version"`           // overrides autodetection. Only support v2.
	SubnetID                       string              `gcfg:"subnet-id"`            // overrides autodetection.
	MemberSubnetID                 string              `gcfg:"member-subnet-id"`     // overrides autodetection.
	NetworkID                      string              `gcfg:"network-id"`           // If specified, will create virtual ip from a subnet in network which has available IP addresses
	FloatingNetworkID              string              `gcfg:"floating-network-id"`  // If specified, will create floating ip for loadbalancer, or do not create floating ip.
	FloatingSubnetID               string              `gcfg:"floating-subnet-id"`   // If specified, will create floating ip for loadbalancer in this particular floating pool subnetwork.
	FloatingSubnet                 string              `gcfg:"floating-subnet"`      // If specified, will create floating ip for loadbalancer in one of the matching floating pool subnetworks.
	FloatingSubnetTags             string              `gcfg:"floating-subnet-tags"` // If specified, will create floating ip for loadbalancer in one of the matching floating pool subnetworks.
	LBClasses                      map[string]*LBClass // Predefined named Floating networks and subnets
	LBMethod                       string              `gcfg:"lb-method"` // default to ROUND_ROBIN.
	LBProvider                     string              `gcfg:"lb-provider"`
	CreateMonitor                  bool                `gcfg:"create-monitor"`
	MonitorDelay                   util.MyDuration     `gcfg:"monitor-delay"`
	MonitorTimeout                 util.MyDuration     `gcfg:"monitor-timeout"`
	MonitorMaxRetries              uint                `gcfg:"monitor-max-retries"`
	MonitorMaxRetriesDown          uint                `gcfg:"monitor-max-retries-down"`
	ManageSecurityGroups           bool                `gcfg:"manage-security-groups"`
	InternalLB                     bool                `gcfg:"internal-lb"`   // default false
	NodeSelector                   string              `gcfg:"node-selector"` // If specified, the loadbalancer members will be assined only from nodes list filtered by node-selector labels
	CascadeDelete                  bool                `gcfg:"cascade-delete"`
	FlavorID                       string              `gcfg:"flavor-id"`
	AvailabilityZone               string              `gcfg:"availability-zone"`
	EnableIngressHostname          bool                `gcfg:"enable-ingress-hostname"`            // Used with proxy protocol by adding a dns suffix to the load balancer IP address. Default false.
	IngressHostnameSuffix          string              `gcfg:"ingress-hostname-suffix"`            // Used with proxy protocol by adding a dns suffix to the load balancer IP address. Default nip.io.
	MaxSharedLB                    int                 `gcfg:"max-shared-lb"`                      //  Number of Services in maximum can share a single load balancer. Default 2
	ContainerStore                 string              `gcfg:"container-store"`                    // Used to specify the store of the tls-container-ref
	ProviderRequiresSerialAPICalls bool                `gcfg:"provider-requires-serial-api-calls"` // default false, the provider supports the "bulk update" API call
	// revive:disable:var-naming
	TlsContainerRef string `gcfg:"default-tls-container-ref"` //  reference to a tls container
	// revive:enable:var-naming
}

// LBClass defines the corresponding floating network, floating subnet or internal subnet ID
type LBClass struct {
	FloatingNetworkID  string `gcfg:"floating-network-id,omitempty"`
	FloatingSubnetID   string `gcfg:"floating-subnet-id,omitempty"`
	FloatingSubnet     string `gcfg:"floating-subnet,omitempty"`
	FloatingSubnetTags string `gcfg:"floating-subnet-tags,omitempty"`
	NetworkID          string `gcfg:"network-id,omitempty"`
	SubnetID           string `gcfg:"subnet-id,omitempty"`
	MemberSubnetID     string `gcfg:"member-subnet-id,omitempty"`
}

// NetworkingOpts is used for networking settings
type NetworkingOpts struct {
	IPv6SupportDisabled bool     `gcfg:"ipv6-support-disabled"`
	PublicNetworkName   []string `gcfg:"public-network-name"`
	InternalNetworkName []string `gcfg:"internal-network-name"`
	AddressSortOrder    string   `gcfg:"address-sort-order"`
}

// RouterOpts is used for Neutron routes
type RouterOpts struct {
	RouterID string `gcfg:"router-id"`
}

// OpenStack is an implementation of cloud provider Interface for OpenStack.
type OpenStack struct {
	provider              *gophercloud.ProviderClient
	epOpts                *gophercloud.EndpointOpts
	lbOpts                LoadBalancerOpts
	routeOpts             RouterOpts
	metadataOpts          metadata.Opts
	networkingOpts        NetworkingOpts
	kclient               kubernetes.Interface
	nodeInformer          coreinformers.NodeInformer
	nodeInformerHasSynced func() bool

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder
}

// Config is used to read and store information from the cloud configuration file
type Config struct {
	Global            client.AuthOpts
	LoadBalancer      LoadBalancerOpts
	LoadBalancerClass map[string]*LBClass
	Route             RouterOpts
	Metadata          metadata.Opts
	Networking        NetworkingOpts
}

func init() {
	metrics.RegisterMetrics("occm")

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

// Initialize passes a Kubernetes clientBuilder interface to the cloud provider
func (os *OpenStack) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
	clientset := clientBuilder.ClientOrDie("cloud-controller-manager")
	os.kclient = clientset
	os.eventBroadcaster = record.NewBroadcaster()
	os.eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: os.kclient.CoreV1().Events("")})
	os.eventRecorder = os.eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cloud-provider-openstack"})
}

// ReadConfig reads values from the cloud.conf
func ReadConfig(config io.Reader) (Config, error) {
	if config == nil {
		return Config{}, fmt.Errorf("no OpenStack cloud provider config file given")
	}
	var cfg Config

	// Set default values explicitly
	cfg.LoadBalancer.Enabled = true
	cfg.LoadBalancer.InternalLB = false
	cfg.LoadBalancer.NodeSelector = ""
	cfg.LoadBalancer.LBProvider = "amphora"
	cfg.LoadBalancer.LBMethod = "ROUND_ROBIN"
	cfg.LoadBalancer.CreateMonitor = false
	cfg.LoadBalancer.ManageSecurityGroups = false
	cfg.LoadBalancer.MonitorDelay = util.MyDuration{Duration: 5 * time.Second}
	cfg.LoadBalancer.MonitorTimeout = util.MyDuration{Duration: 3 * time.Second}
	cfg.LoadBalancer.MonitorMaxRetries = 1
	cfg.LoadBalancer.MonitorMaxRetriesDown = 3
	cfg.LoadBalancer.CascadeDelete = true
	cfg.LoadBalancer.EnableIngressHostname = false
	cfg.LoadBalancer.IngressHostnameSuffix = defaultProxyHostnameSuffix
	cfg.LoadBalancer.TlsContainerRef = ""
	cfg.LoadBalancer.ContainerStore = "barbican"
	cfg.LoadBalancer.MaxSharedLB = 2
	cfg.LoadBalancer.ProviderRequiresSerialAPICalls = false

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

	if !slices.Contains(supportedLBProvider, cfg.LoadBalancer.LBProvider) {
		klog.Warningf("Unsupported LoadBalancer Provider: %s", cfg.LoadBalancer.LBProvider)
	}

	if !slices.Contains(supportedContainerStore, cfg.LoadBalancer.ContainerStore) {
		klog.Warningf("Unsupported Container Store: %s", cfg.LoadBalancer.ContainerStore)
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

// Instances v1 is no longer supported
func (os *OpenStack) Instances() (cloudprovider.Instances, bool) {
	return nil, false
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
	if !os.lbOpts.Enabled {
		klog.V(4).Info("openstack.LoadBalancer() support for LoadBalancer controller is disabled")
		return nil, false
	}

	network, err := client.NewNetworkV2(os.provider, os.epOpts)
	if err != nil {
		klog.Fatalf("Failed to create an OpenStack Network client: %v", err)
		return nil, false
	}

	lb, err := client.NewLoadBalancerV2(os.provider, os.epOpts)
	if err != nil {
		klog.Fatalf("Failed to create an OpenStack LoadBalancer client: %v", err)
		return nil, false
	}

	// keymanager client is optional
	secret, err := client.NewKeyManagerV1(os.provider, os.epOpts)
	if err != nil {
		klog.Warningf("Failed to create an OpenStack Secret client: %v", err)
	}

	// LBaaS v1 is deprecated in the OpenStack Liberty release.
	// Currently kubernetes OpenStack cloud provider just support LBaaS v2.
	lbVersion := os.lbOpts.LBVersion
	if lbVersion != "" && lbVersion != "v2" {
		klog.Fatalf("Config error: currently only support LBaaS v2, unrecognised lb-version \"%v\"", lbVersion)
		return nil, false
	}

	klog.V(1).Info("Claiming to support LoadBalancer")

	return &LbaasV2{LoadBalancer{secret, network, lb, os.lbOpts, os.kclient, os.eventRecorder}}, true
}

// Zones indicates that we support zones
// DEPRECATED: Zones is deprecated in favor of retrieving zone/region information from InstancesV2.
func (os *OpenStack) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}

// Routes initializes routes support
func (os *OpenStack) Routes() (cloudprovider.Routes, bool) {
	klog.V(4).Info("openstack.Routes() called")

	ctx := context.TODO()
	network, err := client.NewNetworkV2(os.provider, os.epOpts)
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Network client: %v", err)
		return nil, false
	}

	netExts, err := openstackutil.GetNetworkExtensions(ctx, network)
	if err != nil {
		klog.Warningf("Failed to list neutron extensions: %v", err)
		return nil, false
	}

	if !netExts["extraroute"] && !netExts["extraroute-atomic"] {
		klog.V(3).Info("Neutron extraroute extension not found, required for Routes support")
		return nil, false
	}

	r, err := NewRoutes(os, network, netExts["extraroute-atomic"], netExts["allowed-address-pairs"])
	if err != nil {
		klog.Warningf("Error initialising Routes support: %v", err)
		return nil, false
	}

	if netExts["extraroute-atomic"] {
		klog.V(1).Info("Claiming to support Routes with atomic updates")
	} else {
		klog.V(1).Info("Claiming to support Routes")
	}

	return r, true
}

// SetInformers implements InformerUser interface by setting up informer-fed caches to
// leverage Kubernetes API for caching
func (os *OpenStack) SetInformers(informerFactory informers.SharedInformerFactory) {
	klog.V(1).Infof("Setting up informers for Cloud")
	os.nodeInformer = informerFactory.Core().V1().Nodes()
	os.nodeInformerHasSynced = os.nodeInformer.Informer().HasSynced
}
