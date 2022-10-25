/*
Copyright 2016 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/keymanager/v1/containers"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	v2monitors "github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/monitors"
	v2pools "github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
	neutrontags "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	neutronports "github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	"github.com/gophercloud/gophercloud/pagination"
	secgroups "github.com/gophercloud/utils/openstack/networking/v2/extensions/security/groups"
	"gopkg.in/godo.v2/glob"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"

	"k8s.io/cloud-provider-openstack/pkg/metrics"
	cpoutil "k8s.io/cloud-provider-openstack/pkg/util"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	netsets "k8s.io/cloud-provider-openstack/pkg/util/net/sets"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

// Note: when creating a new Loadbalancer (VM), it can take some time before it is ready for use,
// this timeout is used for waiting until the Loadbalancer provisioning status goes to ACTIVE state.
const (
	servicePrefix                       = "kube_service_"
	defaultLoadBalancerSourceRangesIPv4 = "0.0.0.0/0"
	defaultLoadBalancerSourceRangesIPv6 = "::/0"
	activeStatus                        = "ACTIVE"
	annotationXForwardedFor             = "X-Forwarded-For"

	ServiceAnnotationLoadBalancerInternal             = "service.beta.kubernetes.io/openstack-internal-load-balancer"
	ServiceAnnotationLoadBalancerConnLimit            = "loadbalancer.openstack.org/connection-limit"
	ServiceAnnotationLoadBalancerFloatingNetworkID    = "loadbalancer.openstack.org/floating-network-id"
	ServiceAnnotationLoadBalancerFloatingSubnet       = "loadbalancer.openstack.org/floating-subnet"
	ServiceAnnotationLoadBalancerFloatingSubnetID     = "loadbalancer.openstack.org/floating-subnet-id"
	ServiceAnnotationLoadBalancerFloatingSubnetTags   = "loadbalancer.openstack.org/floating-subnet-tags"
	ServiceAnnotationLoadBalancerClass                = "loadbalancer.openstack.org/class"
	ServiceAnnotationLoadBalancerKeepFloatingIP       = "loadbalancer.openstack.org/keep-floatingip"
	ServiceAnnotationLoadBalancerPortID               = "loadbalancer.openstack.org/port-id"
	ServiceAnnotationLoadBalancerProxyEnabled         = "loadbalancer.openstack.org/proxy-protocol"
	ServiceAnnotationLoadBalancerSubnetID             = "loadbalancer.openstack.org/subnet-id"
	ServiceAnnotationLoadBalancerNetworkID            = "loadbalancer.openstack.org/network-id"
	ServiceAnnotationLoadBalancerMemberSubnetID       = "loadbalancer.openstack.org/member-subnet-id"
	ServiceAnnotationLoadBalancerTimeoutClientData    = "loadbalancer.openstack.org/timeout-client-data"
	ServiceAnnotationLoadBalancerTimeoutMemberConnect = "loadbalancer.openstack.org/timeout-member-connect"
	ServiceAnnotationLoadBalancerTimeoutMemberData    = "loadbalancer.openstack.org/timeout-member-data"
	ServiceAnnotationLoadBalancerTimeoutTCPInspect    = "loadbalancer.openstack.org/timeout-tcp-inspect"
	ServiceAnnotationLoadBalancerXForwardedFor        = "loadbalancer.openstack.org/x-forwarded-for"
	ServiceAnnotationLoadBalancerFlavorID             = "loadbalancer.openstack.org/flavor-id"
	ServiceAnnotationLoadBalancerAvailabilityZone     = "loadbalancer.openstack.org/availability-zone"
	// ServiceAnnotationLoadBalancerEnableHealthMonitor defines whether to create health monitor for the load balancer
	// pool, if not specified, use 'create-monitor' config. The health monitor can be created or deleted dynamically.
	ServiceAnnotationLoadBalancerEnableHealthMonitor     = "loadbalancer.openstack.org/enable-health-monitor"
	ServiceAnnotationLoadBalancerHealthMonitorDelay      = "loadbalancer.openstack.org/health-monitor-delay"
	ServiceAnnotationLoadBalancerHealthMonitorTimeout    = "loadbalancer.openstack.org/health-monitor-timeout"
	ServiceAnnotationLoadBalancerHealthMonitorMaxRetries = "loadbalancer.openstack.org/health-monitor-max-retries"
	ServiceAnnotationLoadBalancerLoadbalancerHostname    = "loadbalancer.openstack.org/hostname"
	// revive:disable:var-naming
	ServiceAnnotationTlsContainerRef = "loadbalancer.openstack.org/default-tls-container-ref"
	// revive:enable:var-naming
	// See https://nip.io
	defaultProxyHostnameSuffix      = "nip.io"
	ServiceAnnotationLoadBalancerID = "loadbalancer.openstack.org/load-balancer-id"
)

// LbaasV2 is a LoadBalancer implementation based on Octavia
type LbaasV2 struct {
	LoadBalancer
}

// floatingSubnetSpec contains the specification of the public subnet to use for
// a public network. If given it may either describe the subnet id or
// a subnet name pattern for the subnet to use. If a pattern is given
// the first subnet matching the name pattern with an allocatable floating ip
// will be selected.
type floatingSubnetSpec struct {
	subnetID   string
	subnet     string
	subnetTags string
}

// TweakSubNetListOpsFunction is used to modify List Options for subnets
type TweakSubNetListOpsFunction func(*subnets.ListOpts)

// matcher matches a subnet
type matcher func(subnet *subnets.Subnet) bool

type servicePatcher struct {
	kclient kubernetes.Interface
	base    *corev1.Service
	updated *corev1.Service
}

var _ cloudprovider.LoadBalancer = &LbaasV2{}

// negate returns a negated matches for a given one
func negate(f matcher) matcher { return func(s *subnets.Subnet) bool { return !f(s) } }

func andMatcher(a, b matcher) matcher {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(s *subnets.Subnet) bool {
		return a(s) && b(s)
	}
}

// reexpNameMatcher creates a subnet matcher matching a subnet by name for a given regexp.
func regexpNameMatcher(r *regexp.Regexp) matcher {
	return func(s *subnets.Subnet) bool { return r.FindString(s.Name) == s.Name }
}

// subnetNameMatcher creates a subnet matcher matching a subnet by name for a given glob
// or regexp
func subnetNameMatcher(pat string) (matcher, error) {
	// try to create floating IP in matching subnets
	var match matcher
	not := false
	if strings.HasPrefix(pat, "!") {
		not = true
		pat = pat[1:]
	}
	if strings.HasPrefix(pat, "~") {
		rexp, err := regexp.Compile(pat[1:])
		if err != nil {
			return nil, fmt.Errorf("invalid subnet regexp pattern %q: %s", pat[1:], err)
		}
		match = regexpNameMatcher(rexp)
	} else {
		match = regexpNameMatcher(glob.Globexp(pat))
	}
	if not {
		match = negate(match)
	}
	return match, nil
}

// subnetTagMatcher matches a subnet by a given tag spec
func subnetTagMatcher(tags string) matcher {
	// try to create floating IP in matching subnets
	var match matcher

	list, not, all := tagList(tags)

	match = func(s *subnets.Subnet) bool {
		for _, tag := range list {
			found := false
			for _, t := range s.Tags {
				if t == tag {
					found = true
					break
				}
			}
			if found {
				if !all {
					return !not
				}
			} else {
				if all {
					return not
				}
			}
		}
		return not != all
	}
	return match
}

func (s *floatingSubnetSpec) Configured() bool {
	if s != nil && (s.subnetID != "" || s.MatcherConfigured()) {
		return true
	}
	return false
}

func (s *floatingSubnetSpec) ListSubnetsForNetwork(lbaas *LbaasV2, networkID string) ([]subnets.Subnet, error) {
	matcher, err := s.Matcher(false)
	if err != nil {
		return nil, err
	}
	list, err := lbaas.listSubnetsForNetwork(networkID, s.tweakListOpts)
	if err != nil {
		return nil, err
	}
	if matcher == nil {
		return list, nil
	}

	// filter subnets according to spec
	var foundSubnets []subnets.Subnet
	for _, subnet := range list {
		if matcher(&subnet) {
			foundSubnets = append(foundSubnets, subnet)
		}
	}
	return foundSubnets, nil
}

// tweakListOpts can be used to optimize a subnet list query for the
// actually described subnet filter
func (s *floatingSubnetSpec) tweakListOpts(opts *subnets.ListOpts) {
	if s.subnetTags != "" {
		list, not, all := tagList(s.subnetTags)
		tags := strings.Join(list, ",")
		if all {
			if not {
				opts.NotTagsAny = tags // at least one tag must be missing
			} else {
				opts.Tags = tags // all tags must be present
			}
		} else {
			if not {
				opts.NotTags = tags // none of the tags are present
			} else {
				opts.TagsAny = tags // at least one tag is present
			}
		}
	}
}

func (s *floatingSubnetSpec) MatcherConfigured() bool {
	if s != nil && s.subnetID == "" && (s.subnet != "" || s.subnetTags != "") {
		return true
	}
	return false
}

func addField(s, name, value string) string {
	if value == "" {
		return s
	}
	if s == "" {
		s += ", "
	}
	return fmt.Sprintf("%s%s: %q", s, name, value)
}

func (s *floatingSubnetSpec) String() string {
	if s == nil || (s.subnetID == "" && s.subnet == "" && s.subnetTags == "") {
		return "<none>"
	}
	pat := addField("", "subnetID", s.subnetID)
	pat = addField(pat, "pattern", s.subnet)
	return addField(pat, "tags", s.subnetTags)
}

func (s *floatingSubnetSpec) Matcher(tag bool) (matcher, error) {
	if !s.MatcherConfigured() {
		return nil, nil
	}
	var match matcher
	var err error
	if s.subnet != "" {
		match, err = subnetNameMatcher(s.subnet)
		if err != nil {
			return nil, err
		}
	}
	if tag && s.subnetTags != "" {
		match = andMatcher(match, subnetTagMatcher(s.subnetTags))
	}
	if match == nil {
		match = func(s *subnets.Subnet) bool { return true }
	}
	return match, nil
}

func tagList(tags string) ([]string, bool, bool) {
	not := strings.HasPrefix(tags, "!")
	if not {
		tags = tags[1:]
	}
	all := strings.HasPrefix(tags, "&")
	if all {
		tags = tags[1:]
	}
	list := strings.Split(tags, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}
	return list, not, all
}

// serviceConfig contains configurations for creating a Service.
type serviceConfig struct {
	internal                bool
	connLimit               int
	configClassName         string
	lbNetworkID             string
	lbSubnetID              string
	lbMemberSubnetID        string
	lbPublicNetworkID       string
	lbPublicSubnetSpec      *floatingSubnetSpec
	keepClientIP            bool
	enableProxyProtocol     bool
	timeoutClientData       int
	timeoutMemberConnect    int
	timeoutMemberData       int
	timeoutTCPInspect       int
	allowedCIDR             []string
	enableMonitor           bool
	flavorID                string
	availabilityZone        string
	tlsContainerRef         string
	lbID                    string
	lbName                  string
	supportLBTags           bool
	healthCheckNodePort     int
	healthMonitorDelay      int
	healthMonitorTimeout    int
	healthMonitorMaxRetries int
	preferredIPFamily       corev1.IPFamily // preferred (the first) IP family indicated in service's `spec.ipFamilies`
}

type listenerKey struct {
	Protocol listeners.Protocol
	Port     int
}

// getLoadbalancerByName get the load balancer which is in valid status by the given name/legacy name.
func getLoadbalancerByName(client *gophercloud.ServiceClient, name string, legacyName string) (*loadbalancers.LoadBalancer, error) {
	var validLBs []loadbalancers.LoadBalancer

	opts := loadbalancers.ListOpts{
		Name: name,
	}
	allLoadbalancers, err := openstackutil.GetLoadBalancers(client, opts)
	if err != nil {
		return nil, err
	}

	if len(allLoadbalancers) == 0 {
		if len(legacyName) > 0 {
			// Backoff to get load balnacer by legacy name.
			opts := loadbalancers.ListOpts{
				Name: legacyName,
			}
			allLoadbalancers, err = openstackutil.GetLoadBalancers(client, opts)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, cpoerrors.ErrNotFound
		}
	}

	for _, lb := range allLoadbalancers {
		// All the ProvisioningStatus could be found here https://developer.openstack.org/api-ref/load-balancer/v2/index.html#provisioning-status-codes
		if lb.ProvisioningStatus != "DELETED" && lb.ProvisioningStatus != "PENDING_DELETE" {
			validLBs = append(validLBs, lb)
		}
	}

	if len(validLBs) > 1 {
		return nil, cpoerrors.ErrMultipleResults
	}
	if len(validLBs) == 0 {
		return nil, cpoerrors.ErrNotFound
	}

	return &validLBs[0], nil
}

// Check if a member exists for node
func memberExists(members []v2pools.Member, addr string, port int) bool {
	for _, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			return true
		}
	}

	return false
}

func popListener(existingListeners []listeners.Listener, id string) []listeners.Listener {
	newListeners := []listeners.Listener{}
	for _, existingListener := range existingListeners {
		if existingListener.ID != id {
			newListeners = append(newListeners, existingListener)
		}
	}
	return newListeners
}

func popMember(members []v2pools.Member, addr string, port int) []v2pools.Member {
	for i, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			members[i] = members[len(members)-1]
			members = members[:len(members)-1]
		}
	}

	return members
}

func getSecurityGroupName(service *corev1.Service) string {
	securityGroupName := fmt.Sprintf("lb-sg-%s-%s-%s", service.UID, service.Namespace, service.Name)
	//OpenStack requires that the name of a security group is shorter than 255 bytes.
	if len(securityGroupName) > 255 {
		securityGroupName = securityGroupName[:255]
	}

	return securityGroupName
}

func getSecurityGroupRules(client *gophercloud.ServiceClient, opts rules.ListOpts) ([]rules.SecGroupRule, error) {
	var securityRules []rules.SecGroupRule

	mc := metrics.NewMetricContext("security_group_rule", "list")
	pager := rules.List(client, opts)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		ruleList, err := rules.ExtractRules(page)
		if err != nil {
			return false, err
		}
		securityRules = append(securityRules, ruleList...)
		return true, nil
	})

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return securityRules, nil
}

func toRuleProtocol(protocol corev1.Protocol) rules.RuleProtocol {
	switch protocol {
	case corev1.ProtocolTCP:
		return rules.ProtocolTCP
	case corev1.ProtocolUDP:
		return rules.ProtocolUDP
	default:
		return rules.RuleProtocol(strings.ToLower(string(protocol)))
	}
}

func getListenerProtocol(protocol corev1.Protocol, svcConf *serviceConfig) listeners.Protocol {
	// Make neutron-lbaas code work
	if svcConf != nil {
		if svcConf.tlsContainerRef != "" {
			return listeners.ProtocolTerminatedHTTPS
		} else if svcConf.keepClientIP {
			return listeners.ProtocolHTTP
		}
	}

	switch protocol {
	case corev1.ProtocolTCP:
		return listeners.ProtocolTCP
	case corev1.ProtocolUDP:
		return listeners.ProtocolUDP
	default:
		return listeners.Protocol(protocol)
	}
}

func createNodeSecurityGroup(client *gophercloud.ServiceClient, nodeSecurityGroupID string, port int, protocol corev1.Protocol, lbSecGroup string) error {
	v4NodeSecGroupRuleCreateOpts := rules.CreateOpts{
		Direction:     rules.DirIngress,
		PortRangeMax:  port,
		PortRangeMin:  port,
		Protocol:      toRuleProtocol(protocol),
		RemoteGroupID: lbSecGroup,
		SecGroupID:    nodeSecurityGroupID,
		EtherType:     rules.EtherType4,
	}

	v6NodeSecGroupRuleCreateOpts := rules.CreateOpts{
		Direction:     rules.DirIngress,
		PortRangeMax:  port,
		PortRangeMin:  port,
		Protocol:      toRuleProtocol(protocol),
		RemoteGroupID: lbSecGroup,
		SecGroupID:    nodeSecurityGroupID,
		EtherType:     rules.EtherType6,
	}

	mc := metrics.NewMetricContext("security_group_rule", "create")
	_, err := rules.Create(client, v4NodeSecGroupRuleCreateOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		return err
	}

	mc = metrics.NewMetricContext("security_group_rule", "create")
	_, err = rules.Create(client, v6NodeSecGroupRuleCreateOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		return err
	}
	return nil
}

// This function is deprecated.
func (lbaas *LbaasV2) createLoadBalancer(service *corev1.Service, name, clusterName string, lbClass *LBClass, internalAnnotation bool, vipPort string) (*loadbalancers.LoadBalancer, error) {
	createOpts := loadbalancers.CreateOpts{
		Name:        name,
		Description: fmt.Sprintf("Kubernetes external service %s/%s from cluster %s", service.Namespace, service.Name, clusterName),
		Provider:    lbaas.opts.LBProvider,
	}

	if vipPort != "" {
		createOpts.VipPortID = vipPort
	} else {
		if lbClass != nil && lbClass.SubnetID != "" {
			createOpts.VipSubnetID = lbClass.SubnetID
		} else {
			createOpts.VipSubnetID = lbaas.opts.SubnetID
		}

		if lbClass != nil && lbClass.NetworkID != "" {
			createOpts.VipNetworkID = lbClass.NetworkID
		} else if lbaas.opts.NetworkID != "" {
			createOpts.VipNetworkID = lbaas.opts.NetworkID
		} else {
			klog.V(4).Infof("network-id parameter not passed, it will be inferred from subnet-id")
		}
	}

	loadBalancerIP := service.Spec.LoadBalancerIP
	if loadBalancerIP != "" && internalAnnotation {
		createOpts.VipAddress = loadBalancerIP
	}

	mc := metrics.NewMetricContext("loadbalancer", "create")
	loadbalancer, err := loadbalancers.Create(lbaas.lb, createOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, fmt.Errorf("error creating loadbalancer %v: %v", createOpts, err)
	}

	// when NetworkID is specified, subnet will be selected by the backend for allocating virtual IP
	if (lbClass != nil && lbClass.NetworkID != "") || lbaas.opts.NetworkID != "" {
		lbaas.opts.SubnetID = loadbalancer.VipSubnetID
	}

	return loadbalancer, nil
}

func (lbaas *LbaasV2) createFullyPopulatedOctaviaLoadBalancer(name, clusterName string, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) (*loadbalancers.LoadBalancer, error) {
	createOpts := loadbalancers.CreateOpts{
		Name:        name,
		Description: fmt.Sprintf("Kubernetes external service %s/%s from cluster %s", service.Namespace, service.Name, clusterName),
		Provider:    lbaas.opts.LBProvider,
	}

	if svcConf.supportLBTags {
		createOpts.Tags = []string{svcConf.lbName}
	}

	if svcConf.flavorID != "" {
		createOpts.FlavorID = svcConf.flavorID
	}

	if svcConf.availabilityZone != "" {
		createOpts.AvailabilityZone = svcConf.availabilityZone
	}

	vipPort := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerPortID, "")
	lbClass := lbaas.opts.LBClasses[svcConf.configClassName]

	if vipPort != "" {
		createOpts.VipPortID = vipPort
	} else {
		if lbClass != nil && lbClass.SubnetID != "" {
			createOpts.VipSubnetID = lbClass.SubnetID
		} else {
			createOpts.VipSubnetID = svcConf.lbSubnetID
		}

		if lbClass != nil && lbClass.NetworkID != "" {
			createOpts.VipNetworkID = lbClass.NetworkID
		} else if svcConf.lbNetworkID != "" {
			createOpts.VipNetworkID = svcConf.lbNetworkID
		} else {
			klog.V(4).Infof("network-id parameter not passed, it will be inferred from subnet-id")
		}
	}

	// For external load balancer, the LoadBalancerIP is a public IP address.
	loadBalancerIP := service.Spec.LoadBalancerIP
	if loadBalancerIP != "" && svcConf.internal {
		createOpts.VipAddress = loadBalancerIP
	}

	for portIndex, port := range service.Spec.Ports {
		listenerCreateOpt := lbaas.buildListenerCreateOpt(port, svcConf)
		listenerCreateOpt.Name = cutString(fmt.Sprintf("listener_%d_%s", portIndex, name))
		members, newMembers, err := lbaas.buildBatchUpdateMemberOpts(port, nodes, svcConf)
		if err != nil {
			return nil, err
		}
		poolCreateOpt := lbaas.buildPoolCreateOpt(string(listenerCreateOpt.Protocol), service, svcConf)
		poolCreateOpt.Members = members
		// Pool name must be provided to create fully populated loadbalancer
		poolCreateOpt.Name = cutString(fmt.Sprintf("pool_%d_%s", portIndex, name))
		var withHealthMonitor string
		if svcConf.enableMonitor {
			opts := lbaas.buildMonitorCreateOpts(svcConf, port)
			opts.Name = cutString(fmt.Sprintf("monitor_%d_%s", port.Port, name))
			poolCreateOpt.Monitor = &opts
			withHealthMonitor = " with healthmonitor"
		}

		listenerCreateOpt.DefaultPool = &poolCreateOpt
		createOpts.Listeners = append(createOpts.Listeners, listenerCreateOpt)
		klog.V(2).Infof("Loadbalancer %s: adding pool%s using protocol %s with %d members", name, withHealthMonitor, poolCreateOpt.Protocol, len(newMembers))
	}

	mc := metrics.NewMetricContext("loadbalancer", "create")
	loadbalancer, err := loadbalancers.Create(lbaas.lb, createOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		var printObj interface{} = createOpts
		if opts, err := json.Marshal(createOpts); err == nil {
			printObj = string(opts)
		}
		return nil, fmt.Errorf("error creating loadbalancer %v: %v", printObj, err)
	}

	// In case subnet ID is not configured
	if svcConf.lbMemberSubnetID == "" {
		svcConf.lbMemberSubnetID = loadbalancer.VipSubnetID
	}

	if err := openstackutil.WaitLoadbalancerActive(lbaas.lb, loadbalancer.ID); err != nil {
		return nil, err
	}

	return loadbalancer, nil
}

// GetLoadBalancer returns whether the specified load balancer exists and its status
func (lbaas *LbaasV2) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (*corev1.LoadBalancerStatus, bool, error) {
	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.getLoadBalancerLegacyName(ctx, clusterName, service)
	lbID := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")
	var loadbalancer *loadbalancers.LoadBalancer
	var err error

	if lbID != "" {
		loadbalancer, err = openstackutil.GetLoadbalancerByID(lbaas.lb, lbID)
	} else {
		loadbalancer, err = getLoadbalancerByName(lbaas.lb, name, legacyName)
	}
	if err != nil && cpoerrors.IsNotFound(err) {
		return nil, false, nil
	}
	if loadbalancer == nil {
		return nil, false, err
	}

	status := &corev1.LoadBalancerStatus{}
	portID := loadbalancer.VipPortID
	if portID != "" {
		floatIP, err := openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
		if err != nil {
			return nil, false, fmt.Errorf("failed when trying to get floating IP for port %s: %v", portID, err)
		}
		if floatIP != nil {
			status.Ingress = []corev1.LoadBalancerIngress{{IP: floatIP.FloatingIP}}
		} else {
			status.Ingress = []corev1.LoadBalancerIngress{{IP: loadbalancer.VipAddress}}
		}
	}

	return status, true, nil
}

// GetLoadBalancerName returns the constructed load balancer name.
func (lbaas *LbaasV2) GetLoadBalancerName(_ context.Context, clusterName string, service *corev1.Service) string {
	name := fmt.Sprintf("%s%s_%s_%s", servicePrefix, clusterName, service.Namespace, service.Name)
	return cutString(name)
}

// getLoadBalancerLegacyName returns the legacy load balancer name for backward compatibility.
func (lbaas *LbaasV2) getLoadBalancerLegacyName(_ context.Context, _ string, service *corev1.Service) string {
	return cloudprovider.DefaultLoadBalancerName(service)
}

// cutString makes sure the string length doesn't exceed 255, which is usually the maximum string length in OpenStack.
func cutString(original string) string {
	ret := original
	if len(original) > 255 {
		ret = original[:255]
	}
	return ret
}

// The LB needs to be configured with instance addresses on the same
// subnet as the LB (aka opts.SubnetID). Currently, we're just
// guessing that the node's InternalIP is the right address.
// In case no InternalIP can be found, ExternalIP is tried.
// If neither InternalIP nor ExternalIP can be found an error is
// returned.
// If preferredIPFamily is specified, only address of the specified IP family can be returned.
func nodeAddressForLB(node *corev1.Node, preferredIPFamily corev1.IPFamily) (string, error) {
	addrs := node.Status.Addresses
	if len(addrs) == 0 {
		return "", cpoerrors.ErrNoAddressFound
	}

	allowedAddrTypes := []corev1.NodeAddressType{corev1.NodeInternalIP, corev1.NodeExternalIP}

	for _, allowedAddrType := range allowedAddrTypes {
		for _, addr := range addrs {
			if addr.Type == allowedAddrType {
				switch preferredIPFamily {
				case corev1.IPv4Protocol:
					if netutils.IsIPv4String(addr.Address) {
						return addr.Address, nil
					}
				case corev1.IPv6Protocol:
					if netutils.IsIPv6String(addr.Address) {
						return addr.Address, nil
					}
				default:
					return addr.Address, nil
				}
			}
		}
	}

	return "", cpoerrors.ErrNoAddressFound
}

// getStringFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's value or a specified defaultSetting
func getStringFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting string) string {
	klog.V(4).Infof("getStringFromServiceAnnotation(%s/%s, %v, %v)", service.Namespace, service.Name, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		//if there is an annotation for this setting, set the "setting" var to it
		// annotationValue can be empty, it is working as designed
		// it makes possible for instance provisioning loadbalancer without floatingip
		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, annotationValue)
		return annotationValue
	}
	//if there is no annotation, set "settings" var to the value from cloud config
	if defaultSetting != "" {
		klog.V(4).Infof("Could not find a Service Annotation; falling back on cloud-config setting: %v = %v", annotationKey, defaultSetting)
	}
	return defaultSetting
}

// getIntFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's integer value or a specified defaultSetting
func getIntFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting int) int {
	klog.V(4).Infof("getIntFromServiceAnnotation(%s/%s, %v, %v)", service.Namespace, service.Name, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		returnValue, err := strconv.Atoi(annotationValue)
		if err != nil {
			klog.Warningf("Could not parse int value from %q, failing back to default %s = %v, %v", annotationValue, annotationKey, defaultSetting, err)
			return defaultSetting
		}

		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, annotationValue)
		return returnValue
	}
	klog.V(4).Infof("Could not find a Service Annotation; falling back to default setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting
}

// getBoolFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's boolean value or a specified defaultSetting
func getBoolFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting bool) bool {
	klog.V(4).Infof("getBoolFromServiceAnnotation(%s/%s, %v, %v)", service.Namespace, service.Name, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		returnValue := false
		switch annotationValue {
		case "true":
			returnValue = true
		case "false":
			returnValue = false
		default:
			returnValue = defaultSetting
		}

		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, returnValue)
		return returnValue
	}
	klog.V(4).Infof("Could not find a Service Annotation; falling back to default setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting
}

// getSubnetIDForLB returns subnet-id for a specific node
func getSubnetIDForLB(compute *gophercloud.ServiceClient, node corev1.Node, preferredIPFamily corev1.IPFamily) (string, error) {
	ipAddress, err := nodeAddressForLB(&node, preferredIPFamily)
	if err != nil {
		return "", err
	}

	instanceID := node.Spec.ProviderID
	if ind := strings.LastIndex(instanceID, "/"); ind >= 0 {
		instanceID = instanceID[(ind + 1):]
	}

	interfaces, err := getAttachedInterfacesByID(compute, instanceID)
	if err != nil {
		return "", err
	}

	for _, intf := range interfaces {
		for _, fixedIP := range intf.FixedIPs {
			if fixedIP.IPAddress == ipAddress {
				return fixedIP.SubnetID, nil
			}
		}
	}

	return "", cpoerrors.ErrNotFound
}

// applyNodeSecurityGroupIDForLB associates the security group with all the ports on the nodes.
func applyNodeSecurityGroupIDForLB(compute *gophercloud.ServiceClient, network *gophercloud.ServiceClient, nodes []*corev1.Node, sg string) error {
	for _, node := range nodes {
		nodeName := types.NodeName(node.Name)
		srv, err := getServerByName(compute, nodeName)
		if err != nil {
			return err
		}

		listOpts := neutronports.ListOpts{DeviceID: srv.ID}
		allPorts, err := openstackutil.GetPorts(network, listOpts)
		if err != nil {
			return err
		}

		for _, port := range allPorts {
			// If the Security Group is already present on the port, skip it.
			// As soon as this only supports Go 1.18, this can be replaces by
			// slices.Contains.
			if func() bool {
				for _, currentSG := range port.SecurityGroups {
					if currentSG == sg {
						return true
					}
				}
				return false
			}() {
				continue
			}

			newSGs := append(port.SecurityGroups, sg)
			updateOpts := neutronports.UpdateOpts{SecurityGroups: &newSGs}
			mc := metrics.NewMetricContext("port", "update")
			res := neutronports.Update(network, port.ID, updateOpts)
			if mc.ObserveRequest(res.Err) != nil {
				return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
			}
			// Add the security group ID as a tag to the port in order to find all these ports when removing the security group.
			mc = metrics.NewMetricContext("port_tag", "add")
			err := neutrontags.Add(network, "ports", port.ID, sg).ExtractErr()
			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("failed to add tag %s to port %s: %v", sg, port.ID, err)
			}
		}
	}

	return nil
}

// disassociateSecurityGroupForLB removes the given security group from the ports
func disassociateSecurityGroupForLB(network *gophercloud.ServiceClient, sg string) error {
	// Find all the ports that have the security group associated.
	listOpts := neutronports.ListOpts{TagsAny: sg}
	allPorts, err := openstackutil.GetPorts(network, listOpts)
	if err != nil {
		return err
	}

	// Disassocate security group and remove the tag.
	for _, port := range allPorts {
		existingSGs := sets.NewString()
		for _, sgID := range port.SecurityGroups {
			existingSGs.Insert(sgID)
		}
		existingSGs.Delete(sg)

		// Update port security groups
		newSGs := existingSGs.List()
		updateOpts := neutronports.UpdateOpts{SecurityGroups: &newSGs}
		mc := metrics.NewMetricContext("port", "update")
		res := neutronports.Update(network, port.ID, updateOpts)
		if mc.ObserveRequest(res.Err) != nil {
			return fmt.Errorf("failed to update security group for port %s: %v", port.ID, res.Err)
		}
		// Remove the security group ID tag from the port.
		mc = metrics.NewMetricContext("port_tag", "delete")
		err := neutrontags.Delete(network, "ports", port.ID, sg).ExtractErr()
		if mc.ObserveRequest(err) != nil {
			return fmt.Errorf("failed to remove tag %s to port %s: %v", sg, port.ID, res.Err)
		}
	}

	return nil
}

// getNodeSecurityGroupIDForLB lists node-security-groups for specific nodes
func getNodeSecurityGroupIDForLB(compute *gophercloud.ServiceClient, network *gophercloud.ServiceClient, nodes []*corev1.Node) ([]string, error) {
	secGroupIDs := sets.NewString()

	for _, node := range nodes {
		nodeName := types.NodeName(node.Name)
		srv, err := getServerByName(compute, nodeName)
		if err != nil {
			return []string{}, err
		}

		// Get the security groups of all the ports on the worker nodes. In the future, we could filter the ports by some way.
		// case 0: node1:SG1  node2:SG1  return SG1
		// case 1: node1:SG1  node2:SG2  return SG1,SG2
		// case 2: node1:SG1,SG2  node2:SG3,SG4  return SG1,SG2,SG3,SG4
		// case 3: node1:SG1,SG2  node2:SG2,SG3  return SG1,SG2,SG3
		listOpts := neutronports.ListOpts{DeviceID: srv.ID}
		allPorts, err := openstackutil.GetPorts(network, listOpts)
		if err != nil {
			return []string{}, err
		}

		for _, port := range allPorts {
			for _, sg := range port.SecurityGroups {
				secGroupIDs.Insert(sg)
			}
		}
	}

	return secGroupIDs.List(), nil
}

// isSecurityGroupNotFound return true while 'err' is object of gophercloud.ErrResourceNotFound
func isSecurityGroupNotFound(err error) bool {
	errType := reflect.TypeOf(err).String()
	errTypeSlice := strings.Split(errType, ".")
	errTypeValue := ""
	if len(errTypeSlice) != 0 {
		errTypeValue = errTypeSlice[len(errTypeSlice)-1]
	}
	if errTypeValue == "ErrResourceNotFound" {
		return true
	}

	return false
}

// deleteListeners deletes listeners and its default pool.
func (lbaas *LbaasV2) deleteListeners(lbID string, listenerList []listeners.Listener) error {
	for _, listener := range listenerList {
		klog.InfoS("Deleting listener", "listenerID", listener.ID, "lbID", lbID)

		pool, err := openstackutil.GetPoolByListener(lbaas.lb, lbID, listener.ID)
		if err != nil && err != cpoerrors.ErrNotFound {
			return fmt.Errorf("error getting pool for obsolete listener %s: %v", listener.ID, err)
		}
		if pool != nil {
			klog.InfoS("Deleting pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)
			// Delete pool automatically deletes all its members.
			if err := openstackutil.DeletePool(lbaas.lb, pool.ID, lbID); err != nil {
				return err
			}
			klog.InfoS("Deleted pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)
		}

		if err := openstackutil.DeleteListener(lbaas.lb, listener.ID, lbID); err != nil {
			return err
		}
		klog.InfoS("Deleted listener", "listenerID", listener.ID, "lbID", lbID)
	}

	return nil
}

// deleteOctaviaListeners is used not simply for deleting listeners but only deleting listeners used to be created by the Service.
func (lbaas *LbaasV2) deleteOctaviaListeners(lbID string, listenerList []listeners.Listener, isLBOwner bool, lbName string) error {
	for _, listener := range listenerList {
		// If the listener was created by this Service before or after supporting shared LB.
		if (isLBOwner && len(listener.Tags) == 0) || cpoutil.Contains(listener.Tags, lbName) {
			klog.InfoS("Deleting listener", "listenerID", listener.ID, "lbID", lbID)

			pool, err := openstackutil.GetPoolByListener(lbaas.lb, lbID, listener.ID)
			if err != nil && err != cpoerrors.ErrNotFound {
				return fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
			}
			if pool != nil {
				klog.InfoS("Deleting pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)

				// Delete pool automatically deletes all its members.
				if err := openstackutil.DeletePool(lbaas.lb, pool.ID, lbID); err != nil {
					return err
				}
				klog.InfoS("Deleted pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)
			}

			if err := openstackutil.DeleteListener(lbaas.lb, listener.ID, lbID); err != nil {
				return err
			}

			klog.InfoS("Deleted listener", "listenerID", listener.ID, "lbID", lbID)
		} else {
			// This listener is created and managed by others, shouldn't delete.
			klog.V(4).InfoS("Ignoring the listener used by others", "listenerID", listener.ID, "loadbalancerID", lbID, "tags", listener.Tags)
			continue
		}
	}

	return nil
}

func (lbaas *LbaasV2) createFloatingIP(msg string, floatIPOpts floatingips.CreateOpts) (*floatingips.FloatingIP, error) {
	klog.V(4).Infof("%s floating ip with opts %+v", msg, floatIPOpts)
	mc := metrics.NewMetricContext("floating_ip", "create")
	floatIP, err := floatingips.Create(lbaas.network, floatIPOpts).Extract()
	err = PreserveGopherError(err)
	if mc.ObserveRequest(err) != nil {
		return floatIP, fmt.Errorf("error creating LB floatingip: %s", err)
	}
	return floatIP, err
}

// Priority of choosing VIP port floating IP:
// 1. The floating IP that is already attached to the VIP port.
// 2. Floating IP specified in Spec.LoadBalancerIP
// 3. Create a new one
func (lbaas *LbaasV2) getServiceAddress(clusterName string, service *corev1.Service, lb *loadbalancers.LoadBalancer, svcConf *serviceConfig) (string, error) {
	if svcConf.internal {
		return lb.VipAddress, nil
	}

	var floatIP *floatingips.FloatingIP
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)

	// first attempt: fetch floating IP attached to load balancer's VIP port
	portID := lb.VipPortID
	floatIP, err := openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
	if err != nil {
		return "", fmt.Errorf("failed when getting floating IP for port %s: %v", portID, err)
	}
	klog.V(4).Infof("Found floating ip %v by loadbalancer port id %q", floatIP, portID)

	// second attempt: fetch floating IP specified in service Spec.LoadBalancerIP
	// if found, associate floating IP with loadbalancer's VIP port
	loadBalancerIP := service.Spec.LoadBalancerIP
	if floatIP == nil && loadBalancerIP != "" {
		opts := floatingips.ListOpts{
			FloatingIP: loadBalancerIP,
		}
		existingIPs, err := openstackutil.GetFloatingIPs(lbaas.network, opts)
		if err != nil {
			return "", fmt.Errorf("failed when trying to get existing floating IP %s, error: %v", loadBalancerIP, err)
		}
		klog.V(4).Infof("Found floating ips %v by loadbalancer ip %q", existingIPs, loadBalancerIP)

		if len(existingIPs) > 0 {
			floatingip := existingIPs[0]
			if len(floatingip.PortID) == 0 {
				floatUpdateOpts := floatingips.UpdateOpts{
					PortID: &portID,
				}
				klog.V(4).Infof("Attaching floating ip %q to loadbalancer port %q", floatingip.FloatingIP, portID)
				mc := metrics.NewMetricContext("floating_ip", "update")
				floatIP, err = floatingips.Update(lbaas.network, floatingip.ID, floatUpdateOpts).Extract()
				if mc.ObserveRequest(err) != nil {
					return "", fmt.Errorf("error updating LB floatingip %+v: %v", floatUpdateOpts, err)
				}
			} else {
				return "", fmt.Errorf("floating IP %s is not available", loadBalancerIP)
			}
		}
	}

	// third attempt: create a new floating IP
	if floatIP == nil {
		if svcConf.lbPublicNetworkID != "" {
			klog.V(2).Infof("Creating floating IP %s for loadbalancer %s", loadBalancerIP, lb.ID)

			floatIPOpts := floatingips.CreateOpts{
				FloatingNetworkID: svcConf.lbPublicNetworkID,
				PortID:            portID,
				Description:       fmt.Sprintf("Floating IP for Kubernetes external service %s from cluster %s", serviceName, clusterName),
			}

			if loadBalancerIP == "" && svcConf.lbPublicSubnetSpec.MatcherConfigured() {
				var foundSubnet subnets.Subnet
				// tweak list options for tags
				foundSubnets, err := svcConf.lbPublicSubnetSpec.ListSubnetsForNetwork(lbaas, svcConf.lbPublicNetworkID)
				if err != nil {
					return "", err
				}
				if len(foundSubnets) == 0 {
					return "", fmt.Errorf("no subnet matching %s found for network %s",
						svcConf.lbPublicSubnetSpec, svcConf.lbPublicNetworkID)
				}

				// try to create floating IP in matching subnets (tags already filtered by list options)
				klog.V(4).Infof("found %d subnets matching %s for network %s", len(foundSubnets),
					svcConf.lbPublicSubnetSpec, svcConf.lbPublicNetworkID)
				for _, subnet := range foundSubnets {
					floatIPOpts.SubnetID = subnet.ID
					floatIP, err = lbaas.createFloatingIP(fmt.Sprintf("Trying subnet %s for creating", subnet.Name), floatIPOpts)
					if err == nil {
						foundSubnet = subnet
						break
					}
					klog.V(2).Infof("cannot use subnet %s: %s", subnet.Name, err)
				}
				if err != nil {
					return "", fmt.Errorf("no free subnet matching %q found for network %s (last error %s)",
						svcConf.lbPublicSubnetSpec, svcConf.lbPublicNetworkID, err)
				}
				klog.V(2).Infof("Successfully created floating IP %s for loadbalancer %s on subnet %s(%s)", floatIP.FloatingIP, lb.ID, foundSubnet.Name, foundSubnet.ID)
			} else {
				if svcConf.lbPublicSubnetSpec != nil {
					floatIPOpts.SubnetID = svcConf.lbPublicSubnetSpec.subnetID
				}
				floatIPOpts.FloatingIP = loadBalancerIP
				floatIP, err = lbaas.createFloatingIP("Creating", floatIPOpts)
				if err != nil {
					return "", err
				}
				klog.V(2).Infof("Successfully created floating IP %s for loadbalancer %s", floatIP.FloatingIP, lb.ID)
			}

		} else {
			klog.Warningf("Floating network configuration not provided for Service %s, forcing to ensure an internal load balancer service", serviceName)
		}
	}

	if floatIP != nil {
		return floatIP.FloatingIP, nil
	}

	return lb.VipAddress, nil
}

func (lbaas *LbaasV2) ensureOctaviaHealthMonitor(lbID string, name string, pool *v2pools.Pool, port corev1.ServicePort, svcConf *serviceConfig) error {
	monitorID := pool.MonitorID

	if monitorID != "" {
		monitor, err := openstackutil.GetHealthMonitor(lbaas.lb, monitorID)
		if err != nil {
			return err
		}
		//Recreate health monitor with correct protocol if externalTrafficPolicy was changed
		if (svcConf.healthCheckNodePort > 0 && monitor.Type != "HTTP") ||
			(svcConf.healthCheckNodePort == 0 && monitor.Type == "HTTP") {
			klog.InfoS("Recreating health monitor for the pool", "pool", pool.ID, "oldMonitor", monitorID)
			if err := openstackutil.DeleteHealthMonitor(lbaas.lb, monitorID, lbID); err != nil {
				return err
			}
			monitorID = ""
		}
		if svcConf.healthMonitorDelay != monitor.Delay || svcConf.healthMonitorTimeout != monitor.Timeout || svcConf.healthMonitorMaxRetries != monitor.MaxRetries {
			updateOpts := v2monitors.UpdateOpts{
				Delay:      svcConf.healthMonitorDelay,
				Timeout:    svcConf.healthMonitorTimeout,
				MaxRetries: svcConf.healthMonitorMaxRetries,
			}
			klog.Infof("Updating health monitor %s updateOpts %+v", monitorID, updateOpts)
			if err := openstackutil.UpdateHealthMonitor(lbaas.lb, monitorID, updateOpts); err != nil {
				return err
			}
		}
	}
	if monitorID == "" && svcConf.enableMonitor {
		klog.V(2).Infof("Creating monitor for pool %s", pool.ID)

		createOpts := lbaas.buildMonitorCreateOpts(svcConf, port)
		// Populate PoolID, attribute is omitted for consumption of the createOpts for fully populated Loadbalancer
		createOpts.PoolID = pool.ID
		createOpts.Name = name
		monitor, err := openstackutil.CreateHealthMonitor(lbaas.lb, createOpts, lbID)
		if err != nil {
			return err
		}
		monitorID = monitor.ID
		klog.Infof("Health monitor %s for pool %s created.", monitorID, pool.ID)
	} else if monitorID != "" && !svcConf.enableMonitor {
		klog.Infof("Deleting health monitor %s for pool %s", monitorID, pool.ID)

		if err := openstackutil.DeleteHealthMonitor(lbaas.lb, monitorID, lbID); err != nil {
			return err
		}
	}

	return nil
}

// buildMonitorCreateOpts returns a v2monitors.CreateOpts without PoolID for consumption of both, fully popuplated Loadbalancers and Monitors.
func (lbaas *LbaasV2) buildMonitorCreateOpts(svcConf *serviceConfig, port corev1.ServicePort) v2monitors.CreateOpts {
	opts := v2monitors.CreateOpts{
		Type:       string(port.Protocol),
		Delay:      svcConf.healthMonitorDelay,
		Timeout:    svcConf.healthMonitorTimeout,
		MaxRetries: svcConf.healthMonitorMaxRetries,
	}
	if port.Protocol == corev1.ProtocolUDP {
		opts.Type = "UDP-CONNECT"
	}
	if svcConf.healthCheckNodePort > 0 {
		setHTTPHealthMonitor := true
		if lbaas.opts.LBProvider == "ovn" {
			// ovn-octavia-provider doesn't support HTTP monitors at all. We got to avoid creating it with ovn.
			setHTTPHealthMonitor = false
		} else if opts.Type == "UDP-CONNECT" {
			// Older Octavia versions or OVN provider doesn't support HTTP monitors on UDP pools. We got to check if that's the case.
			setHTTPHealthMonitor = openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureHTTPMonitorsOnUDP, lbaas.opts.LBProvider)
		}

		if setHTTPHealthMonitor {
			opts.Type = "HTTP"
			opts.URLPath = "/healthz"
			opts.HTTPMethod = "GET"
			opts.ExpectedCodes = "200"
		}
	}
	return opts
}

// Make sure the pool is created for the Service, nodes are added as pool members.
func (lbaas *LbaasV2) ensureOctaviaPool(lbID string, name string, listener *listeners.Listener, service *corev1.Service, port corev1.ServicePort, nodes []*corev1.Node, svcConf *serviceConfig) (*v2pools.Pool, error) {
	pool, err := openstackutil.GetPoolByListener(lbaas.lb, lbID, listener.ID)
	if err != nil && err != cpoerrors.ErrNotFound {
		return nil, fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
	}

	// By default, use the protocol of the listener
	poolProto := v2pools.Protocol(listener.Protocol)
	if svcConf.enableProxyProtocol {
		poolProto = v2pools.ProtocolPROXY
	} else if (svcConf.keepClientIP || svcConf.tlsContainerRef != "") && poolProto != v2pools.ProtocolHTTP {
		poolProto = v2pools.ProtocolHTTP
	}

	// Delete the pool and its members if it already exists and has the wrong protocol
	if pool != nil && v2pools.Protocol(pool.Protocol) != poolProto {
		klog.InfoS("Deleting unused pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)

		// Delete pool automatically deletes all its members.
		if err := openstackutil.DeletePool(lbaas.lb, pool.ID, lbID); err != nil {
			return nil, err
		}
		pool = nil
	}

	if pool == nil {
		createOpt := lbaas.buildPoolCreateOpt(listener.Protocol, service, svcConf)
		createOpt.ListenerID = listener.ID
		createOpt.Name = name

		klog.InfoS("Creating pool", "listenerID", listener.ID, "protocol", createOpt.Protocol)
		pool, err = openstackutil.CreatePool(lbaas.lb, createOpt, lbID)
		if err != nil {
			return nil, err
		}
		klog.V(2).Infof("Pool %s created for listener %s", pool.ID, listener.ID)
	}

	curMembers := sets.NewString()
	poolMembers, err := openstackutil.GetMembersbyPool(lbaas.lb, pool.ID)
	if err != nil {
		klog.Errorf("failed to get members in the pool %s: %v", pool.ID, err)
	}
	for _, m := range poolMembers {
		curMembers.Insert(fmt.Sprintf("%s-%s-%d-%d", m.Name, m.Address, m.ProtocolPort, m.MonitorPort))
	}

	members, newMembers, err := lbaas.buildBatchUpdateMemberOpts(port, nodes, svcConf)
	if err != nil {
		return nil, err
	}

	if !curMembers.Equal(newMembers) {
		klog.V(2).Infof("Updating %d members for pool %s", len(members), pool.ID)
		if err := openstackutil.BatchUpdatePoolMembers(lbaas.lb, lbID, pool.ID, members); err != nil {
			return nil, err
		}
		klog.V(2).Infof("Successfully updated %d members for pool %s", len(members), pool.ID)
	}

	return pool, nil
}

func (lbaas *LbaasV2) buildPoolCreateOpt(listenerProtocol string, service *corev1.Service, svcConf *serviceConfig) v2pools.CreateOpts {
	// By default, use the protocol of the listener
	poolProto := v2pools.Protocol(listenerProtocol)
	if svcConf.enableProxyProtocol {
		poolProto = v2pools.ProtocolPROXY
	} else if (svcConf.keepClientIP || svcConf.tlsContainerRef != "") && poolProto != v2pools.ProtocolHTTP {
		if svcConf.keepClientIP && svcConf.tlsContainerRef != "" {
			klog.V(4).Infof("Forcing to use %q protocol for pool because annotations %q %q are set", v2pools.ProtocolHTTP, ServiceAnnotationLoadBalancerXForwardedFor, ServiceAnnotationTlsContainerRef)
		} else if svcConf.keepClientIP {
			klog.V(4).Infof("Forcing to use %q protocol for pool because annotation %q is set", v2pools.ProtocolHTTP, ServiceAnnotationLoadBalancerXForwardedFor)
		} else {
			klog.V(4).Infof("Forcing to use %q protocol for pool because annotations %q is set", v2pools.ProtocolHTTP, ServiceAnnotationTlsContainerRef)
		}
		poolProto = v2pools.ProtocolHTTP
	}

	affinity := service.Spec.SessionAffinity
	var persistence *v2pools.SessionPersistence
	switch affinity {
	case corev1.ServiceAffinityNone:
		persistence = nil
	case corev1.ServiceAffinityClientIP:
		persistence = &v2pools.SessionPersistence{Type: "SOURCE_IP"}
	}

	lbmethod := v2pools.LBMethod(lbaas.opts.LBMethod)
	return v2pools.CreateOpts{
		Protocol:    poolProto,
		LBMethod:    lbmethod,
		Persistence: persistence,
	}
}

// buildBatchUpdateMemberOpts returns v2pools.BatchUpdateMemberOpts array for Services and Nodes alongside a list of member names
func (lbaas *LbaasV2) buildBatchUpdateMemberOpts(port corev1.ServicePort, nodes []*corev1.Node, svcConf *serviceConfig) ([]v2pools.BatchUpdateMemberOpts, sets.String, error) {
	var members []v2pools.BatchUpdateMemberOpts
	newMembers := sets.NewString()

	for _, node := range nodes {
		addr, err := nodeAddressForLB(node, svcConf.preferredIPFamily)
		if err != nil {
			if err == cpoerrors.ErrNoAddressFound {
				// Node failure, do not create member
				klog.Warningf("Failed to get the address of node %s for creating member: %v", node.Name, err)
				continue
			} else {
				return nil, nil, fmt.Errorf("error getting address of node %s: %v", node.Name, err)
			}
		}

		member := v2pools.BatchUpdateMemberOpts{
			Address:      addr,
			ProtocolPort: int(port.NodePort),
			Name:         &node.Name,
			SubnetID:     &svcConf.lbMemberSubnetID,
		}
		if svcConf.healthCheckNodePort > 0 {
			useHealthCheckNodePort := true
			if lbaas.opts.LBProvider == "ovn" {
				// ovn-octavia-provider doesn't support HTTP monitors at all, if we have it we got to rely on NodePort
				// and UDP-CONNECT health monitor.
				useHealthCheckNodePort = false
			} else if port.Protocol == "UDP" {
				// Older Octavia versions doesn't support HTTP monitors on UDP pools. If we have one like that, we got
				// to rely on checking the NodePort instead.
				useHealthCheckNodePort = openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureHTTPMonitorsOnUDP, lbaas.opts.LBProvider)
			}
			if useHealthCheckNodePort {
				member.MonitorPort = &svcConf.healthCheckNodePort
			}
		}
		members = append(members, member)
		newMembers.Insert(fmt.Sprintf("%s-%s-%d-%d", node.Name, addr, member.ProtocolPort, svcConf.healthCheckNodePort))
	}
	return members, newMembers, nil
}

// Make sure the listener is created for Service
func (lbaas *LbaasV2) ensureOctaviaListener(lbID string, name string, curListenerMapping map[listenerKey]*listeners.Listener, port corev1.ServicePort, svcConf *serviceConfig, _ *corev1.Service) (*listeners.Listener, error) {
	listener, isPresent := curListenerMapping[listenerKey{
		Protocol: getListenerProtocol(port.Protocol, svcConf),
		Port:     int(port.Port),
	}]
	if !isPresent {
		listenerCreateOpt := lbaas.buildListenerCreateOpt(port, svcConf)
		listenerCreateOpt.LoadbalancerID = lbID
		listenerCreateOpt.Name = name

		klog.V(2).Infof("Creating listener for port %d using protocol %s", int(port.Port), listenerCreateOpt.Protocol)

		var err error
		listener, err = openstackutil.CreateListener(lbaas.lb, lbID, listenerCreateOpt)
		if err != nil {
			return nil, fmt.Errorf("failed to create listener for loadbalancer %s: %v", lbID, err)
		}

		klog.V(2).Infof("Listener %s created for loadbalancer %s", listener.ID, lbID)
	} else {
		listenerChanged := false
		updateOpts := listeners.UpdateOpts{}

		if svcConf.supportLBTags {
			if !cpoutil.Contains(listener.Tags, svcConf.lbName) {
				var newTags []string
				copy(newTags, listener.Tags)
				newTags = append(newTags, svcConf.lbName)
				updateOpts.Tags = &newTags
				listenerChanged = true
			}
		}

		if svcConf.connLimit != listener.ConnLimit {
			updateOpts.ConnLimit = &svcConf.connLimit
			listenerChanged = true
		}

		listenerKeepClientIP := listener.InsertHeaders[annotationXForwardedFor] == "true"
		if svcConf.keepClientIP != listenerKeepClientIP {
			updateOpts.InsertHeaders = &listener.InsertHeaders
			if svcConf.keepClientIP {
				if *updateOpts.InsertHeaders == nil {
					*updateOpts.InsertHeaders = make(map[string]string)
				}
				(*updateOpts.InsertHeaders)[annotationXForwardedFor] = "true"
			} else {
				delete(*updateOpts.InsertHeaders, annotationXForwardedFor)
			}
			listenerChanged = true
		}
		if svcConf.tlsContainerRef != listener.DefaultTlsContainerRef {
			updateOpts.DefaultTlsContainerRef = &svcConf.tlsContainerRef
			listenerChanged = true
		}
		if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureTimeout, lbaas.opts.LBProvider) {
			if svcConf.timeoutClientData != listener.TimeoutClientData {
				updateOpts.TimeoutClientData = &svcConf.timeoutClientData
				listenerChanged = true
			}
			if svcConf.timeoutMemberConnect != listener.TimeoutMemberConnect {
				updateOpts.TimeoutMemberConnect = &svcConf.timeoutMemberConnect
				listenerChanged = true
			}
			if svcConf.timeoutMemberData != listener.TimeoutMemberData {
				updateOpts.TimeoutMemberData = &svcConf.timeoutMemberData
				listenerChanged = true
			}
			if svcConf.timeoutTCPInspect != listener.TimeoutTCPInspect {
				updateOpts.TimeoutTCPInspect = &svcConf.timeoutTCPInspect
				listenerChanged = true
			}
		}
		if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureVIPACL, lbaas.opts.LBProvider) {
			if !cpoutil.StringListEqual(svcConf.allowedCIDR, listener.AllowedCIDRs) {
				updateOpts.AllowedCIDRs = &svcConf.allowedCIDR
				listenerChanged = true
			}
		}

		if listenerChanged {
			klog.InfoS("Updating listener", "listenerID", listener.ID, "lbID", lbID, "updateOpts", updateOpts)
			if err := openstackutil.UpdateListener(lbaas.lb, lbID, listener.ID, updateOpts); err != nil {
				return nil, fmt.Errorf("failed to update listener %s of loadbalancer %s: %v", listener.ID, lbID, err)
			}
			klog.InfoS("Updated listener", "listenerID", listener.ID, "lbID", lbID)
		}
	}

	return listener, nil
}

// buildListenerCreateOpt returns listeners.CreateOpts for a specific Service port and configuration
func (lbaas *LbaasV2) buildListenerCreateOpt(port corev1.ServicePort, svcConf *serviceConfig) listeners.CreateOpts {
	listenerProtocol := listeners.Protocol(port.Protocol)

	listenerCreateOpt := listeners.CreateOpts{
		Protocol:     listenerProtocol,
		ProtocolPort: int(port.Port),
		ConnLimit:    &svcConf.connLimit,
	}

	if svcConf.supportLBTags {
		listenerCreateOpt.Tags = []string{svcConf.lbName}
	}

	if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureTimeout, lbaas.opts.LBProvider) {
		listenerCreateOpt.TimeoutClientData = &svcConf.timeoutClientData
		listenerCreateOpt.TimeoutMemberConnect = &svcConf.timeoutMemberConnect
		listenerCreateOpt.TimeoutMemberData = &svcConf.timeoutMemberData
		listenerCreateOpt.TimeoutTCPInspect = &svcConf.timeoutTCPInspect
	}

	if svcConf.keepClientIP {
		listenerCreateOpt.InsertHeaders = map[string]string{annotationXForwardedFor: "true"}
	}

	if svcConf.tlsContainerRef != "" {
		listenerCreateOpt.DefaultTlsContainerRef = svcConf.tlsContainerRef
	}

	// protocol selection
	if svcConf.tlsContainerRef != "" && listenerCreateOpt.Protocol != listeners.ProtocolTerminatedHTTPS {
		klog.V(4).Infof("Forcing to use %q protocol for listener because %q annotation is set", listeners.ProtocolTerminatedHTTPS, ServiceAnnotationTlsContainerRef)
		listenerCreateOpt.Protocol = listeners.ProtocolTerminatedHTTPS
	} else if svcConf.keepClientIP && listenerCreateOpt.Protocol != listeners.ProtocolHTTP {
		klog.V(4).Infof("Forcing to use %q protocol for listener because %q annotation is set", listeners.ProtocolHTTP, ServiceAnnotationLoadBalancerXForwardedFor)
		listenerCreateOpt.Protocol = listeners.ProtocolHTTP
	}

	if len(svcConf.allowedCIDR) > 0 {
		listenerCreateOpt.AllowedCIDRs = svcConf.allowedCIDR
	}
	return listenerCreateOpt
}

// getMemberSubnetID gets the configured member-subnet-id from the different possible sources.
func (lbaas *LbaasV2) getMemberSubnetID(service *corev1.Service, svcConf *serviceConfig) (string, error) {
	// Get Member Subnet from Service Annotation
	memberSubnetIDAnnotation := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerMemberSubnetID, "")
	if memberSubnetIDAnnotation != "" {
		return memberSubnetIDAnnotation, nil
	}

	// Get Member Subnet from Config Class
	configClassName := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, "")
	if configClassName != "" {
		lbClass := lbaas.opts.LBClasses[configClassName]
		if lbClass == nil {
			return "", fmt.Errorf("invalid loadbalancer class %q", configClassName)
		}
		if lbClass.MemberSubnetID != "" {
			return lbClass.MemberSubnetID, nil
		}
	}

	// Get Member Subnet from Default Config
	if lbaas.opts.MemberSubnetID != "" {
		return lbaas.opts.MemberSubnetID, nil
	}

	return "", nil
}

// getSubnetID gets the configured subnet-id from the different possible sources.
func (lbaas *LbaasV2) getSubnetID(service *corev1.Service, svcConf *serviceConfig) (string, error) {
	// Get subnet from service annotation
	SubnetIDAnnotation := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerSubnetID, "")
	if SubnetIDAnnotation != "" {
		return SubnetIDAnnotation, nil
	}

	// Get subnet from config class
	configClassName := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, "")
	if configClassName != "" {
		lbClass := lbaas.opts.LBClasses[configClassName]
		if lbClass == nil {
			return "", fmt.Errorf("invalid loadbalancer class %q", configClassName)
		}
		if lbClass.SubnetID != "" {
			return lbClass.SubnetID, nil
		}
	}

	// Get subnet from Default Config
	if lbaas.opts.SubnetID != "" {
		return lbaas.opts.SubnetID, nil
	}

	return "", nil
}

// getNetworkID gets the configured network-id from the different possible sources.
func (lbaas *LbaasV2) getNetworkID(service *corev1.Service, svcConf *serviceConfig) (string, error) {
	// Get subnet from service annotation
	SubnetIDAnnotation := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerNetworkID, "")
	if SubnetIDAnnotation != "" {
		return SubnetIDAnnotation, nil
	}

	// Get subnet from config class
	configClassName := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, "")
	if configClassName != "" {
		lbClass := lbaas.opts.LBClasses[configClassName]
		if lbClass == nil {
			return "", fmt.Errorf("invalid loadbalancer class %q", configClassName)
		}
		if lbClass.NetworkID != "" {
			return lbClass.NetworkID, nil
		}
	}

	// Get subnet from Default Config
	if lbaas.opts.NetworkID != "" {
		return lbaas.opts.NetworkID, nil
	}

	return "", nil
}

func (lbaas *LbaasV2) checkServiceUpdate(service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) error {
	if len(service.Spec.Ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)

	if len(service.Spec.IPFamilies) > 0 {
		// Since OCCM does not support multiple load-balancers per service yet,
		// the first IP family will determine the IP family of the load-balancer
		svcConf.preferredIPFamily = service.Spec.IPFamilies[0]
	}

	svcConf.lbID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")
	svcConf.supportLBTags = openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureTags, lbaas.opts.LBProvider)

	// Find subnet ID for creating members
	memberSubnetID, err := lbaas.getMemberSubnetID(service, svcConf)
	if err != nil {
		return fmt.Errorf("unable to get member-subnet-id, %w", err)
	}
	if memberSubnetID != "" {
		svcConf.lbMemberSubnetID = memberSubnetID
	} else if lbaas.opts.SubnetID != "" {
		svcConf.lbMemberSubnetID = lbaas.opts.SubnetID
	} else {
		svcConf.configClassName = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, "")
		if svcConf.configClassName != "" {
			lbClass := lbaas.opts.LBClasses[svcConf.configClassName]
			if lbClass == nil {
				return fmt.Errorf("invalid loadbalancer class %q", svcConf.configClassName)
			}

			if lbClass.SubnetID != "" {
				svcConf.lbMemberSubnetID = lbClass.SubnetID
			}
		} else {
			svcConf.lbMemberSubnetID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerSubnetID, lbaas.opts.SubnetID)
			if len(svcConf.lbMemberSubnetID) == 0 && len(nodes) > 0 {
				subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0], svcConf.preferredIPFamily)
				if err != nil {
					return fmt.Errorf("no subnet-id found for service %s: %v", serviceName, err)
				}
				svcConf.lbMemberSubnetID = subnetID
			}
		}
	}

	// This affects the protocol of listener and pool
	keepClientIP := getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerXForwardedFor, false)
	useProxyProtocol := getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerProxyEnabled, false)
	if useProxyProtocol && keepClientIP {
		return fmt.Errorf("annotation %s and %s cannot be used together", ServiceAnnotationLoadBalancerProxyEnabled, ServiceAnnotationLoadBalancerXForwardedFor)
	}
	svcConf.keepClientIP = keepClientIP
	svcConf.enableProxyProtocol = useProxyProtocol

	svcConf.tlsContainerRef = getStringFromServiceAnnotation(service, ServiceAnnotationTlsContainerRef, lbaas.opts.TlsContainerRef)
	svcConf.enableMonitor = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerEnableHealthMonitor, lbaas.opts.CreateMonitor)
	if svcConf.enableMonitor && lbaas.opts.UseOctavia && service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal && service.Spec.HealthCheckNodePort > 0 {
		svcConf.healthCheckNodePort = int(service.Spec.HealthCheckNodePort)
	}
	svcConf.healthMonitorDelay = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorDelay, int(lbaas.opts.MonitorDelay.Duration.Seconds()))
	svcConf.healthMonitorTimeout = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorTimeout, int(lbaas.opts.MonitorTimeout.Duration.Seconds()))
	svcConf.healthMonitorMaxRetries = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorMaxRetries, int(lbaas.opts.MonitorMaxRetries))
	return nil
}

func (lbaas *LbaasV2) checkServiceDelete(service *corev1.Service, svcConf *serviceConfig) error {
	svcConf.lbID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")
	svcConf.supportLBTags = openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureTags, lbaas.opts.LBProvider)

	// This affects the protocol of listener and pool
	svcConf.keepClientIP = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerXForwardedFor, false)
	svcConf.enableProxyProtocol = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerProxyEnabled, false)
	svcConf.tlsContainerRef = getStringFromServiceAnnotation(service, ServiceAnnotationTlsContainerRef, lbaas.opts.TlsContainerRef)

	return nil
}

func (lbaas *LbaasV2) checkService(service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) error {
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)

	if len(nodes) == 0 {
		return fmt.Errorf("there are no available nodes for LoadBalancer service %s", serviceName)
	}
	ports := service.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no service ports provided")
	}

	if len(service.Spec.IPFamilies) > 0 {
		// Since OCCM does not support multiple load-balancers per service yet,
		// the first IP family will determine the IP family of the load-balancer
		svcConf.preferredIPFamily = service.Spec.IPFamilies[0]
	}

	svcConf.lbID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")
	svcConf.supportLBTags = openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureTags, lbaas.opts.LBProvider)

	// If in the config file internal-lb=true, user is not allowed to create external service.
	if lbaas.opts.InternalLB {
		if !getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerInternal, false) {
			klog.V(3).InfoS("Enforcing internal LB", "annotation", true, "config", false)
		}
		svcConf.internal = true
	} else if svcConf.preferredIPFamily == corev1.IPv6Protocol {
		// floating IPs are not supported in IPv6 networks
		svcConf.internal = true
	} else {
		svcConf.internal = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerInternal, lbaas.opts.InternalLB)
	}

	svcConf.tlsContainerRef = getStringFromServiceAnnotation(service, ServiceAnnotationTlsContainerRef, lbaas.opts.TlsContainerRef)
	if svcConf.tlsContainerRef != "" {
		if lbaas.secret == nil {
			return fmt.Errorf("failed to create a TLS Terminated loadbalancer because openstack keymanager client is not "+
				"initialized and default-tls-container-ref %q is set", svcConf.tlsContainerRef)
		}

		// check if container exists for 'barbican' container store
		// tls container ref has the format: https://{keymanager_host}/v1/containers/{uuid}
		if lbaas.opts.ContainerStore == "barbican" {
			slice := strings.Split(svcConf.tlsContainerRef, "/")
			containerID := slice[len(slice)-1]
			container, err := containers.Get(lbaas.secret, containerID).Extract()
			if err != nil {
				return fmt.Errorf("failed to get tls container %q: %v", svcConf.tlsContainerRef, err)
			}
			klog.V(4).Infof("Default TLS container %q found", container.ContainerRef)
		}
	}

	svcConf.connLimit = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerConnLimit, -1)

	lbNetworkID, err := lbaas.getNetworkID(service, svcConf)
	if err != nil {
		return fmt.Errorf("failed to get network id to create load balancer for service %s: %v", serviceName, err)
	}
	svcConf.lbNetworkID = lbNetworkID

	lbSubnetID, err := lbaas.getSubnetID(service, svcConf)
	if err != nil {
		return fmt.Errorf("failed to get subnet to create load balancer for service %s: %v", serviceName, err)
	}
	svcConf.lbSubnetID = lbSubnetID

	if lbaas.opts.SubnetID != "" {
		svcConf.lbMemberSubnetID = lbaas.opts.SubnetID
	} else {
		svcConf.lbMemberSubnetID = svcConf.lbSubnetID
	}
	if len(svcConf.lbNetworkID) == 0 && len(svcConf.lbSubnetID) == 0 {
		subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0], svcConf.preferredIPFamily)
		if err != nil {
			return fmt.Errorf("failed to get subnet to create load balancer for service %s: %v", serviceName, err)
		}
		svcConf.lbSubnetID = subnetID
		svcConf.lbMemberSubnetID = subnetID
	}

	// Override the specific member-subnet-id, if explictly configured.
	// Otherwise use subnet-id.
	memberSubnetID, err := lbaas.getMemberSubnetID(service, svcConf)
	if err != nil {
		return fmt.Errorf("unable to get member-subnet-id, %w", err)
	}
	if memberSubnetID != "" {
		svcConf.lbMemberSubnetID = memberSubnetID
	}

	if !svcConf.internal {
		var lbClass *LBClass
		var floatingNetworkID string
		var floatingSubnet floatingSubnetSpec

		klog.V(4).Infof("Ensure an external loadbalancer service")

		svcConf.configClassName = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, "")
		if svcConf.configClassName != "" {
			lbClass = lbaas.opts.LBClasses[svcConf.configClassName]
			if lbClass == nil {
				return fmt.Errorf("invalid loadbalancer class %q", svcConf.configClassName)
			}

			klog.V(4).Infof("Found loadbalancer class %q with %+v", svcConf.configClassName, lbClass)

			// Get floating network id and floating subnet id from loadbalancer class
			floatingNetworkID = lbClass.FloatingNetworkID
			floatingSubnet.subnetID = lbClass.FloatingSubnetID
			if floatingSubnet.subnetID == "" {
				floatingSubnet.subnet = lbClass.FloatingSubnet
				floatingSubnet.subnetTags = lbClass.FloatingSubnetTags
			}
		}

		if floatingNetworkID == "" {
			floatingNetworkID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingNetworkID, lbaas.opts.FloatingNetworkID)
			if floatingNetworkID == "" {
				var err error
				floatingNetworkID, err = openstackutil.GetFloatingNetworkID(lbaas.network)
				if err != nil {
					klog.Warningf("Failed to find floating-network-id for Service %s: %v", serviceName, err)
				}
			}
		}

		// apply defaults from CCM config
		if floatingNetworkID == "" {
			floatingNetworkID = lbaas.opts.FloatingNetworkID
		}

		if !floatingSubnet.Configured() {
			annos := floatingSubnetSpec{}
			annos.subnetID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingSubnetID, "")
			if annos.subnetID == "" {
				annos.subnet = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingSubnet, "")
				annos.subnetTags = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingSubnetTags, "")
			}
			if annos.Configured() {
				floatingSubnet = annos
			} else {
				floatingSubnet.subnetID = lbaas.opts.FloatingSubnetID
				if floatingSubnet.subnetID == "" {
					floatingSubnet.subnetTags = lbaas.opts.FloatingSubnetTags
					floatingSubnet.subnet = lbaas.opts.FloatingSubnet
				}
			}
		}

		// check subnets belongs to network
		if floatingNetworkID != "" && floatingSubnet.subnetID != "" {
			mc := metrics.NewMetricContext("subnet", "get")
			subnet, err := subnets.Get(lbaas.network, floatingSubnet.subnetID).Extract()
			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("failed to find subnet %q: %v", floatingSubnet.subnetID, err)
			}

			if subnet.NetworkID != floatingNetworkID {
				return fmt.Errorf("floating IP subnet %q doesn't belong to the network %q", floatingSubnet.subnetID, subnet.NetworkID)
			}
		}

		svcConf.lbPublicNetworkID = floatingNetworkID
		if floatingSubnet.Configured() {
			klog.V(4).Infof("Using subnet spec %+v for %s", floatingSubnet, serviceName)
			svcConf.lbPublicSubnetSpec = &floatingSubnet
		} else {
			klog.V(4).Infof("no subnet spec found for %s", serviceName)
		}
	} else {
		klog.V(4).Infof("Ensure an internal loadbalancer service.")
	}

	keepClientIP := getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerXForwardedFor, false)
	useProxyProtocol := getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerProxyEnabled, false)
	if useProxyProtocol && keepClientIP {
		return fmt.Errorf("annotation %s and %s cannot be used together", ServiceAnnotationLoadBalancerProxyEnabled, ServiceAnnotationLoadBalancerXForwardedFor)
	}
	svcConf.keepClientIP = keepClientIP
	svcConf.enableProxyProtocol = useProxyProtocol

	if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureTimeout, lbaas.opts.LBProvider) {
		svcConf.timeoutClientData = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutClientData, 50000)
		svcConf.timeoutMemberConnect = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutMemberConnect, 5000)
		svcConf.timeoutMemberData = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutMemberData, 50000)
		svcConf.timeoutTCPInspect = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutTCPInspect, 0)
	}

	var listenerAllowedCIDRs []string
	sourceRanges, err := GetLoadBalancerSourceRanges(service, svcConf.preferredIPFamily)
	if err != nil {
		return fmt.Errorf("failed to get source ranges for loadbalancer service %s: %v", serviceName, err)
	}
	if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureVIPACL, lbaas.opts.LBProvider) {
		klog.V(4).Info("LoadBalancerSourceRanges is suppported")
		listenerAllowedCIDRs = sourceRanges.StringSlice()
	} else {
		klog.Warning("LoadBalancerSourceRanges is ignored")
	}
	svcConf.allowedCIDR = listenerAllowedCIDRs

	if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureFlavors, lbaas.opts.LBProvider) {
		svcConf.flavorID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFlavorID, lbaas.opts.FlavorID)
	}

	availabilityZone := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerAvailabilityZone, lbaas.opts.AvailabilityZone)
	if openstackutil.IsOctaviaFeatureSupported(lbaas.lb, openstackutil.OctaviaFeatureAvailabilityZones, lbaas.opts.LBProvider) {
		svcConf.availabilityZone = availabilityZone
	} else if availabilityZone != "" {
		klog.Warning("LoadBalancer Availability Zones aren't supported. Please, upgrade Octavia API to version 2.14 or later (Ussuri release) to use them")
	}

	svcConf.enableMonitor = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerEnableHealthMonitor, lbaas.opts.CreateMonitor)
	if svcConf.enableMonitor && lbaas.opts.UseOctavia && service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal && service.Spec.HealthCheckNodePort > 0 {
		svcConf.healthCheckNodePort = int(service.Spec.HealthCheckNodePort)
	}
	svcConf.healthMonitorDelay = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorDelay, int(lbaas.opts.MonitorDelay.Duration.Seconds()))
	svcConf.healthMonitorTimeout = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorTimeout, int(lbaas.opts.MonitorTimeout.Duration.Seconds()))
	svcConf.healthMonitorMaxRetries = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorMaxRetries, int(lbaas.opts.MonitorMaxRetries))
	return nil
}

// checkListenerPorts checks if there is conflict for ports.
func (lbaas *LbaasV2) checkListenerPorts(service *corev1.Service, curListenerMapping map[listenerKey]*listeners.Listener, isLBOwner bool, lbName string) error {
	for _, svcPort := range service.Spec.Ports {
		key := listenerKey{Protocol: listeners.Protocol(svcPort.Protocol), Port: int(svcPort.Port)}

		if listener, isPresent := curListenerMapping[key]; isPresent {
			// The listener is used by this Service if LB name is in the tags, or
			// the listener was created by this Service.
			if cpoutil.Contains(listener.Tags, lbName) || (len(listener.Tags) == 0 && isLBOwner) {
				continue
			} else {
				return fmt.Errorf("the listener port %d already exists", svcPort.Port)
			}
		}
	}

	return nil
}

func newServicePatcher(kclient kubernetes.Interface, base *corev1.Service) servicePatcher {
	return servicePatcher{
		kclient: kclient,
		base:    base.DeepCopy(),
		updated: base,
	}
}

// Patch will submit a patch request for the Service unless the updated service
// reference contains the same set of annotations as the base copied during
// servicePatcher initialization.
func (sp *servicePatcher) Patch(ctx context.Context, err error) error {
	if reflect.DeepEqual(sp.base.Annotations, sp.updated.Annotations) {
		return err
	}
	perr := cpoutil.PatchService(ctx, sp.kclient, sp.base, sp.updated)
	return utilerrors.NewAggregate([]error{err, perr})
}

func (lbaas *LbaasV2) updateServiceAnnotation(service *corev1.Service, annotName, annotValue string) {
	if service.ObjectMeta.Annotations == nil {
		service.ObjectMeta.Annotations = map[string]string{}
	}
	service.ObjectMeta.Annotations[annotName] = annotValue
}

// createLoadBalancerStatus creates the loadbalancer status from the different possible sources
func (lbaas *LbaasV2) createLoadBalancerStatus(service *corev1.Service, svcConf *serviceConfig, addr string) *corev1.LoadBalancerStatus {
	status := &corev1.LoadBalancerStatus{}
	// If hostname is explicetly set
	if hostname := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerLoadbalancerHostname, ""); hostname != "" {
		status.Ingress = []corev1.LoadBalancerIngress{{Hostname: hostname}}
		return status
	}
	// If the load balancer is using the PROXY protocol, expose its IP address via
	// the Hostname field to prevent kube-proxy from injecting an iptables bypass.
	// This is a workaround until
	// https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/1860-kube-proxy-IP-node-binding
	// is implemented (maybe in v1.22).
	if svcConf.enableProxyProtocol && lbaas.opts.EnableIngressHostname {
		fakeHostname := fmt.Sprintf("%s.%s", addr, lbaas.opts.IngressHostnameSuffix)
		status.Ingress = []corev1.LoadBalancerIngress{{Hostname: fakeHostname}}
		return status
	}
	// Default to IP
	status.Ingress = []corev1.LoadBalancerIngress{{IP: addr}}
	return status
}

func (lbaas *LbaasV2) ensureOctaviaLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (lbs *corev1.LoadBalancerStatus, err error) {
	svcConf := new(serviceConfig)

	// Update the service annotations(e.g. add loadbalancer.openstack.org/load-balancer-id) in the end if it doesn't exist.
	patcher := newServicePatcher(lbaas.kclient, service)
	defer func() { err = patcher.Patch(ctx, err) }()

	if err := lbaas.checkService(service, nodes, svcConf); err != nil {
		return nil, err
	}

	// Use more meaningful name for the load balancer but still need to check the legacy name for backward compatibility.
	lbName := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	svcConf.lbName = lbName
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	var loadbalancer *loadbalancers.LoadBalancer
	isLBOwner := false
	createNewLB := false

	// Check the load balancer in the Service annotation.
	if svcConf.lbID != "" {
		loadbalancer, err = openstackutil.GetLoadbalancerByID(lbaas.lb, svcConf.lbID)
		if err != nil {
			return nil, fmt.Errorf("failed to get load balancer %s: %v", svcConf.lbID, err)
		}

		// If this LB name matches the default generated name, the Service 'owns' the LB, but it's also possible for this
		// LB to be shared by other Services.
		// If the names don't match, this is a LB this Service wants to attach.
		if loadbalancer.Name == lbName {
			isLBOwner = true
		}

		// Shared LB can only be supported when the Tag feature is available in Octavia.
		if !svcConf.supportLBTags && !isLBOwner {
			return nil, fmt.Errorf("shared load balancer is only supported with the tag feature in the cloud load balancer service")
		}

		// The load balancer can only be shared with the configured number of Services.
		if svcConf.supportLBTags {
			sharedCount := 0
			for _, tag := range loadbalancer.Tags {
				if strings.HasPrefix(tag, servicePrefix) {
					sharedCount++
				}
			}
			if !isLBOwner && !cpoutil.Contains(loadbalancer.Tags, lbName) && sharedCount+1 > lbaas.opts.MaxSharedLB {
				return nil, fmt.Errorf("load balancer %s already shared with %d Services", loadbalancer.ID, sharedCount)
			}
		}
	} else {
		legacyName := lbaas.getLoadBalancerLegacyName(ctx, clusterName, service)
		loadbalancer, err = getLoadbalancerByName(lbaas.lb, lbName, legacyName)
		if err != nil {
			if err != cpoerrors.ErrNotFound {
				return nil, fmt.Errorf("error getting loadbalancer for Service %s: %v", serviceName, err)
			}

			klog.InfoS("Creating fully populated loadbalancer", "lbName", lbName, "service", klog.KObj(service))
			loadbalancer, err = lbaas.createFullyPopulatedOctaviaLoadBalancer(lbName, clusterName, service, nodes, svcConf)
			if err != nil {
				return nil, fmt.Errorf("error creating loadbalancer %s: %v", lbName, err)
			}
			createNewLB = true
		} else {
			// This is a Service created before shared LB is supported.
			isLBOwner = true
		}
	}

	if loadbalancer.ProvisioningStatus != activeStatus {
		return nil, fmt.Errorf("load balancer %s is not ACTIVE, current provisioning status: %s", loadbalancer.ID, loadbalancer.ProvisioningStatus)
	}

	loadbalancer.Listeners, err = openstackutil.GetListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return nil, err
	}

	klog.V(4).InfoS("Load balancer ensured", "lbID", loadbalancer.ID, "isLBOwner", isLBOwner, "createNewLB", createNewLB)

	// This is an existing load balancer, either created by occm for other Services or by the user outside of cluster.
	if !createNewLB {
		curListeners := loadbalancer.Listeners
		curListenerMapping := make(map[listenerKey]*listeners.Listener)
		for i, l := range curListeners {
			key := listenerKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
			curListenerMapping[key] = &curListeners[i]
		}
		klog.V(4).InfoS("Existing listeners", "portProtocolMapping", curListenerMapping)

		// Check port conflicts
		if err := lbaas.checkListenerPorts(service, curListenerMapping, isLBOwner, lbName); err != nil {
			return nil, err
		}

		for portIndex, port := range service.Spec.Ports {
			listener, err := lbaas.ensureOctaviaListener(loadbalancer.ID, cutString(fmt.Sprintf("listener_%d_%s", portIndex, lbName)), curListenerMapping, port, svcConf, service)
			if err != nil {
				return nil, err
			}

			pool, err := lbaas.ensureOctaviaPool(loadbalancer.ID, cutString(fmt.Sprintf("pool_%d_%s", portIndex, lbName)), listener, service, port, nodes, svcConf)
			if err != nil {
				return nil, err
			}

			if err := lbaas.ensureOctaviaHealthMonitor(loadbalancer.ID, cutString(fmt.Sprintf("monitor_%d_%s", portIndex, lbName)), pool, port, svcConf); err != nil {
				return nil, err
			}

			// After all ports have been processed, remaining listeners are removed if they were created by this Service.
			// The remove of the listener must always happen at the end of the loop to avoid wrong assignment.
			// Modifying the curListeners would also change the mapping.
			curListeners = popListener(curListeners, listener.ID)
		}

		// Deal with the remaining listeners, delete the listener if it was created by this Service previously.
		if err := lbaas.deleteOctaviaListeners(loadbalancer.ID, curListeners, isLBOwner, lbName); err != nil {
			return nil, err
		}
	}

	addr, err := lbaas.getServiceAddress(clusterName, service, loadbalancer, svcConf)
	if err != nil {
		return nil, err
	}

	// Add annotation to Service and add LB name to load balancer tags.
	lbaas.updateServiceAnnotation(service, ServiceAnnotationLoadBalancerID, loadbalancer.ID)
	if svcConf.supportLBTags {
		lbTags := loadbalancer.Tags
		if !cpoutil.Contains(lbTags, lbName) {
			lbTags = append(lbTags, lbName)
			klog.InfoS("Updating load balancer tags", "lbID", loadbalancer.ID, "tags", lbTags)
			if err := openstackutil.UpdateLoadBalancerTags(lbaas.lb, loadbalancer.ID, lbTags); err != nil {
				return nil, err
			}
		}
	}

	// Create status the load balancer
	status := lbaas.createLoadBalancerStatus(service, svcConf, addr)

	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.ensureSecurityGroup(clusterName, service, nodes, loadbalancer, svcConf.preferredIPFamily, svcConf.lbMemberSubnetID)
		if err != nil {
			return status, fmt.Errorf("failed when reconciling security groups for LB service %v/%v: %v", service.Namespace, service.Name, err)
		}
	}

	return status, nil
}

// EnsureLoadBalancer creates a new load balancer or updates the existing one.
func (lbaas *LbaasV2) EnsureLoadBalancer(ctx context.Context, clusterName string, apiService *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	if !lbaas.opts.Enabled {
		return nil, cloudprovider.ImplementedElsewhere
	}

	mc := metrics.NewMetricContext("loadbalancer", "ensure")
	status, err := lbaas.ensureLoadBalancer(ctx, clusterName, apiService, nodes)
	return status, mc.ObserveReconcile(err)
}

func (lbaas *LbaasV2) ensureLoadBalancer(ctx context.Context, clusterName string, apiService *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	serviceName := fmt.Sprintf("%s/%s", apiService.Namespace, apiService.Name)
	klog.InfoS("EnsureLoadBalancer", "cluster", clusterName, "service", klog.KObj(apiService))

	if lbaas.opts.UseOctavia {
		return lbaas.ensureOctaviaLoadBalancer(ctx, clusterName, apiService, nodes)
	}

	// Following code is just for legacy Neutron-LBaaS support which has been deprecated since OpenStack stable/queens
	// and not recommended using in production. No new features should be added.

	if len(nodes) == 0 {
		return nil, fmt.Errorf("there are no available nodes for LoadBalancer service %s", serviceName)
	}

	lbaas.opts.NetworkID = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerNetworkID, lbaas.opts.NetworkID)
	lbaas.opts.SubnetID = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerSubnetID, lbaas.opts.SubnetID)
	if len(lbaas.opts.SubnetID) == 0 && len(lbaas.opts.NetworkID) == 0 {
		// Get SubnetID automatically.
		// The LB needs to be configured with instance addresses on the same subnet, so get SubnetID by one node.
		subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0], "")
		if err != nil {
			klog.Warningf("Failed to find subnet-id for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
			return nil, fmt.Errorf("no subnet-id for service %s/%s : subnet-id not set in cloud provider config, "+
				"and failed to find subnet-id from OpenStack: %v", apiService.Namespace, apiService.Name, err)
		}
		lbaas.opts.SubnetID = subnetID
	}

	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return nil, fmt.Errorf("no ports provided to openstack load balancer")
	}

	internalAnnotation := getBoolFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerInternal, lbaas.opts.InternalLB)

	var lbClass *LBClass
	var floatingNetworkID string
	var floatingSubnetID string
	if !internalAnnotation {
		klog.V(4).Infof("Ensure an external loadbalancer service")

		class := getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerClass, "")
		if class != "" {
			lbClass = lbaas.opts.LBClasses[class]
			if lbClass == nil {
				return nil, fmt.Errorf("invalid loadbalancer class %q", class)
			}

			klog.V(4).Infof("found loadbalancer class %q with %+v", class, lbClass)

			// read floating network id and floating subnet id from loadbalancer class
			if lbClass.FloatingNetworkID != "" {
				floatingNetworkID = lbClass.FloatingNetworkID
			}

			if lbClass.FloatingSubnetID != "" {
				floatingSubnetID = lbClass.FloatingSubnetID
			}
		}

		if floatingNetworkID == "" {
			floatingNetworkID = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerFloatingNetworkID, lbaas.opts.FloatingNetworkID)
			if floatingNetworkID == "" {
				var err error
				floatingNetworkID, err = openstackutil.GetFloatingNetworkID(lbaas.network)
				if err != nil {
					klog.Warningf("Failed to find floating-network-id for Service %s: %v", serviceName, err)
				}
			}
		}

		if floatingSubnetID == "" {
			floatingSubnetName := getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerFloatingSubnet, "")
			if floatingSubnetName != "" {
				lbSubnet, err := lbaas.getSubnet(floatingSubnetName)
				if err != nil || lbSubnet == nil {
					klog.Warningf("Failed to find floating-subnet-id for Service %s: %v", serviceName, err)
				} else {
					floatingSubnetID = lbSubnet.ID
				}
			}

			if floatingSubnetID == "" {
				floatingSubnetID = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerFloatingSubnetID, lbaas.opts.FloatingSubnetID)
			}
		}

		// check subnets belongs to network
		if floatingNetworkID != "" && floatingSubnetID != "" {
			mc := metrics.NewMetricContext("subnet", "get")
			subnet, err := subnets.Get(lbaas.network, floatingSubnetID).Extract()
			if mc.ObserveRequest(err) != nil {
				return nil, fmt.Errorf("failed to find subnet %q: %v", floatingSubnetID, err)
			}

			if subnet.NetworkID != floatingNetworkID {
				return nil, fmt.Errorf("FloatingSubnet %q doesn't belong to FloatingNetwork %q", floatingSubnetID, floatingSubnetID)
			}
		}
	} else {
		klog.V(4).Infof("Ensure an internal loadbalancer service.")
	}

	// Check for TCP protocol on each port
	for _, port := range ports {
		if port.Protocol != corev1.ProtocolTCP {
			return nil, fmt.Errorf("only TCP LoadBalancer is supported for openstack load balancers")
		}
	}

	sourceRanges, err := GetLoadBalancerSourceRanges(apiService, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get source ranges for loadbalancer service %s: %v", serviceName, err)
	}
	if !IsAllowAll(sourceRanges) && !lbaas.opts.ManageSecurityGroups {
		return nil, fmt.Errorf("source range restrictions are not supported for openstack load balancers without managing security groups")
	}

	affinity := apiService.Spec.SessionAffinity
	var persistence *v2pools.SessionPersistence
	switch affinity {
	case corev1.ServiceAffinityNone:
		persistence = nil
	case corev1.ServiceAffinityClientIP:
		persistence = &v2pools.SessionPersistence{Type: "SOURCE_IP"}
	default:
		return nil, fmt.Errorf("unsupported load balancer affinity: %v", affinity)
	}

	// Use more meaningful name for the load balancer but still need to check the legacy name for backward compatibility.
	name := lbaas.GetLoadBalancerName(ctx, clusterName, apiService)
	legacyName := lbaas.getLoadBalancerLegacyName(ctx, clusterName, apiService)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil {
		if err != cpoerrors.ErrNotFound {
			return nil, fmt.Errorf("error getting loadbalancer for Service %s: %v", serviceName, err)
		}

		klog.V(2).Infof("Creating loadbalancer %s", name)

		portID := ""
		if lbClass == nil {
			portID = getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerPortID, "")
		}
		loadbalancer, err = lbaas.createLoadBalancer(apiService, name, clusterName, lbClass, internalAnnotation, portID)
		if err != nil {
			return nil, fmt.Errorf("error creating loadbalancer %s: %v", name, err)
		}
	} else {
		klog.V(2).Infof("LoadBalancer %s(%s) already exists", loadbalancer.Name, loadbalancer.ID)
	}

	if err := openstackutil.WaitLoadbalancerActive(lbaas.lb, loadbalancer.ID); err != nil {
		return nil, err
	}

	oldListeners, err := openstackutil.GetListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting LB %s listeners: %v", loadbalancer.Name, err)
	}
	curListenerMapping := make(map[listenerKey]*listeners.Listener)
	for i, l := range oldListeners {
		key := listenerKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
		curListenerMapping[key] = &oldListeners[i]
	}
	for portIndex, port := range ports {
		key := listenerKey{Protocol: listeners.Protocol(port.Protocol), Port: int(port.Port)}
		listener := curListenerMapping[key]
		climit := getStringFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerConnLimit, "-1")
		connLimit := -1
		tmp, err := strconv.Atoi(climit)
		if err != nil {
			klog.V(4).Infof("Could not parse int value from \"%s\" error \"%v\" failing back to default", climit, err)
		} else {
			connLimit = tmp
		}

		if listener == nil {
			listenerProtocol := listeners.Protocol(port.Protocol)
			listenerCreateOpt := listeners.CreateOpts{
				Name:           cutString(fmt.Sprintf("listener_%d_%s", portIndex, name)),
				Protocol:       listenerProtocol,
				ProtocolPort:   int(port.Port),
				ConnLimit:      &connLimit,
				LoadbalancerID: loadbalancer.ID,
			}

			klog.V(4).Infof("Creating listener for port %d using protocol: %s", int(port.Port), listenerProtocol)

			listener, err = openstackutil.CreateListener(lbaas.lb, loadbalancer.ID, listenerCreateOpt)
			if err != nil {
				return nil, fmt.Errorf("failed to create listener for loadbalancer %s: %v", loadbalancer.ID, err)
			}

			klog.V(4).Infof("Listener %s created for loadbalancer %s", listener.ID, loadbalancer.ID)
		} else {
			listenerChanged := false
			updateOpts := listeners.UpdateOpts{}

			if connLimit != listener.ConnLimit {
				updateOpts.ConnLimit = &connLimit
				listenerChanged = true
			}

			if listenerChanged {
				if err := openstackutil.UpdateListener(lbaas.lb, loadbalancer.ID, listener.ID, updateOpts); err != nil {
					return nil, fmt.Errorf("failed to update listener %s of loadbalancer %s: %v", listener.ID, loadbalancer.ID, err)
				}

				klog.V(4).Infof("Listener %s updated for loadbalancer %s", listener.ID, loadbalancer.ID)
			}
		}

		// After all ports have been processed, remaining listeners are removed as obsolete.
		// Pop valid listeners.
		oldListeners = popListener(oldListeners, listener.ID)

		pool, err := openstackutil.GetPoolByListener(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != cpoerrors.ErrNotFound {
			return nil, fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
		}
		if pool == nil {
			// Use the protocol of the listener
			poolProto := v2pools.Protocol(listener.Protocol)

			lbmethod := v2pools.LBMethod(lbaas.opts.LBMethod)
			createOpt := v2pools.CreateOpts{
				Name:        cutString(fmt.Sprintf("pool_%d_%s", portIndex, name)),
				Protocol:    poolProto,
				LBMethod:    lbmethod,
				ListenerID:  listener.ID,
				Persistence: persistence,
			}

			klog.Infof("Creating pool for listener %s using protocol %s", listener.ID, poolProto)
			pool, err = openstackutil.CreatePool(lbaas.lb, createOpt, loadbalancer.ID)
			if err != nil {
				return nil, err
			}
		}

		klog.V(4).Infof("Pool created for listener %s: %s", listener.ID, pool.ID)

		members, err := openstackutil.GetMembersbyPool(lbaas.lb, pool.ID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting pool members %s: %v", pool.ID, err)
		}
		for _, node := range nodes {
			addr, err := nodeAddressForLB(node, "")
			if err != nil {
				if err == cpoerrors.ErrNotFound {
					// Node failure, do not create member
					klog.Warningf("Failed to create LB pool member for node %s: %v", node.Name, err)
					continue
				} else {
					return nil, fmt.Errorf("error getting address for node %s: %v", node.Name, err)
				}
			}

			if !memberExists(members, addr, int(port.NodePort)) {
				klog.V(4).Infof("Creating member for pool %s", pool.ID)
				mc := metrics.NewMetricContext("loadbalancer_member", "create")
				_, err := v2pools.CreateMember(lbaas.lb, pool.ID, v2pools.CreateMemberOpts{
					Name:         cutString(fmt.Sprintf("member_%d_%s_%s", portIndex, node.Name, name)),
					ProtocolPort: int(port.NodePort),
					Address:      addr,
					SubnetID:     lbaas.opts.SubnetID,
				}).Extract()
				if mc.ObserveRequest(err) != nil {
					return nil, fmt.Errorf("error creating LB pool member for node: %s, %v", node.Name, err)
				}

				if err := openstackutil.WaitLoadbalancerActive(lbaas.lb, loadbalancer.ID); err != nil {
					return nil, err
				}
			} else {
				// After all members have been processed, remaining members are deleted as obsolete.
				members = popMember(members, addr, int(port.NodePort))
			}

			klog.V(4).Infof("Ensured pool %s has member for %s at %s", pool.ID, node.Name, addr)
		}

		// Delete obsolete members for this pool
		for _, member := range members {
			klog.V(4).Infof("Deleting obsolete member %s for pool %s address %s", member.ID, pool.ID, member.Address)
			mc := metrics.NewMetricContext("loadbalancer_member", "delete")
			err := v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				_ = mc.ObserveRequest(err)
				return nil, fmt.Errorf("error deleting obsolete member %s for pool %s address %s: %v", member.ID, pool.ID, member.Address, err)
			}
			_ = mc.ObserveRequest(nil)

			if err := openstackutil.WaitLoadbalancerActive(lbaas.lb, loadbalancer.ID); err != nil {
				return nil, err
			}
		}

		monitorID := pool.MonitorID
		enableHealthMonitor := getBoolFromServiceAnnotation(apiService, ServiceAnnotationLoadBalancerEnableHealthMonitor, lbaas.opts.CreateMonitor)
		if monitorID == "" && enableHealthMonitor {
			klog.Infof("Creating monitor for pool %s", pool.ID)
			monitorProtocol := string(port.Protocol)
			if port.Protocol == corev1.ProtocolUDP {
				monitorProtocol = "UDP-CONNECT"
			}
			createOpts := v2monitors.CreateOpts{
				Name:       cutString(fmt.Sprintf("monitor_%d_%s", portIndex, name)),
				PoolID:     pool.ID,
				Type:       monitorProtocol,
				Delay:      int(lbaas.opts.MonitorDelay.Duration.Seconds()),
				Timeout:    int(lbaas.opts.MonitorTimeout.Duration.Seconds()),
				MaxRetries: int(lbaas.opts.MonitorMaxRetries),
			}
			_, err := openstackutil.CreateHealthMonitor(lbaas.lb, createOpts, loadbalancer.ID)
			if err != nil {
				return nil, err
			}
		} else if monitorID != "" && !enableHealthMonitor {
			klog.Infof("Deleting health monitor %s for pool %s", monitorID, pool.ID)
			mc := metrics.NewMetricContext("loadbalancer_healthmonitor", "delete")
			err := v2monitors.Delete(lbaas.lb, monitorID).ExtractErr()
			if mc.ObserveRequest(err) != nil {
				return nil, fmt.Errorf("failed to delete health monitor %s for pool %s, error: %v", monitorID, pool.ID, err)
			}
		}
	}

	// All remaining listeners are obsolete, delete
	for _, listener := range oldListeners {
		klog.V(4).Infof("Deleting obsolete listener %s:", listener.ID)
		// get pool for listener
		pool, err := openstackutil.GetPoolByListener(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != cpoerrors.ErrNotFound {
			return nil, fmt.Errorf("error getting pool for obsolete listener %s: %v", listener.ID, err)
		}
		if pool != nil {
			// get and delete monitor
			monitorID := pool.MonitorID
			if monitorID != "" {
				klog.Infof("Deleting obsolete monitor %s for pool %s", monitorID, pool.ID)
				if err := openstackutil.DeleteHealthMonitor(lbaas.lb, monitorID, loadbalancer.ID); err != nil {
					return nil, err
				}
			}
			// get and delete pool members
			members, err := openstackutil.GetMembersbyPool(lbaas.lb, pool.ID)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error getting members for pool %s: %v", pool.ID, err)
			}
			for _, member := range members {
				klog.V(4).Infof("Deleting obsolete member %s for pool %s address %s", member.ID, pool.ID, member.Address)
				mc := metrics.NewMetricContext("loadbalancer_member", "delete")
				err := v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
				if err != nil && !cpoerrors.IsNotFound(err) {
					_ = mc.ObserveRequest(err)
					return nil, fmt.Errorf("error deleting obsolete member %s for pool %s address %s: %v", member.ID, pool.ID, member.Address, err)
				}
				_ = mc.ObserveRequest(nil)

				if err := openstackutil.WaitLoadbalancerActive(lbaas.lb, loadbalancer.ID); err != nil {
					return nil, err
				}
			}
			klog.Infof("Deleting obsolete pool %s for listener %s", pool.ID, listener.ID)
			// delete pool
			if err := openstackutil.DeletePool(lbaas.lb, pool.ID, loadbalancer.ID); err != nil {
				return nil, err
			}
		}
		// delete listener
		if err := openstackutil.DeleteListener(lbaas.lb, listener.ID, loadbalancer.ID); err != nil {
			return nil, err
		}
		klog.Infof("Deleted obsolete listener: %s", listener.ID)
	}

	// Priority of choosing VIP port floating IP:
	// 1. The floating IP that is already attached to the VIP port.
	// 2. Floating IP specified in Spec.LoadBalancerIP
	// 3. Create a new one
	var floatIP *floatingips.FloatingIP
	if !internalAnnotation {

		// first attempt: fetch floating IP attached to load balancer's VIP port
		portID := loadbalancer.VipPortID
		floatIP, err = openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
		if err != nil {
			return nil, fmt.Errorf("failed when getting floating IP for port %s: %v", portID, err)
		}
		klog.V(4).Infof("Found floating IP %q by loadbalancer port ID %q", floatIP, portID)

		// second attempt: fetch floating IP specified in service Spec.LoadBalancerIP
		// if found, associate floating IP with loadbalancer's VIP port
		loadBalancerIP := apiService.Spec.LoadBalancerIP
		if floatIP == nil && loadBalancerIP != "" {

			opts := floatingips.ListOpts{
				FloatingIP: loadBalancerIP,
			}
			existingIPs, err := openstackutil.GetFloatingIPs(lbaas.network, opts)
			if err != nil {
				return nil, fmt.Errorf("failed when trying to get existing floating IP %s, error: %v", loadBalancerIP, err)
			}
			klog.V(4).Infof("Found floating IPs %v by loadbalancer IP %q", existingIPs, loadBalancerIP)

			if len(existingIPs) > 0 {
				floatingip := existingIPs[0]
				if len(floatingip.PortID) == 0 {
					floatUpdateOpts := floatingips.UpdateOpts{
						PortID: &portID,
					}
					klog.V(4).Infof("Attaching floating IP %q to loadbalancer port %q", floatingip.FloatingIP, portID)
					mc := metrics.NewMetricContext("floating_ip", "update")
					floatIP, err = floatingips.Update(lbaas.network, floatingip.ID, floatUpdateOpts).Extract()
					if mc.ObserveRequest(err) != nil {
						return nil, fmt.Errorf("error updating LB floating IP %+v: %v", floatUpdateOpts, err)
					}
				} else {
					return nil, fmt.Errorf("floating IP %s is not available", loadBalancerIP)
				}
			}
		}

		// third attempt: create a new floating IP
		if floatIP == nil {
			if floatingNetworkID != "" {
				klog.V(4).Infof("Creating floating IP %s for loadbalancer %s", loadBalancerIP, loadbalancer.ID)
				floatIPOpts := floatingips.CreateOpts{
					FloatingNetworkID: floatingNetworkID,
					PortID:            portID,
					Description:       fmt.Sprintf("Floating IP for Kubernetes external service %s from cluster %s", serviceName, clusterName),
				}

				if floatingSubnetID != "" {
					floatIPOpts.SubnetID = floatingSubnetID
				}

				if loadBalancerIP != "" {
					floatIPOpts.FloatingIP = loadBalancerIP
				}

				klog.V(4).Infof("creating floating IP with opts %+v", floatIPOpts)
				mc := metrics.NewMetricContext("floating_ip", "create")
				floatIP, err = floatingips.Create(lbaas.network, floatIPOpts).Extract()
				if mc.ObserveRequest(err) != nil {
					return nil, fmt.Errorf("error creating LB floating IP %+v: %v", floatIPOpts, err)
				}
			} else {
				klog.Warningf("Failed to find floating network information, for Service %s,"+
					"forcing to ensure an internal load balancer service", serviceName)
			}
		}
	}

	status := &corev1.LoadBalancerStatus{}

	if floatIP != nil {
		status.Ingress = []corev1.LoadBalancerIngress{{IP: floatIP.FloatingIP}}
	} else {
		status.Ingress = []corev1.LoadBalancerIngress{{IP: loadbalancer.VipAddress}}
	}

	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.ensureSecurityGroup(clusterName, apiService, nodes, loadbalancer, "", lbaas.opts.SubnetID)
		if err != nil {
			return status, fmt.Errorf("failed when reconciling security groups for LB service %v/%v: %v", apiService.Namespace, apiService.Name, err)
		}
	}

	return status, nil
}

func (lbaas *LbaasV2) listSubnetsForNetwork(networkID string, tweak ...TweakSubNetListOpsFunction) ([]subnets.Subnet, error) {
	var opts = subnets.ListOpts{NetworkID: networkID}
	for _, f := range tweak {
		if f != nil {
			f(&opts)
		}
	}
	mc := metrics.NewMetricContext("subnet", "list")
	allPages, err := subnets.List(lbaas.network, opts).AllPages()
	if mc.ObserveRequest(err) != nil {
		return nil, fmt.Errorf("error listing subnets of network %s: %v", networkID, err)
	}
	subs, err := subnets.ExtractSubnets(allPages)
	if err != nil {
		return nil, fmt.Errorf("error extracting subnets from pages: %v", err)
	}

	if len(subs) == 0 {
		return nil, fmt.Errorf("could not find subnets for network %s", networkID)
	}
	return subs, nil
}

func (lbaas *LbaasV2) getSubnet(subnet string) (*subnets.Subnet, error) {
	if subnet == "" {
		return nil, nil
	}

	mc := metrics.NewMetricContext("subnet", "list")
	allPages, err := subnets.List(lbaas.network, subnets.ListOpts{Name: subnet}).AllPages()
	if mc.ObserveRequest(err) != nil {
		return nil, fmt.Errorf("error listing subnets: %v", err)
	}
	subs, err := subnets.ExtractSubnets(allPages)
	if err != nil {
		return nil, fmt.Errorf("error extracting subnets from pages: %v", err)
	}

	if len(subs) == 0 {
		return nil, fmt.Errorf("could not find subnet %s", subnet)
	}
	if len(subs) == 1 {
		return &subs[0], nil
	}
	return nil, fmt.Errorf("found multiple subnets with name %s", subnet)
}

// ensureSecurityRule ensures to create a security rule for a defined security
// group, if it not present.
func (lbaas *LbaasV2) ensureSecurityRule(
	direction rules.RuleDirection,
	protocol rules.RuleProtocol,
	etherType rules.RuleEtherType,
	remoteIPPrefix, secGroupID string,
	portRangeMin, portRangeMax int,
) error {
	sgListopts := rules.ListOpts{
		Direction:      string(direction),
		Protocol:       string(protocol),
		PortRangeMax:   portRangeMin,
		PortRangeMin:   portRangeMax,
		RemoteIPPrefix: remoteIPPrefix,
		SecGroupID:     secGroupID,
	}
	sgRules, err := getSecurityGroupRules(lbaas.network, sgListopts)
	if err != nil && !cpoerrors.IsNotFound(err) {
		return fmt.Errorf(
			"failed to find security group rules in %s: %v", secGroupID, err)
	}
	if len(sgRules) != 0 {
		return nil
	}

	sgRuleCreateOpts := rules.CreateOpts{
		Direction:      direction,
		Protocol:       protocol,
		PortRangeMax:   portRangeMin,
		PortRangeMin:   portRangeMax,
		RemoteIPPrefix: remoteIPPrefix,
		SecGroupID:     secGroupID,
		EtherType:      etherType,
	}

	mc := metrics.NewMetricContext("security_group_rule", "create")
	_, err = rules.Create(lbaas.network, sgRuleCreateOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf(
			"failed to create rule for security group %s: %v",
			secGroupID, err)
	}
	return nil
}

// ensureSecurityGroup ensures security group exist for specific loadbalancer service.
// Creating security group for specific loadbalancer service when it does not exist.
func (lbaas *LbaasV2) ensureSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node,
	loadbalancer *loadbalancers.LoadBalancer, preferredIPFamily corev1.IPFamily, memberSubnetID string) error {

	if lbaas.opts.UseOctavia {
		return lbaas.ensureAndUpdateOctaviaSecurityGroup(clusterName, apiService, nodes, memberSubnetID)
	}

	// Following code is just for legacy Neutron-LBaaS support which has been deprecated since OpenStack stable/queens
	// and not recommended using in production. No new features should be added.

	// find node-security-group for service
	var err error
	if len(lbaas.opts.NodeSecurityGroupIDs) == 0 && !lbaas.opts.UseOctavia {
		lbaas.opts.NodeSecurityGroupIDs, err = getNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes)
		if err != nil {
			return fmt.Errorf("failed to find node-security-group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
		}

		klog.V(4).Infof("find node-security-group %v for loadbalancer service %s/%s", lbaas.opts.NodeSecurityGroupIDs, apiService.Namespace, apiService.Name)
	}

	// get service ports
	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	// get service source ranges
	sourceRanges, err := GetLoadBalancerSourceRanges(apiService, preferredIPFamily)
	if err != nil {
		return fmt.Errorf("failed to get source ranges for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
	}

	// ensure security group for LB
	lbSecGroupName := getSecurityGroupName(apiService)
	lbSecGroupID, err := secgroups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		// If the security group of LB not exist, create it later
		if isSecurityGroupNotFound(err) {
			lbSecGroupID = ""
		} else {
			return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
		}
	}
	if len(lbSecGroupID) == 0 {
		// create security group
		lbSecGroupCreateOpts := groups.CreateOpts{
			Name:        lbSecGroupName,
			Description: fmt.Sprintf("Security Group for %s/%s Service LoadBalancer in cluster %s", apiService.Namespace, apiService.Name, clusterName),
		}

		mc := metrics.NewMetricContext("security_group", "create")
		lbSecGroup, err := groups.Create(lbaas.network, lbSecGroupCreateOpts).Extract()
		if mc.ObserveRequest(err) != nil {
			return fmt.Errorf("failed to create Security Group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
		}
		lbSecGroupID = lbSecGroup.ID

		if !lbaas.opts.UseOctavia {
			//add rule in security group
			for _, port := range ports {
				for _, sourceRange := range sourceRanges.StringSlice() {
					ethertype := rules.EtherType4
					network, _, err := net.ParseCIDR(sourceRange)

					if err != nil {
						return fmt.Errorf("error parsing source range %s as a CIDR: %v", sourceRange, err)
					}

					if network.To4() == nil {
						ethertype = rules.EtherType6
					}

					lbSecGroupRuleCreateOpts := rules.CreateOpts{
						Direction:      rules.DirIngress,
						PortRangeMax:   int(port.Port),
						PortRangeMin:   int(port.Port),
						Protocol:       toRuleProtocol(port.Protocol),
						RemoteIPPrefix: sourceRange,
						SecGroupID:     lbSecGroup.ID,
						EtherType:      ethertype,
					}

					mc := metrics.NewMetricContext("security_group_rule", "create")
					_, err = rules.Create(lbaas.network, lbSecGroupRuleCreateOpts).Extract()

					if mc.ObserveRequest(err) != nil {
						return fmt.Errorf("error occurred creating rule for SecGroup %s: %v", lbSecGroup.ID, err)
					}
				}
			}

			lbSecGroupRuleCreateOpts := rules.CreateOpts{
				Direction:      rules.DirIngress,
				PortRangeMax:   4, // ICMP: Code -  Values for ICMP  "Destination Unreachable: Fragmentation Needed and Don't Fragment was Set"
				PortRangeMin:   3, // ICMP: Type
				Protocol:       rules.ProtocolICMP,
				RemoteIPPrefix: "0.0.0.0/0", // The Fragmentation packet can come from anywhere along the path back to the sourceRange - we need to all this from all
				SecGroupID:     lbSecGroup.ID,
				EtherType:      rules.EtherType4,
			}

			mc := metrics.NewMetricContext("security_group_rule", "create")
			_, err = rules.Create(lbaas.network, lbSecGroupRuleCreateOpts).Extract()

			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("error occurred creating rule for SecGroup %s: %v", lbSecGroup.ID, err)
			}

			lbSecGroupRuleCreateOpts = rules.CreateOpts{
				Direction:      rules.DirIngress,
				PortRangeMax:   0, // ICMP: Code - Values for ICMP "Packet Too Big"
				PortRangeMin:   2, // ICMP: Type
				Protocol:       rules.ProtocolICMP,
				RemoteIPPrefix: "::/0", // The Fragmentation packet can come from anywhere along the path back to the sourceRange - we need to all this from all
				SecGroupID:     lbSecGroup.ID,
				EtherType:      rules.EtherType6,
			}

			mc = metrics.NewMetricContext("security_group_rule", "create")
			_, err = rules.Create(lbaas.network, lbSecGroupRuleCreateOpts).Extract()
			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("error occurred creating rule for SecGroup %s: %v", lbSecGroup.ID, err)
			}

			// get security groups of port
			portID := loadbalancer.VipPortID
			port, err := getPortByID(lbaas.network, portID)
			if err != nil {
				return err
			}

			// ensure the vip port has the security groups
			found := false
			for _, portSecurityGroups := range port.SecurityGroups {
				if portSecurityGroups == lbSecGroup.ID {
					found = true
					break
				}
			}

			// update loadbalancer vip port
			if !found {
				port.SecurityGroups = append(port.SecurityGroups, lbSecGroup.ID)
				updateOpts := neutronports.UpdateOpts{SecurityGroups: &port.SecurityGroups}
				mc := metrics.NewMetricContext("port", "update")
				res := neutronports.Update(lbaas.network, portID, updateOpts)
				if mc.ObserveRequest(res.Err) != nil {
					msg := fmt.Sprintf("Error occurred updating port %s for loadbalancer service %s/%s: %v", portID, apiService.Namespace, apiService.Name, res.Err)
					return fmt.Errorf(msg)
				}
			}
		}
	}

	// ensure rules for node security group
	for _, port := range ports {
		// If Octavia is used, the VIP port security group is already taken good care of, we only need to allow ingress
		// traffic from Octavia amphorae to the node port on the worker nodes.
		if lbaas.opts.UseOctavia {
			mc := metrics.NewMetricContext("subnet", "get")
			subnet, err := subnets.Get(lbaas.network, memberSubnetID).Extract()
			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("failed to find subnet %s from openstack: %v", memberSubnetID, err)
			}

			sgListopts := rules.ListOpts{
				Direction:      string(rules.DirIngress),
				Protocol:       string(port.Protocol),
				PortRangeMax:   int(port.NodePort),
				PortRangeMin:   int(port.NodePort),
				RemoteIPPrefix: subnet.CIDR,
				SecGroupID:     lbSecGroupID,
			}
			sgRules, err := getSecurityGroupRules(lbaas.network, sgListopts)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return fmt.Errorf("failed to find security group rules in %s: %v", lbSecGroupID, err)
			}
			if len(sgRules) != 0 {
				continue
			}

			ethertype := rules.EtherType4
			if netutils.IsIPv6CIDRString(subnet.CIDR) {
				ethertype = rules.EtherType6
			}

			// The Octavia amphorae and worker nodes are supposed to be in the same subnet. We allow the ingress traffic
			// from the amphorae to the specific node port on the nodes.
			sgRuleCreateOpts := rules.CreateOpts{
				Direction:      rules.DirIngress,
				PortRangeMax:   int(port.NodePort),
				PortRangeMin:   int(port.NodePort),
				Protocol:       toRuleProtocol(port.Protocol),
				RemoteIPPrefix: subnet.CIDR,
				SecGroupID:     lbSecGroupID,
				EtherType:      ethertype,
			}
			mc = metrics.NewMetricContext("security_group_rule", "create")
			_, err = rules.Create(lbaas.network, sgRuleCreateOpts).Extract()
			if mc.ObserveRequest(err) != nil {
				return fmt.Errorf("failed to create rule for security group %s: %v", lbSecGroupID, err)
			}

			if err := applyNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes, lbSecGroupID); err != nil {
				return err
			}
		} else {
			for _, nodeSecurityGroupID := range lbaas.opts.NodeSecurityGroupIDs {
				opts := rules.ListOpts{
					Direction:     string(rules.DirIngress),
					SecGroupID:    nodeSecurityGroupID,
					RemoteGroupID: lbSecGroupID,
					PortRangeMax:  int(port.NodePort),
					PortRangeMin:  int(port.NodePort),
					Protocol:      string(port.Protocol),
				}
				secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)
				if err != nil && !cpoerrors.IsNotFound(err) {
					msg := fmt.Sprintf("Error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, nodeSecurityGroupID, err)
					return fmt.Errorf(msg)
				}
				if len(secGroupRules) != 0 {
					// Do not add rule when find rules for remote group in the Node Security Group
					continue
				}

				// Add the rules in the Node Security Group
				err = createNodeSecurityGroup(lbaas.network, nodeSecurityGroupID, int(port.NodePort), port.Protocol, lbSecGroupID)
				if err != nil {
					return fmt.Errorf("error occurred creating security group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
				}
			}
		}
	}

	return nil
}

func (lbaas *LbaasV2) updateOctaviaLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	svcConf := new(serviceConfig)
	var err error
	if err := lbaas.checkServiceUpdate(service, nodes, svcConf); err != nil {
		return err
	}

	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	klog.V(2).Infof("Updating %d nodes for Service %s in cluster %s", len(nodes), serviceName, clusterName)

	// Get load balancer
	var loadbalancer *loadbalancers.LoadBalancer
	if svcConf.lbID != "" {
		loadbalancer, err = openstackutil.GetLoadbalancerByID(lbaas.lb, svcConf.lbID)
		if err != nil {
			return fmt.Errorf("failed to get load balancer %s: %v", svcConf.lbID, err)
		}
	} else {
		// This is a Service created before shared LB is supported.
		name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
		legacyName := lbaas.getLoadBalancerLegacyName(ctx, clusterName, service)
		loadbalancer, err = getLoadbalancerByName(lbaas.lb, name, legacyName)
		if err != nil {
			return err
		}
	}
	if loadbalancer.ProvisioningStatus != activeStatus {
		return fmt.Errorf("load balancer %s is not ACTIVE, current provisioning status: %s", loadbalancer.ID, loadbalancer.ProvisioningStatus)
	}

	loadbalancer.Listeners, err = openstackutil.GetListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return err
	}

	// Now, we have a load balancer.

	// Get all listeners for this loadbalancer, by "port&protocol".
	lbListeners := make(map[listenerKey]listeners.Listener)
	for _, l := range loadbalancer.Listeners {
		key := listenerKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
		lbListeners[key] = l
	}

	// Update pool members for each listener.
	for portIndex, port := range service.Spec.Ports {
		proto := getListenerProtocol(port.Protocol, svcConf)
		listener, ok := lbListeners[listenerKey{
			Protocol: proto,
			Port:     int(port.Port),
		}]
		if !ok {
			return fmt.Errorf("loadbalancer %s does not contain required listener for port %d and protocol %s", loadbalancer.ID, port.Port, port.Protocol)
		}

		_, err := lbaas.ensureOctaviaPool(loadbalancer.ID, cutString(fmt.Sprintf("pool_%d_%s", portIndex, loadbalancer.Name)), &listener, service, port, nodes, svcConf)
		if err != nil {
			return err
		}
	}

	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.updateSecurityGroup(clusterName, service, nodes, svcConf.lbMemberSubnetID)
		if err != nil {
			return fmt.Errorf("failed to update Security Group for loadbalancer service %s: %v", serviceName, err)
		}
	}

	return nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
func (lbaas *LbaasV2) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	if !lbaas.opts.Enabled {
		return cloudprovider.ImplementedElsewhere
	}
	mc := metrics.NewMetricContext("loadbalancer", "update")
	err := lbaas.updateLoadBalancer(ctx, clusterName, service, nodes)
	return mc.ObserveReconcile(err)
}

func (lbaas *LbaasV2) updateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	if lbaas.opts.UseOctavia {
		return lbaas.updateOctaviaLoadBalancer(ctx, clusterName, service, nodes)
	}

	// Following code is just for legacy Neutron-LBaaS support which has been deprecated since OpenStack stable/queens
	// and not recommended using in production. No new features should be added.

	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	klog.V(4).Infof("UpdateLoadBalancer(%v, %s, %v)", clusterName, serviceName, nodes)

	lbaas.opts.SubnetID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerSubnetID, lbaas.opts.SubnetID)
	if len(lbaas.opts.SubnetID) == 0 && len(nodes) > 0 {
		// Get SubnetID automatically.
		// The LB needs to be configured with instance addresses on the same subnet, so get SubnetID by one node.
		subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0], "")
		if err != nil {
			klog.Warningf("Failed to find subnet-id for loadbalancer service %s/%s: %v", service.Namespace, service.Name, err)
			return fmt.Errorf("no subnet-id for service %s/%s : subnet-id not set in cloud provider config, "+
				"and failed to find subnet-id from OpenStack: %v", service.Namespace, service.Name, err)
		}
		lbaas.opts.SubnetID = subnetID
	}

	ports := service.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.getLoadBalancerLegacyName(ctx, clusterName, service)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil {
		return err
	}
	if loadbalancer == nil {
		return fmt.Errorf("loadbalancer does not exist for Service %s", serviceName)
	}

	// Get all listeners for this loadbalancer, by "port key".
	type portKey struct {
		Protocol listeners.Protocol
		Port     int
	}
	var listenerIDs []string
	lbListeners := make(map[portKey]listeners.Listener)
	allListeners, err := openstackutil.GetListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return fmt.Errorf("error getting listeners for LB %s: %v", loadbalancer.ID, err)
	}
	for _, l := range allListeners {
		key := portKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
		lbListeners[key] = l
		listenerIDs = append(listenerIDs, l.ID)
	}

	// Get all pools for this loadbalancer, by listener ID.
	lbPools := make(map[string]v2pools.Pool)
	for _, listenerID := range listenerIDs {
		pool, err := openstackutil.GetPoolByListener(lbaas.lb, loadbalancer.ID, listenerID)
		if err != nil {
			return fmt.Errorf("error getting pool for listener %s: %v", listenerID, err)
		}
		lbPools[listenerID] = *pool
	}

	// Compose Set of member (addresses) that _should_ exist
	addrs := make(map[string]*corev1.Node)
	for _, node := range nodes {
		addr, err := nodeAddressForLB(node, "")
		if err != nil {
			return err
		}
		addrs[addr] = node
	}

	// Check for adding/removing members associated with each port
	for portIndex, port := range ports {
		// Get listener associated with this port
		listener, ok := lbListeners[portKey{
			Protocol: getListenerProtocol(port.Protocol, nil),
			Port:     int(port.Port),
		}]
		if !ok {
			return fmt.Errorf("loadbalancer %s does not contain required listener for port %d and protocol %s", loadbalancer.ID, port.Port, port.Protocol)
		}

		// Get pool associated with this listener
		pool, ok := lbPools[listener.ID]
		if !ok {
			return fmt.Errorf("loadbalancer %s does not contain required pool for listener %s", loadbalancer.ID, listener.ID)
		}

		// Find existing pool members (by address) for this port
		getMembers, err := openstackutil.GetMembersbyPool(lbaas.lb, pool.ID)
		if err != nil {
			return fmt.Errorf("error getting pool members %s: %v", pool.ID, err)
		}
		members := make(map[string]v2pools.Member)
		for _, member := range getMembers {
			members[member.Address] = member
		}

		// Add any new members for this port
		for addr, node := range addrs {
			if _, ok := members[addr]; ok && members[addr].ProtocolPort == int(port.NodePort) {
				// Already exists, do not create member
				continue
			}
			mc := metrics.NewMetricContext("loadbalancer_member", "create")
			_, err := v2pools.CreateMember(lbaas.lb, pool.ID, v2pools.CreateMemberOpts{
				Name:         cutString(fmt.Sprintf("member_%d_%s_%s", portIndex, node.Name, loadbalancer.Name)),
				Address:      addr,
				ProtocolPort: int(port.NodePort),
				SubnetID:     lbaas.opts.SubnetID,
			}).Extract()
			if mc.ObserveRequest(err) != nil {
				return err
			}

			if err := openstackutil.WaitLoadbalancerActive(lbaas.lb, loadbalancer.ID); err != nil {
				return err
			}
		}

		// Remove any old members for this port
		for _, member := range members {
			if _, ok := addrs[member.Address]; ok && member.ProtocolPort == int(port.NodePort) {
				// Still present, do not delete member
				continue
			}
			mc := metrics.NewMetricContext("loadbalancer_member", "delete")
			err = v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return mc.ObserveRequest(err)
			}
			_ = mc.ObserveRequest(nil)

			if err := openstackutil.WaitLoadbalancerActive(lbaas.lb, loadbalancer.ID); err != nil {
				return err
			}
		}
	}

	if lbaas.opts.ManageSecurityGroups {
		// MemberSubnetID is not needed, since this is the legacy call
		err := lbaas.updateSecurityGroup(clusterName, service, nodes, "")
		if err != nil {
			return fmt.Errorf("failed to update Security Group for loadbalancer service %s: %v", serviceName, err)
		}
	}

	return nil
}

// ensureAndUpdateOctaviaSecurityGroup handles the creation and update of the security group and the securiry rules for the octavia load balancer
func (lbaas *LbaasV2) ensureAndUpdateOctaviaSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node, memberSubnetID string) error {
	// get service ports
	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	// ensure security group for LB
	lbSecGroupName := getSecurityGroupName(apiService)
	lbSecGroupID, err := secgroups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		// If the security group of LB not exist, create it later
		if isSecurityGroupNotFound(err) {
			lbSecGroupID = ""
		} else {
			return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
		}
	}
	if len(lbSecGroupID) == 0 {
		// create security group
		lbSecGroupCreateOpts := groups.CreateOpts{
			Name:        lbSecGroupName,
			Description: fmt.Sprintf("Security Group for %s/%s Service LoadBalancer in cluster %s", apiService.Namespace, apiService.Name, clusterName),
		}

		mc := metrics.NewMetricContext("security_group", "create")
		lbSecGroup, err := groups.Create(lbaas.network, lbSecGroupCreateOpts).Extract()
		if mc.ObserveRequest(err) != nil {
			return fmt.Errorf("failed to create Security Group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
		}
		lbSecGroupID = lbSecGroup.ID
	}

	mc := metrics.NewMetricContext("subnet", "get")
	subnet, err := subnets.Get(lbaas.network, memberSubnetID).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf(
			"failed to find subnet %s from openstack: %v", memberSubnetID, err)
	}

	etherType := rules.EtherType4
	if netutils.IsIPv6CIDRString(subnet.CIDR) {
		etherType = rules.EtherType6
	}

	if apiService.Spec.HealthCheckNodePort != 0 {
		err = lbaas.ensureSecurityRule(
			rules.DirIngress,
			rules.ProtocolTCP,
			etherType,
			subnet.CIDR,
			lbSecGroupID,
			int(apiService.Spec.HealthCheckNodePort),
			int(apiService.Spec.HealthCheckNodePort),
		)
		if err != nil {
			return fmt.Errorf(
				"failed to apply security rule for health check node port, %w",
				err)
		}
	}

	// ensure rules for node security group
	for _, port := range ports {
		err = lbaas.ensureSecurityRule(
			rules.DirIngress,
			rules.RuleProtocol(port.Protocol),
			etherType,
			subnet.CIDR,
			lbSecGroupID,
			int(port.NodePort),
			int(port.NodePort),
		)
		if err != nil {
			return fmt.Errorf(
				"failed to apply security rule for port %d, %w",
				port.NodePort, err)
		}

		if err := applyNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes, lbSecGroupID); err != nil {
			return err
		}
	}
	return nil
}

// updateSecurityGroup updating security group for specific loadbalancer service.
func (lbaas *LbaasV2) updateSecurityGroup(clusterName string, apiService *corev1.Service, nodes []*corev1.Node, memberSubnetID string) error {
	if lbaas.opts.UseOctavia {
		return lbaas.ensureAndUpdateOctaviaSecurityGroup(clusterName, apiService, nodes, memberSubnetID)
	}

	// Following code is just for legacy Neutron-LBaaS support which has been deprecated since OpenStack stable/queens
	// and not recommended using in production. No new features should be added.

	originalNodeSecurityGroupIDs := lbaas.opts.NodeSecurityGroupIDs

	var err error
	lbaas.opts.NodeSecurityGroupIDs, err = getNodeSecurityGroupIDForLB(lbaas.compute, lbaas.network, nodes)
	if err != nil {
		return fmt.Errorf("failed to find node-security-group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
	}
	klog.V(4).Infof("find node-security-group %v for loadbalancer service %s/%s", lbaas.opts.NodeSecurityGroupIDs, apiService.Namespace, apiService.Name)

	original := sets.NewString(originalNodeSecurityGroupIDs...)
	current := sets.NewString(lbaas.opts.NodeSecurityGroupIDs...)
	removals := original.Difference(current)

	// Generate Name
	lbSecGroupName := getSecurityGroupName(apiService)
	lbSecGroupID, err := secgroups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
	}

	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	for _, port := range ports {
		for removal := range removals {
			// Delete the rules in the Node Security Group
			opts := rules.ListOpts{
				Direction:     string(rules.DirIngress),
				SecGroupID:    removal,
				RemoteGroupID: lbSecGroupID,
				PortRangeMax:  int(port.NodePort),
				PortRangeMin:  int(port.NodePort),
				Protocol:      string(port.Protocol),
			}
			secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return fmt.Errorf("error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, removal, err)
			}

			for _, rule := range secGroupRules {
				mc := metrics.NewMetricContext("security_group_rule", "delete")
				res := rules.Delete(lbaas.network, rule.ID)
				if res.Err != nil && !cpoerrors.IsNotFound(res.Err) {
					_ = mc.ObserveRequest(err)
					return fmt.Errorf("error occurred deleting security group rule: %s: %v", rule.ID, res.Err)
				}
				_ = mc.ObserveRequest(nil)
			}
		}

		for _, nodeSecurityGroupID := range lbaas.opts.NodeSecurityGroupIDs {
			opts := rules.ListOpts{
				Direction:     string(rules.DirIngress),
				SecGroupID:    nodeSecurityGroupID,
				RemoteGroupID: lbSecGroupID,
				PortRangeMax:  int(port.NodePort),
				PortRangeMin:  int(port.NodePort),
				Protocol:      string(port.Protocol),
			}
			secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return fmt.Errorf("error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, nodeSecurityGroupID, err)
			}
			if len(secGroupRules) != 0 {
				// Do not add rule when find rules for remote group in the Node Security Group
				continue
			}

			// Add the rules in the Node Security Group
			err = createNodeSecurityGroup(lbaas.network, nodeSecurityGroupID, int(port.NodePort), port.Protocol, lbSecGroupID)
			if err != nil {
				return fmt.Errorf("error occurred creating security group for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
			}
		}
	}

	return nil
}

// EnsureLoadBalancerDeleted deletes the specified load balancer
func (lbaas *LbaasV2) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	mc := metrics.NewMetricContext("loadbalancer", "delete")
	err := lbaas.ensureLoadBalancerDeleted(ctx, clusterName, service)
	return mc.ObserveReconcile(err)
}

// ensureLoadBalancerDeletedLegacy deletes the neutron-lbaas load balancer resources.
func (lbaas *LbaasV2) ensureLoadBalancerDeletedLegacy(loadbalancer *loadbalancers.LoadBalancer) error {
	// get all listeners associated with this loadbalancer
	listenerList, err := openstackutil.GetListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return fmt.Errorf("error getting LB %s listeners: %v", loadbalancer.ID, err)
	}

	// get all pools (and health monitors) associated with this loadbalancer
	var monitorIDs []string
	for _, listener := range listenerList {
		pool, err := openstackutil.GetPoolByListener(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != cpoerrors.ErrNotFound {
			return fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
		}
		if pool != nil {
			if pool.MonitorID != "" {
				monitorIDs = append(monitorIDs, pool.MonitorID)
			}
		}
	}

	// delete all monitors
	for _, monitorID := range monitorIDs {
		if err := openstackutil.DeleteHealthMonitor(lbaas.lb, monitorID, loadbalancer.ID); err != nil {
			return err
		}
	}

	// delete all listeners
	if err := lbaas.deleteListeners(loadbalancer.ID, listenerList); err != nil {
		return err
	}

	// delete loadbalancer
	if err := openstackutil.DeleteLoadbalancer(lbaas.lb, loadbalancer.ID, false); err != nil {
		return err
	}

	return nil
}

func (lbaas *LbaasV2) ensureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	lbName := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.getLoadBalancerLegacyName(ctx, clusterName, service)
	var err error
	var loadbalancer *loadbalancers.LoadBalancer
	isSharedLB := false
	updateLBTag := false
	isCreatedByOCCM := false

	svcConf := new(serviceConfig)
	if err := lbaas.checkServiceDelete(service, svcConf); err != nil {
		return err
	}

	if svcConf.lbID != "" {
		loadbalancer, err = openstackutil.GetLoadbalancerByID(lbaas.lb, svcConf.lbID)
	} else {
		// This may happen when this Service creation was failed previously.
		loadbalancer, err = getLoadbalancerByName(lbaas.lb, lbName, legacyName)
	}
	if err != nil && !cpoerrors.IsNotFound(err) {
		return err
	}
	if loadbalancer == nil {
		return nil
	}

	if loadbalancer.ProvisioningStatus != activeStatus {
		return fmt.Errorf("load balancer %s is not ACTIVE, current provisioning status: %s", loadbalancer.ID, loadbalancer.ProvisioningStatus)
	}

	if strings.HasPrefix(loadbalancer.Name, servicePrefix) {
		isCreatedByOCCM = true
	}

	if svcConf.supportLBTags {
		for _, tag := range loadbalancer.Tags {
			if tag == lbName {
				updateLBTag = true
			} else if strings.HasPrefix(tag, servicePrefix) {
				isSharedLB = true
			}
		}
	}

	// If the LB is shared by other Service or the LB was not created by occm, the LB should not be deleted.
	needDeleteLB := true
	if isSharedLB || !isCreatedByOCCM {
		needDeleteLB = false
	}

	klog.V(4).InfoS("Deleting service", "service", klog.KObj(service), "needDeleteLB", needDeleteLB, "isSharedLB", isSharedLB, "updateLBTag", updateLBTag, "isCreatedByOCCM", isCreatedByOCCM)

	keepFloatingAnnotation := getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerKeepFloatingIP, false)
	if needDeleteLB && !keepFloatingAnnotation {
		if loadbalancer.VipPortID != "" {
			portID := loadbalancer.VipPortID
			fip, err := openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
			if err != nil {
				return fmt.Errorf("failed to get floating IP for loadbalancer VIP port %s: %v", portID, err)
			}

			// Delete the floating IP only if it was created dynamically by the controller manager.
			if fip != nil {
				klog.InfoS("Matching floating IP", "floatingIP", fip.FloatingIP, "description", fip.Description)
				matched, err := regexp.Match("Floating IP for Kubernetes external service", []byte(fip.Description))
				if err != nil {
					return err
				}

				if matched {
					klog.InfoS("Deleting floating IP for service", "floatingIP", fip.FloatingIP, "service", klog.KObj(service))
					mc := metrics.NewMetricContext("floating_ip", "delete")
					err := floatingips.Delete(lbaas.network, fip.ID).ExtractErr()
					if mc.ObserveRequest(err) != nil {
						return fmt.Errorf("failed to delete floating IP %s for loadbalancer VIP port %s: %v", fip.FloatingIP, portID, err)
					}
					klog.InfoS("Deleted floating IP for service", "floatingIP", fip.FloatingIP, "service", klog.KObj(service))
				}
			}
		}
	}

	// For neutron-lbaas
	if !lbaas.opts.UseOctavia {
		return lbaas.ensureLoadBalancerDeletedLegacy(loadbalancer)
	}

	if needDeleteLB && lbaas.opts.CascadeDelete {
		klog.InfoS("Deleting load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
		if err := openstackutil.DeleteLoadbalancer(lbaas.lb, loadbalancer.ID, true); err != nil {
			return err
		}
		klog.InfoS("Deleted load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
	} else {
		// get all listeners associated with this loadbalancer
		listenerList, err := openstackutil.GetListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return fmt.Errorf("error getting LB %s listeners: %v", loadbalancer.ID, err)
		}

		if !needDeleteLB {
			var listenersToDelete []listeners.Listener
			curListenerMapping := make(map[listenerKey]*listeners.Listener)
			for i, l := range listenerList {
				key := listenerKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
				curListenerMapping[key] = &listenerList[i]
			}

			for _, port := range service.Spec.Ports {
				proto := getListenerProtocol(port.Protocol, svcConf)
				listener, isPresent := curListenerMapping[listenerKey{
					Protocol: proto,
					Port:     int(port.Port),
				}]
				if isPresent && cpoutil.Contains(listener.Tags, lbName) {
					listenersToDelete = append(listenersToDelete, *listener)
				}
			}
			listenerList = listenersToDelete
		}

		// get all pools (and health monitors) associated with this loadbalancer
		var monitorIDs []string
		for _, listener := range listenerList {
			pool, err := openstackutil.GetPoolByListener(lbaas.lb, loadbalancer.ID, listener.ID)
			if err != nil && err != cpoerrors.ErrNotFound {
				return fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
			}
			if pool != nil {
				if pool.MonitorID != "" {
					monitorIDs = append(monitorIDs, pool.MonitorID)
				}
			}
		}

		// delete monitors
		for _, monitorID := range monitorIDs {
			klog.InfoS("Deleting health monitor", "monitorID", monitorID, "lbID", loadbalancer.ID)
			if err := openstackutil.DeleteHealthMonitor(lbaas.lb, monitorID, loadbalancer.ID); err != nil {
				return err
			}
			klog.InfoS("Deleted health monitor", "monitorID", monitorID, "lbID", loadbalancer.ID)
		}

		// delete listeners
		if err := lbaas.deleteListeners(loadbalancer.ID, listenerList); err != nil {
			return err
		}

		if needDeleteLB {
			// delete the loadbalancer in old way, i.e. no cascading.
			klog.InfoS("Deleting load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
			if err := openstackutil.DeleteLoadbalancer(lbaas.lb, loadbalancer.ID, false); err != nil {
				return err
			}
			klog.InfoS("Deleted load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
		}
	}

	// Remove the Service's tag from the load balancer.
	if !needDeleteLB && updateLBTag {
		var newTags []string
		for _, tag := range loadbalancer.Tags {
			if tag != lbName {
				newTags = append(newTags, tag)
			}
		}
		// An empty list won't trigger tags update.
		if len(newTags) == 0 {
			newTags = []string{""}
		}
		klog.InfoS("Updating load balancer tags", "lbID", loadbalancer.ID, "tags", newTags)
		if err := openstackutil.UpdateLoadBalancerTags(lbaas.lb, loadbalancer.ID, newTags); err != nil {
			return err
		}
		klog.InfoS("Updated load balancer tags", "lbID", loadbalancer.ID)
	}

	// Delete the Security Group
	if lbaas.opts.ManageSecurityGroups {
		if err := lbaas.EnsureSecurityGroupDeleted(clusterName, service); err != nil {
			return err
		}
	}

	return nil
}

// EnsureSecurityGroupDeleted deleting security group for specific loadbalancer service.
func (lbaas *LbaasV2) EnsureSecurityGroupDeleted(_ string, service *corev1.Service) error {
	// Generate Name
	lbSecGroupName := getSecurityGroupName(service)
	lbSecGroupID, err := secgroups.IDFromName(lbaas.network, lbSecGroupName)
	if err != nil {
		if isSecurityGroupNotFound(err) {
			// It is OK when the security group has been deleted by others.
			return nil
		}
		return fmt.Errorf("error occurred finding security group: %s: %v", lbSecGroupName, err)
	}

	if lbaas.opts.UseOctavia {
		// Disassociate the security group from the neutron ports on the nodes.
		if err := disassociateSecurityGroupForLB(lbaas.network, lbSecGroupID); err != nil {
			return fmt.Errorf("failed to disassociate security group %s: %v", lbSecGroupID, err)
		}
	}

	mc := metrics.NewMetricContext("security_group", "delete")
	lbSecGroup := groups.Delete(lbaas.network, lbSecGroupID)
	if lbSecGroup.Err != nil && !cpoerrors.IsNotFound(lbSecGroup.Err) {
		return mc.ObserveRequest(lbSecGroup.Err)
	}
	_ = mc.ObserveRequest(nil)

	if len(lbaas.opts.NodeSecurityGroupIDs) == 0 {
		// Just happen when nodes have not Security Group, or should not happen
		// UpdateLoadBalancer and EnsureLoadBalancer can set lbaas.opts.NodeSecurityGroupIDs when it is empty
		// And service controller call UpdateLoadBalancer to set lbaas.opts.NodeSecurityGroupIDs when controller manager service is restarted.
		klog.Warningf("Can not find node-security-group from all the nodes of this cluster when delete loadbalancer service %s/%s",
			service.Namespace, service.Name)
	} else {
		// Delete the rules in the Node Security Group
		for _, nodeSecurityGroupID := range lbaas.opts.NodeSecurityGroupIDs {
			opts := rules.ListOpts{
				SecGroupID:    nodeSecurityGroupID,
				RemoteGroupID: lbSecGroupID,
			}
			secGroupRules, err := getSecurityGroupRules(lbaas.network, opts)

			if err != nil && !cpoerrors.IsNotFound(err) {
				msg := fmt.Sprintf("error finding rules for remote group id %s in security group id %s: %v", lbSecGroupID, nodeSecurityGroupID, err)
				return fmt.Errorf(msg)
			}

			for _, rule := range secGroupRules {
				mc := metrics.NewMetricContext("security_group_rule", "delete")
				res := rules.Delete(lbaas.network, rule.ID)
				if res.Err != nil && !cpoerrors.IsNotFound(res.Err) {
					_ = mc.ObserveRequest(res.Err)
					return fmt.Errorf("error occurred deleting security group rule: %s: %v", rule.ID, res.Err)
				}
				_ = mc.ObserveRequest(nil)
			}
		}
	}

	return nil
}

// IsAllowAll checks whether the netsets.IPNet allows traffic from 0.0.0.0/0
func IsAllowAll(ipnets netsets.IPNet) bool {
	for _, s := range ipnets.StringSlice() {
		if s == "0.0.0.0/0" {
			return true
		}
	}
	return false
}

// GetLoadBalancerSourceRanges first try to parse and verify LoadBalancerSourceRanges field from a service.
// If the field is not specified, turn to parse and verify the AnnotationLoadBalancerSourceRangesKey annotation from a service,
// extracting the source ranges to allow, and if not present returns a default (allow-all) value.
func GetLoadBalancerSourceRanges(service *corev1.Service, preferredIPFamily corev1.IPFamily) (netsets.IPNet, error) {
	var ipnets netsets.IPNet
	var err error
	// if SourceRange field is specified, ignore sourceRange annotation
	if len(service.Spec.LoadBalancerSourceRanges) > 0 {
		specs := service.Spec.LoadBalancerSourceRanges
		ipnets, err = netsets.ParseIPNets(specs...)

		if err != nil {
			return nil, fmt.Errorf("service.Spec.LoadBalancerSourceRanges: %v is not valid. Expecting a list of IP ranges. For example, 10.0.0.0/24. Error msg: %v", specs, err)
		}
	} else {
		val := service.Annotations[corev1.AnnotationLoadBalancerSourceRangesKey]
		val = strings.TrimSpace(val)
		if val == "" {
			if preferredIPFamily == corev1.IPv6Protocol {
				val = defaultLoadBalancerSourceRangesIPv6
			} else {
				val = defaultLoadBalancerSourceRangesIPv4
			}
		}
		specs := strings.Split(val, ",")
		ipnets, err = netsets.ParseIPNets(specs...)
		if err != nil {
			return nil, fmt.Errorf("%s: %s is not valid. Expecting a comma-separated list of source IP ranges. For example, 10.0.0.0/24,192.168.2.0/24", corev1.AnnotationLoadBalancerSourceRangesKey, val)
		}
	}
	return ipnets, nil
}

// PreserveGopherError preserves the error details delivered with the response
// that are explicitly discarded by dedicated error types.
// The gopher library, because of an unknown reason, explicitly hides
// the detailed error information from the response body and replaces it
// with a generic phrase that does not help to identify the problem anymore.
// This method resurrects the error message from the response body for
// such cases. For example for an 404 Error the provided message just
// tells `Resource not found`, which is not helpful, because it hides
// the real error information, which might be something completely different.
// error types from provider_client.go
func PreserveGopherError(rawError error) error {
	if rawError == nil {
		return nil
	}
	if v, ok := rawError.(gophercloud.ErrErrorAfterReauthentication); ok {
		rawError = v.ErrOriginal
	}
	var details []byte
	switch e := rawError.(type) {
	case gophercloud.ErrDefault400:
	case gophercloud.ErrDefault401:
		details = e.Body
	case gophercloud.ErrDefault403:
	case gophercloud.ErrDefault404:
		details = e.Body
	case gophercloud.ErrDefault405:
		details = e.Body
	case gophercloud.ErrDefault408:
		details = e.Body
	case gophercloud.ErrDefault409:
	case gophercloud.ErrDefault429:
		details = e.Body
	case gophercloud.ErrDefault500:
		details = e.Body
	case gophercloud.ErrDefault503:
		details = e.Body
	default:
		return rawError
	}

	if details != nil {
		return fmt.Errorf("%s: %s", rawError, details)
	}
	return rawError
}
