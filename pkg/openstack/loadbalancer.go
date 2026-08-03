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
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/keymanager/v1/containers"
	"github.com/gophercloud/gophercloud/v2/openstack/keymanager/v1/secrets"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	v2monitors "github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/monitors"
	v2pools "github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"
	"k8s.io/utils/ptr"

	"k8s.io/cloud-provider-openstack/pkg/metrics"
	cpoutil "k8s.io/cloud-provider-openstack/pkg/util"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	netsets "k8s.io/cloud-provider-openstack/pkg/util/net/sets"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

// Note: when creating a new Loadbalancer (VM), it can take some time before it is ready for use,
// this timeout is used for waiting until the Loadbalancer provisioning status goes to ACTIVE state.
const (
	defaultLoadBalancerSourceRangesIPv4 = "0.0.0.0/0"
	defaultLoadBalancerSourceRangesIPv6 = "::/0"
	activeStatus                        = "ACTIVE"
	errorStatus                         = "ERROR"
	annotationXForwardedFor             = "X-Forwarded-For"

	ServiceAnnotationLoadBalancerInternal             = "service.beta.kubernetes.io/openstack-internal-load-balancer"
	ServiceAnnotationLoadBalancerNodeSelector         = "loadbalancer.openstack.org/node-selector"
	ServiceAnnotationLoadBalancerConnLimit            = "loadbalancer.openstack.org/connection-limit"
	ServiceAnnotationLoadBalancerFloatingNetworkID    = "loadbalancer.openstack.org/floating-network-id"
	ServiceAnnotationLoadBalancerFloatingSubnet       = "loadbalancer.openstack.org/floating-subnet"
	ServiceAnnotationLoadBalancerFloatingSubnetID     = "loadbalancer.openstack.org/floating-subnet-id"
	ServiceAnnotationLoadBalancerFloatingSubnetTags   = "loadbalancer.openstack.org/floating-subnet-tags"
	ServiceAnnotationLoadBalancerClass                = "loadbalancer.openstack.org/class"
	ServiceAnnotationLoadBalancerKeepFloatingIP       = "loadbalancer.openstack.org/keep-floatingip"
	ServiceAnnotationLoadBalancerPortID               = "loadbalancer.openstack.org/port-id"
	ServiceAnnotationLoadBalancerLbMethod             = "loadbalancer.openstack.org/lb-method"
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
	ServiceAnnotationLoadBalancerEnableHealthMonitor         = "loadbalancer.openstack.org/enable-health-monitor"
	ServiceAnnotationLoadBalancerHealthMonitorDelay          = "loadbalancer.openstack.org/health-monitor-delay"
	ServiceAnnotationLoadBalancerHealthMonitorTimeout        = "loadbalancer.openstack.org/health-monitor-timeout"
	ServiceAnnotationLoadBalancerHealthMonitorMaxRetries     = "loadbalancer.openstack.org/health-monitor-max-retries"
	ServiceAnnotationLoadBalancerHealthMonitorMaxRetriesDown = "loadbalancer.openstack.org/health-monitor-max-retries-down"
	ServiceAnnotationLoadBalancerLoadbalancerHostname        = "loadbalancer.openstack.org/hostname"
	ServiceAnnotationLoadBalancerAddress                     = "loadbalancer.openstack.org/load-balancer-address"
	// revive:disable:var-naming
	ServiceAnnotationTlsContainerRef = "loadbalancer.openstack.org/default-tls-container-ref"
	// revive:enable:var-naming
	// See https://nip.io
	defaultProxyHostnameSuffix      = "nip.io"
	ServiceAnnotationLoadBalancerID = "loadbalancer.openstack.org/load-balancer-id"

	// Octavia resources name formats
	servicePrefix  = "kube_service_"
	lbFormat       = "%s%s_%s_%s"
	listenerPrefix = "listener_"
	listenerFormat = listenerPrefix + "%d_%s"
	poolPrefix     = "pool_"
	poolFormat     = poolPrefix + "%d_%s"
	monitorPrefix  = "monitor_"
	monitorFormat  = monitorPrefix + "%d_%s"
)

// LbaasV2 is a LoadBalancer implementation based on Octavia
type LbaasV2 struct {
	LoadBalancer
}

var _ cloudprovider.LoadBalancer = &LbaasV2{}

// serviceConfig contains configurations for creating a Service.
type serviceConfig struct {
	internal                    bool
	connLimit                   int
	configClassName             string
	lbNetworkID                 string
	lbSubnetID                  string
	lbMemberSubnetID            string
	lbPublicNetworkID           string
	lbPublicSubnetSpec          *floatingSubnetSpec
	nodeSelectors               map[string]string
	keepClientIP                bool
	poolLbMethod                string
	proxyProtocolVersion        *v2pools.Protocol
	timeoutClientData           int
	timeoutMemberConnect        int
	timeoutMemberData           int
	timeoutTCPInspect           int
	allowedCIDR                 []string
	enableMonitor               bool
	flavorID                    string
	availabilityZone            string
	tlsContainerRef             string
	lbID                        string
	lbName                      string
	supportLBTags               bool
	healthCheckNodePort         int
	healthMonitorDelay          int
	healthMonitorTimeout        int
	healthMonitorMaxRetries     int
	healthMonitorMaxRetriesDown int
	preferredIPFamily           corev1.IPFamily // preferred (the first) IP family indicated in service's `spec.ipFamilies`
}

type listenerKey struct {
	Protocol listeners.Protocol
	Port     int
}

// getLoadbalancerByName get the load balancer which is in valid status by the given name/legacy name.
func getLoadbalancerByName(ctx context.Context, client *gophercloud.ServiceClient, name string, legacyName string) (*loadbalancers.LoadBalancer, error) {
	var validLBs []loadbalancers.LoadBalancer

	opts := loadbalancers.ListOpts{
		Name: name,
	}
	allLoadbalancers, err := openstackutil.GetLoadBalancers(ctx, client, opts)
	if err != nil {
		return nil, err
	}

	if len(allLoadbalancers) == 0 {
		if len(legacyName) > 0 {
			// Backoff to get load balnacer by legacy name.
			opts := loadbalancers.ListOpts{
				Name: legacyName,
			}
			allLoadbalancers, err = openstackutil.GetLoadBalancers(ctx, client, opts)
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

func popListener(existingListeners []listeners.Listener, id string) []listeners.Listener {
	newListeners := []listeners.Listener{}
	for _, existingListener := range existingListeners {
		if existingListener.ID != id {
			newListeners = append(newListeners, existingListener)
		}
	}
	return newListeners
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

func (lbaas *LbaasV2) createOctaviaLoadBalancer(ctx context.Context, name, clusterName string, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) (*loadbalancers.LoadBalancer, error) {
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

	lbaas.applyVIPCreateOpts(service, svcConf, &createOpts)

	// For external load balancer, the LoadBalancerIP is a public IP address.
	loadBalancerIP := service.Spec.LoadBalancerIP
	if loadBalancerIP != "" {
		if svcConf.internal || (svcConf.preferredIPFamily == corev1.IPv6Protocol) {
			createOpts.VipAddress = loadBalancerIP
		}
	}

	if !lbaas.opts.ProviderRequiresSerialAPICalls {
		listenerOpts, err := lbaas.buildListenersCreateOpts(ctx, service, nodes, svcConf, name)
		if err != nil {
			return nil, err
		}
		createOpts.Listeners = listenerOpts
	}

	mc := metrics.NewMetricContext("loadbalancer", "create")
	loadbalancer, err := loadbalancers.Create(ctx, lbaas.lb, createOpts).Extract()
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

	if loadbalancer, err = openstackutil.WaitActiveAndGetLoadBalancer(ctx, lbaas.lb, loadbalancer.ID); err != nil {
		if loadbalancer != nil && loadbalancer.ProvisioningStatus == errorStatus {
			// If LB landed in ERROR state we should delete it and retry the creation later.
			if err = lbaas.deleteLoadBalancer(ctx, loadbalancer, service, svcConf, true); err != nil {
				return nil, fmt.Errorf("loadbalancer %s is in ERROR state and there was an error when removing it: %v", loadbalancer.ID, err)
			}
			return nil, fmt.Errorf("loadbalancer %s has gone into ERROR state, please check Octavia for details. Load balancer was "+
				"deleted and its creation will be retried", loadbalancer.ID)
		}
		return nil, err
	}

	return loadbalancer, nil
}

// applyVIPCreateOpts sets the VIP addressing on createOpts: a pinned VIP port when configured, otherwise the
// VIP subnet and network resolved from the load balancer class, falling back to the service config.
func (lbaas *LbaasV2) applyVIPCreateOpts(service *corev1.Service, svcConf *serviceConfig, createOpts *loadbalancers.CreateOpts) {
	vipPort := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerPortID, "")
	if vipPort != "" {
		createOpts.VipPortID = vipPort
		return
	}

	lbClass := lbaas.opts.LBClasses[svcConf.configClassName]

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

// buildListenersCreateOpts builds the listener create options for each service port, each with its pool,
// members, and (when enabled) a health monitor.
func (lbaas *LbaasV2) buildListenersCreateOpts(ctx context.Context, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig, name string) ([]listeners.CreateOpts, error) {
	var listenerOpts []listeners.CreateOpts
	for portIndex, port := range service.Spec.Ports {
		listenerCreateOpt := lbaas.buildListenerCreateOpt(ctx, port, svcConf, cpoutil.Sprintf255(listenerFormat, portIndex, name))
		members, newMembers, err := lbaas.buildCreateMemberOpts(ctx, port, nodes, svcConf)
		if err != nil {
			return nil, err
		}

		poolCreateOpt := lbaas.buildPoolCreateOpt(string(listenerCreateOpt.Protocol), service, svcConf, cpoutil.Sprintf255(poolFormat, portIndex, name))
		poolCreateOpt.Members = members

		// Pool name must be provided to create fully populated loadbalancer
		var withHealthMonitor string
		if svcConf.enableMonitor {
			opts := lbaas.buildMonitorCreateOpts(ctx, svcConf, port, cpoutil.Sprintf255(monitorFormat, portIndex, name))
			poolCreateOpt.Monitor = &opts
			withHealthMonitor = " with healthmonitor"
		}

		listenerCreateOpt.DefaultPool = &poolCreateOpt
		listenerOpts = append(listenerOpts, listenerCreateOpt)
		klog.V(2).Infof("Loadbalancer %s: adding pool%s using protocol %s with %d members", name, withHealthMonitor, poolCreateOpt.Protocol, len(newMembers))
	}

	return listenerOpts, nil
}

// GetLoadBalancer returns whether the specified load balancer exists and its status
func (lbaas *LbaasV2) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (*corev1.LoadBalancerStatus, bool, error) {
	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.getLoadBalancerLegacyName(service)
	lbID := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")
	var loadbalancer *loadbalancers.LoadBalancer
	var err error

	if lbID != "" {
		loadbalancer, err = openstackutil.GetLoadbalancerByID(ctx, lbaas.lb, lbID)
	} else {
		loadbalancer, err = getLoadbalancerByName(ctx, lbaas.lb, name, legacyName)
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
		floatIP, err := openstackutil.GetFloatingIPByPortID(ctx, lbaas.network, portID)
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
	return cpoutil.Sprintf255(lbFormat, servicePrefix, clusterName, service.Namespace, service.Name)
}

// getLoadBalancerLegacyName returns the legacy load balancer name for backward compatibility.
func (lbaas *LbaasV2) getLoadBalancerLegacyName(service *corev1.Service) string {
	return cloudprovider.DefaultLoadBalancerName(service)
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

// getKeyValueFromServiceAnnotation converts a comma-separated list of key-value
// pairs from the specified annotation into a map or returns the specified
// defaultSetting if the annotation is empty
func getKeyValueFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting string) map[string]string {
	annotationValue := getStringFromServiceAnnotation(service, annotationKey, defaultSetting)
	return cpoutil.StringToMap(annotationValue)
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
// If the annotation is not found or is not a valid boolean ("true" or "false"), it falls back to the defaultSetting and logs a message accordingly.
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
			klog.Infof("Found a non-boolean Service Annotation: %v = %v (falling back to default setting: %v)", annotationKey, annotationValue, defaultSetting)
			return defaultSetting
		}

		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, returnValue)
		return returnValue
	}
	klog.V(4).Infof("Could not find a Service Annotation; falling back to default setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting
}

// getProxyProtocolFromServiceAnnotation searches a given v1.Service the ServiceAnnotationLoadBalancerProxyEnabled to guess if the proxyProtocol needs to be
// enabled and return the ProxyProtocol's version which is need to be applied
func getProxyProtocolFromServiceAnnotation(service *corev1.Service) *v2pools.Protocol {
	switch getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerProxyEnabled, "false") {
	case "true":
		return ptr.To(v2pools.ProtocolPROXY)
	case "v1":
		return ptr.To(v2pools.ProtocolPROXY)
	case "v2":
		return ptr.To(v2pools.ProtocolPROXYV2)
	default:
		return nil
	}
}

// getSubnetIDForLB returns subnet-id for a specific node
func getSubnetIDForLB(ctx context.Context, network *gophercloud.ServiceClient, node corev1.Node, preferredIPFamily corev1.IPFamily) (string, error) {
	ipAddress, err := nodeAddressForLB(&node, preferredIPFamily)
	if err != nil {
		return "", err
	}

	instanceID, _, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return "", fmt.Errorf("can't determine instance ID from ProviderID when autodetecting LB subnet: %w", err)
	}

	ports, err := getAttachedPorts(ctx, network, instanceID)
	if err != nil {
		return "", err
	}

	for _, port := range ports {
		for _, fixedIP := range port.FixedIPs {
			if fixedIP.IPAddress == ipAddress {
				return fixedIP.SubnetID, nil
			}
		}
	}

	return "", cpoerrors.ErrNotFound
}

// isPortMember returns true if IP and subnetID are one of the FixedIPs on the port
func isPortMember(port PortWithPortSecurity, ip string, subnetID string) bool {
	for _, fixedIP := range port.FixedIPs {
		if (subnetID == "" || subnetID == fixedIP.SubnetID) && ip == fixedIP.IPAddress {
			return true
		}
	}
	return false
}

// deleteListeners deletes all given listeners and their default pools.
func (lbaas *LbaasV2) deleteListeners(ctx context.Context, lbID string, listenerList []listeners.Listener) error {
	for _, listener := range listenerList {
		if err := lbaas.deleteListenerWithPool(ctx, lbID, listener); err != nil {
			return err
		}
	}

	return nil
}

// deleteListenerWithPool deletes a listener and its default pool.
func (lbaas *LbaasV2) deleteListenerWithPool(ctx context.Context, lbID string, listener listeners.Listener) error {
	klog.InfoS("Deleting listener", "listenerID", listener.ID, "lbID", lbID)

	pool, err := openstackutil.GetPoolByListener(ctx, lbaas.lb, lbID, listener.ID)
	if err != nil && err != cpoerrors.ErrNotFound {
		return fmt.Errorf("error getting pool for obsolete listener %s: %v", listener.ID, err)
	}
	if pool != nil {
		klog.InfoS("Deleting pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)
		// Delete pool automatically deletes all its members.
		if err := openstackutil.DeletePool(ctx, lbaas.lb, pool.ID, lbID); err != nil {
			return err
		}
		klog.InfoS("Deleted pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)
	}

	if err := openstackutil.DeleteListener(ctx, lbaas.lb, listener.ID, lbID); err != nil {
		return err
	}
	klog.InfoS("Deleted listener", "listenerID", listener.ID, "lbID", lbID)

	return nil
}

// deleteServiceListeners deletes only the listeners owned by the current Service, skipping those belonging to other Services sharing the load balancer.
func (lbaas *LbaasV2) deleteServiceListeners(ctx context.Context, lbID string, listenerList []listeners.Listener, isLBOwner bool, lbName string) error {
	for _, listener := range listenerList {
		// If the listener was created by this Service before or after supporting shared LB.
		if !isListenerOwnedByService(listener, isLBOwner, lbName) {
			// This listener is created and managed by others, shouldn't delete.
			klog.V(4).InfoS("Ignoring the listener used by others", "listenerID", listener.ID, "loadbalancerID", lbID, "tags", listener.Tags)
			continue
		}
		if err := lbaas.deleteListenerWithPool(ctx, lbID, listener); err != nil {
			return err
		}
	}

	return nil
}

// isListenerOwnedByService reports whether the listener belongs to the current Service.
func isListenerOwnedByService(listener listeners.Listener, isLBOwner bool, lbName string) bool {
	return slices.Contains(listener.Tags, lbName) || (len(listener.Tags) == 0 && isLBOwner)
}

func (lbaas *LbaasV2) createFloatingIP(ctx context.Context, msg string, floatIPOpts floatingips.CreateOpts) (*floatingips.FloatingIP, error) {
	klog.V(4).Infof("%s floating ip with opts %+v", msg, floatIPOpts)
	mc := metrics.NewMetricContext("floating_ip", "create")
	floatIP, err := floatingips.Create(ctx, lbaas.network, floatIPOpts).Extract()
	err = PreserveGopherError(err)
	if mc.ObserveRequest(err) != nil {
		return floatIP, fmt.Errorf("error creating LB floatingip: %v", err)
	}
	return floatIP, err
}

func (lbaas *LbaasV2) updateFloatingIP(ctx context.Context, floatingip *floatingips.FloatingIP, portID *string) (*floatingips.FloatingIP, error) {
	floatUpdateOpts := floatingips.UpdateOpts{
		PortID: portID,
	}
	if portID != nil {
		klog.V(4).Infof("Attaching floating ip %q to loadbalancer port %q", floatingip.FloatingIP, *portID)
	} else {
		klog.V(4).Infof("Detaching floating ip %q from port %q", floatingip.FloatingIP, floatingip.PortID)
	}
	mc := metrics.NewMetricContext("floating_ip", "update")
	floatingip, err := floatingips.Update(ctx, lbaas.network, floatingip.ID, floatUpdateOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, fmt.Errorf("error updating LB floatingip %+v: %v", floatUpdateOpts, err)
	}
	return floatingip, nil
}

// ensureFloatingIP manages a FIP for a Service and returns the address that should be advertised in the
// .Status.LoadBalancer. In particular it will:
//  1. Lookup if any FIP is already attached to the VIP port of the LB.
//     a) If it is and Service is internal, it will attempt to detach the FIP and delete it if it was created
//     by cloud provider. This is to support cases of changing the internal annotation.
//     b) If the Service is not the owner of the LB it will not contiue to prevent accidental exposure of the
//     possible internal Services already existing on that LB.
//     c) If it's external Service, it will use that existing FIP.
//  2. Lookup FIP specified in Spec.LoadBalancerIP and try to assign it to the LB VIP port.
//  3. Try to create and assign a new FIP:
//     a) If Spec.LoadBalancerIP is not set, just create a random FIP in the external network and use that.
//     b) If Spec.LoadBalancerIP is specified, try to create a FIP with that address. By default this is not allowed by
//     the Neutron policy for regular users!
func (lbaas *LbaasV2) ensureFloatingIP(ctx context.Context, clusterName string, service *corev1.Service, lb *loadbalancers.LoadBalancer, svcConf *serviceConfig, isLBOwner bool) (string, error) {
	// first attempt: if a FIP is already attached to the LB's VIP port, we'll be using that.
	portID := lb.VipPortID
	floatIP, err := openstackutil.GetFloatingIPByPortID(ctx, lbaas.network, portID)
	if err != nil {
		return "", fmt.Errorf("failed when getting floating IP for port %s: %w", portID, err)
	}

	if floatIP != nil {
		klog.V(4).Infof("Found floating ip %v by loadbalancer port id %q", floatIP, portID)
	}

	if svcConf.internal && isLBOwner {
		if err := lbaas.releaseFloatingIPForInternalLB(ctx, service, floatIP, portID); err != nil {
			return "", err
		}
		return lb.VipAddress, nil
	}

	// we cannot add a FIP to a shared LB when we're a secondary Service or we risk adding it to an internal
	// Service and exposing it to the world unintentionally.
	if floatIP == nil && !isLBOwner {
		return "", fmt.Errorf("cannot attach a floating IP to a load balancer for a shared Service %s/%s, only owner Service can do that",
			service.Namespace, service.Name)
	}

	// second attempt: adopt the floating IP specified in service Spec.LoadBalancerIP, if any.
	loadBalancerIP := service.Spec.LoadBalancerIP
	if floatIP == nil && loadBalancerIP != "" {
		floatIP, err = lbaas.adoptFloatingIP(ctx, loadBalancerIP, portID)
		if err != nil {
			return "", err
		}
	}

	// third attempt: create a new floating IP
	if floatIP == nil {
		floatIP, err = lbaas.createFloatingIPForLB(ctx, clusterName, service, lb, svcConf, loadBalancerIP, portID)
		if err != nil {
			return "", err
		}
	}

	if floatIP != nil {
		return floatIP.FloatingIP, nil
	}

	return lb.VipAddress, nil
}

// releaseFloatingIPForInternalLB removes the floating IP from an internal load balancer owned by the Service:
// it deletes the IP if the provider created it (unless the keep-floatingip annotation is set), and otherwise
// detaches it from the VIP port. It is a no-op when no floating IP is attached.
func (lbaas *LbaasV2) releaseFloatingIPForInternalLB(ctx context.Context, service *corev1.Service, floatIP *floatingips.FloatingIP, portID string) error {
	if floatIP == nil {
		return nil
	}

	fipDeleted := false
	if !getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerKeepFloatingIP, false) {
		klog.V(4).Infof("Deleting floating IP %v attached to loadbalancer port id %q for internal service %s", floatIP, portID, serviceName(service))
		deleted, err := lbaas.deleteFIPIfCreatedByProvider(ctx, floatIP, portID, service)
		if err != nil {
			return err
		}
		fipDeleted = deleted
	}

	// If the FIP wasn't deleted (keep-floatingip annotation or not created by us) we still detach it.
	if !fipDeleted {
		if _, err := lbaas.updateFloatingIP(ctx, floatIP, nil); err != nil {
			return err
		}
	}

	return nil
}

// adoptFloatingIP looks up an existing floating IP by address and, if it is not attached to any port, associates
// it with the given VIP port. It returns nil when no such floating IP exists, and an error when it is already in use.
func (lbaas *LbaasV2) adoptFloatingIP(ctx context.Context, loadBalancerIP, portID string) (*floatingips.FloatingIP, error) {
	existingIPs, err := openstackutil.GetFloatingIPs(ctx, lbaas.network, floatingips.ListOpts{FloatingIP: loadBalancerIP})
	if err != nil {
		return nil, fmt.Errorf("failed when trying to get existing floating IP %s, error: %w", loadBalancerIP, err)
	}

	klog.V(4).Infof("Found floating ips %v by loadbalancer ip %q", existingIPs, loadBalancerIP)

	if len(existingIPs) == 0 {
		return nil, nil
	}

	floatingIP := existingIPs[0]
	if len(floatingIP.PortID) != 0 {
		return nil, fmt.Errorf("floating IP %s is not available", loadBalancerIP)
	}

	return lbaas.updateFloatingIP(ctx, &floatingIP, &portID)
}

// createFloatingIPForLB creates a floating IP for the load balancer's VIP port, or returns nil when no floating network is configured.
func (lbaas *LbaasV2) createFloatingIPForLB(ctx context.Context, clusterName string, service *corev1.Service, lb *loadbalancers.LoadBalancer, svcConf *serviceConfig, loadBalancerIP, portID string) (*floatingips.FloatingIP, error) {
	serviceName := serviceName(service)

	if svcConf.lbPublicNetworkID == "" {
		msg := "Floating network configuration not provided for Service %s, forcing to ensure an internal load balancer service"
		lbaas.eventRecorder.Eventf(service, corev1.EventTypeWarning, eventLBForceInternal, msg, serviceName)
		klog.Warningf(msg, serviceName)
		return nil, nil
	}

	klog.V(2).Infof("Creating floating IP %s for loadbalancer %s", loadBalancerIP, lb.ID)

	floatIPOpts := floatingips.CreateOpts{
		FloatingNetworkID: svcConf.lbPublicNetworkID,
		PortID:            portID,
		Description:       fmt.Sprintf("Floating IP for Kubernetes external service %s from cluster %s", serviceName, clusterName),
	}

	if loadBalancerIP == "" && svcConf.lbPublicSubnetSpec.matcherConfigured() {
		return lbaas.createFloatingIPInSubnet(ctx, lb, svcConf, floatIPOpts)
	}

	if svcConf.lbPublicSubnetSpec != nil {
		floatIPOpts.SubnetID = svcConf.lbPublicSubnetSpec.subnetID
	}
	floatIPOpts.FloatingIP = loadBalancerIP
	floatIP, err := lbaas.createFloatingIP(ctx, "Creating", floatIPOpts)
	if err != nil {
		return nil, err
	}

	klog.V(2).Infof("Successfully created floating IP %s for loadbalancer %s", floatIP.FloatingIP, lb.ID)

	return floatIP, nil
}

// createFloatingIPInSubnet lists the subnets matching the configured subnet spec and creates the
// floating IP in the first one that accepts it, returning an error if none is usable.
func (lbaas *LbaasV2) createFloatingIPInSubnet(ctx context.Context, lb *loadbalancers.LoadBalancer, svcConf *serviceConfig, floatIPOpts floatingips.CreateOpts) (*floatingips.FloatingIP, error) {
	foundSubnets, err := svcConf.lbPublicSubnetSpec.listSubnetsForNetwork(ctx, lbaas, svcConf.lbPublicNetworkID)
	if err != nil {
		return nil, err
	}

	if len(foundSubnets) == 0 {
		return nil, fmt.Errorf("no subnet matching %s found for network %s",
			svcConf.lbPublicSubnetSpec, svcConf.lbPublicNetworkID)
	}

	klog.V(4).Infof("found %d subnets matching %s for network %s", len(foundSubnets),
		svcConf.lbPublicSubnetSpec, svcConf.lbPublicNetworkID)

	var floatIP *floatingips.FloatingIP
	var foundSubnet subnets.Subnet
	for _, subnet := range foundSubnets {
		floatIPOpts.SubnetID = subnet.ID
		floatIP, err = lbaas.createFloatingIP(ctx, fmt.Sprintf("Trying subnet %s for creating", subnet.Name), floatIPOpts)
		if err == nil {
			foundSubnet = subnet
			break
		}
		klog.V(2).Infof("cannot use subnet %s: %v", subnet.Name, err)
	}
	if err != nil {
		return nil, fmt.Errorf("no free subnet matching %q found for network %s (last error %w)",
			svcConf.lbPublicSubnetSpec, svcConf.lbPublicNetworkID, err)
	}

	klog.V(2).Infof("Successfully created floating IP %s for loadbalancer %s on subnet %s(%s)", floatIP.FloatingIP, lb.ID, foundSubnet.Name, foundSubnet.ID)

	return floatIP, nil
}

// poolProtocol returns the protocol the pool should use, forcing HTTP when client-IP or TLS termination is requested.
func (lbaas *LbaasV2) poolProtocol(listenerProtocol v2pools.Protocol, svcConf *serviceConfig) v2pools.Protocol {
	if svcConf.proxyProtocolVersion != nil {
		return *svcConf.proxyProtocolVersion
	}

	if (svcConf.keepClientIP || svcConf.tlsContainerRef != "") && listenerProtocol != v2pools.ProtocolHTTP {
		switch {
		case svcConf.keepClientIP && svcConf.tlsContainerRef != "":
			klog.V(4).Infof("Forcing to use %q protocol for pool because annotations %q %q are set", v2pools.ProtocolHTTP, ServiceAnnotationLoadBalancerXForwardedFor, ServiceAnnotationTlsContainerRef)
		case svcConf.keepClientIP:
			klog.V(4).Infof("Forcing to use %q protocol for pool because annotation %q is set", v2pools.ProtocolHTTP, ServiceAnnotationLoadBalancerXForwardedFor)
		default:
			klog.V(4).Infof("Forcing to use %q protocol for pool because annotations %q is set", v2pools.ProtocolHTTP, ServiceAnnotationTlsContainerRef)
		}
		return v2pools.ProtocolHTTP
	}

	return listenerProtocol
}

func (lbaas *LbaasV2) ensureOctaviaHealthMonitor(ctx context.Context, lbID string, name string, pool *v2pools.Pool, port corev1.ServicePort, svcConf *serviceConfig) error {
	monitorID := pool.MonitorID

	if monitorID == "" {
		// do nothing
		if !svcConf.enableMonitor {
			return nil
		}

		// a new monitor must be created
		klog.V(2).Infof("Creating monitor for pool %s", pool.ID)
		createOpts := lbaas.buildMonitorCreateOpts(ctx, svcConf, port, name)
		return lbaas.createOctaviaHealthMonitor(ctx, createOpts, pool.ID, lbID)
	}

	// an existing monitor must be deleted
	if !svcConf.enableMonitor {
		klog.Infof("Deleting health monitor %s for pool %s", monitorID, pool.ID)
		return openstackutil.DeleteHealthMonitor(ctx, lbaas.lb, monitorID, lbID)
	}

	// get an existing monitor status
	monitor, err := openstackutil.GetHealthMonitor(ctx, lbaas.lb, monitorID)
	if err != nil {
		// return err on 404 is ok, since we get monitorID dynamically from the pool
		return err
	}

	// recreate health monitor with a new type
	createOpts := lbaas.buildMonitorCreateOpts(ctx, svcConf, port, name)
	if createOpts.Type != monitor.Type {
		klog.InfoS("Recreating health monitor for the pool", "pool", pool.ID, "oldMonitor", monitorID)
		if err := openstackutil.DeleteHealthMonitor(ctx, lbaas.lb, monitorID, lbID); err != nil {
			return err
		}
		return lbaas.createOctaviaHealthMonitor(ctx, createOpts, pool.ID, lbID)
	}

	// update new monitor parameters
	if name != monitor.Name ||
		svcConf.healthMonitorDelay != monitor.Delay ||
		svcConf.healthMonitorTimeout != monitor.Timeout ||
		svcConf.healthMonitorMaxRetries != monitor.MaxRetries ||
		svcConf.healthMonitorMaxRetriesDown != monitor.MaxRetriesDown {
		updateOpts := v2monitors.UpdateOpts{
			Name:           &name,
			Delay:          svcConf.healthMonitorDelay,
			Timeout:        svcConf.healthMonitorTimeout,
			MaxRetries:     svcConf.healthMonitorMaxRetries,
			MaxRetriesDown: svcConf.healthMonitorMaxRetriesDown,
		}
		klog.Infof("Updating health monitor %s updateOpts %+v", monitorID, updateOpts)
		return openstackutil.UpdateHealthMonitor(ctx, lbaas.lb, monitorID, updateOpts, lbID)
	}

	return nil
}

func (lbaas *LbaasV2) canUseHTTPMonitor(ctx context.Context, port corev1.ServicePort) bool {
	if lbaas.opts.LBProvider == "ovn" {
		// ovn-octavia-provider doesn't support HTTP monitors at all. We got to avoid creating it with ovn.
		return false
	}

	if port.Protocol == corev1.ProtocolUDP {
		// Older Octavia versions or OVN provider doesn't support HTTP monitors on UDP pools. We got to check if that's the case.
		return openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureHTTPMonitorsOnUDP, lbaas.opts.LBProvider)
	}

	return true
}

// buildMonitorCreateOpts returns a v2monitors.CreateOpts without PoolID for consumption of both, fully popuplated Loadbalancers and Monitors.
func (lbaas *LbaasV2) buildMonitorCreateOpts(ctx context.Context, svcConf *serviceConfig, port corev1.ServicePort, name string) v2monitors.CreateOpts {
	opts := v2monitors.CreateOpts{
		Name:           name,
		Type:           string(port.Protocol),
		Delay:          svcConf.healthMonitorDelay,
		Timeout:        svcConf.healthMonitorTimeout,
		MaxRetries:     svcConf.healthMonitorMaxRetries,
		MaxRetriesDown: svcConf.healthMonitorMaxRetriesDown,
	}
	if port.Protocol == corev1.ProtocolUDP {
		opts.Type = "UDP-CONNECT"
	}
	if svcConf.healthCheckNodePort > 0 && lbaas.canUseHTTPMonitor(ctx, port) {
		opts.Type = "HTTP"
		opts.URLPath = "/healthz"
		opts.HTTPMethod = "GET"
		opts.ExpectedCodes = "200"
	}
	return opts
}

func (lbaas *LbaasV2) createOctaviaHealthMonitor(ctx context.Context, createOpts v2monitors.CreateOpts, poolID, lbID string) error {
	// populate PoolID, attribute is omitted for consumption of the createOpts for fully populated Loadbalancer
	createOpts.PoolID = poolID
	monitor, err := openstackutil.CreateHealthMonitor(ctx, lbaas.lb, createOpts, lbID)
	if err != nil {
		return err
	}
	klog.Infof("Health monitor %s for pool %s created.", monitor.ID, poolID)

	return nil
}

// Make sure the pool is created for the Service, nodes are added as pool members.
func (lbaas *LbaasV2) ensureOctaviaPool(ctx context.Context, lbID string, name string, listener *listeners.Listener, service *corev1.Service, port corev1.ServicePort, nodes []*corev1.Node, svcConf *serviceConfig) (*v2pools.Pool, error) {
	pool, err := openstackutil.GetPoolByListener(ctx, lbaas.lb, lbID, listener.ID)
	if err != nil && err != cpoerrors.ErrNotFound {
		return nil, fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
	}

	// By default, use the protocol of the listener
	poolProto := lbaas.poolProtocol(v2pools.Protocol(listener.Protocol), svcConf)

	// Delete the pool and its members if it already exists and has the wrong protocol
	if pool != nil && v2pools.Protocol(pool.Protocol) != poolProto {
		klog.InfoS("Deleting unused pool", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)

		// Delete pool automatically deletes all its members.
		if err := openstackutil.DeletePool(ctx, lbaas.lb, pool.ID, lbID); err != nil {
			return nil, err
		}
		pool = nil
	}

	// If LBMethod changes, update the Pool with the new value
	var poolLbMethod string
	if svcConf.poolLbMethod != "" {
		poolLbMethod = svcConf.poolLbMethod
	} else {
		// if LBMethod is not defined, fallback on default OCCM's default method
		poolLbMethod = lbaas.opts.LBMethod
	}
	if pool != nil && pool.LBMethod != poolLbMethod {
		klog.InfoS("Updating LoadBalancer LBMethod", "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)
		err = openstackutil.UpdatePool(ctx, lbaas.lb, lbID, pool.ID, v2pools.UpdateOpts{LBMethod: v2pools.LBMethod(poolLbMethod)})
		if err != nil {
			err = PreserveGopherError(err)
			msg := fmt.Sprintf("Error updating LB method for LoadBalancer: %v", err)
			klog.Errorf(msg, "poolID", pool.ID, "listenerID", listener.ID, "lbID", lbID)
			lbaas.eventRecorder.Event(service, corev1.EventTypeWarning, eventLBLbMethodUnknown, msg)
		} else {
			pool.LBMethod = poolLbMethod
		}
	}

	if pool == nil {
		createOpt := lbaas.buildPoolCreateOpt(listener.Protocol, service, svcConf, name)
		createOpt.ListenerID = listener.ID

		klog.InfoS("Creating pool", "listenerID", listener.ID, "protocol", createOpt.Protocol)
		pool, err = openstackutil.CreatePool(ctx, lbaas.lb, createOpt, lbID)
		if err != nil {
			return nil, err
		}
		klog.V(2).Infof("Pool %s created for listener %s", pool.ID, listener.ID)
	}

	if err := lbaas.reconcilePoolMembers(ctx, lbID, pool, port, nodes, svcConf); err != nil {
		return nil, err
	}

	return pool, nil
}

// reconcilePoolMembers updates the pool's members to match the current nodes, using serial API calls when
// the provider requires them, otherwise a single batch update when the member set has changed.
func (lbaas *LbaasV2) reconcilePoolMembers(ctx context.Context, lbID string, pool *v2pools.Pool, port corev1.ServicePort, nodes []*corev1.Node, svcConf *serviceConfig) error {
	if lbaas.opts.ProviderRequiresSerialAPICalls {
		klog.V(2).Infof("Using serial API calls to update members for pool %s", pool.ID)
		nodePort := int(port.NodePort)
		if err := openstackutil.SeriallyReconcilePoolMembers(ctx, lbaas.lb, pool, nodePort, lbID, nodes); err != nil {
			return err
		}
		return nil
	}

	curMembers := sets.New[string]()
	poolMembers, err := openstackutil.GetMembersbyPool(ctx, lbaas.lb, pool.ID)
	if err != nil {
		klog.Errorf("failed to get members in the pool %s: %v", pool.ID, err)
	}
	for _, m := range poolMembers {
		curMembers.Insert(fmt.Sprintf("%s-%s-%d-%d", m.Name, m.Address, m.ProtocolPort, m.MonitorPort))
	}

	members, newMembers, err := lbaas.buildBatchUpdateMemberOpts(ctx, port, nodes, svcConf)
	if err != nil {
		return err
	}

	if !curMembers.Equal(newMembers) {
		klog.V(2).Infof("Updating %d members for pool %s", len(members), pool.ID)
		if err = openstackutil.BatchUpdatePoolMembers(ctx, lbaas.lb, lbID, pool.ID, members); err != nil {
			return err
		}
		klog.V(2).Infof("Successfully updated %d members for pool %s", len(members), pool.ID)
	}

	return nil
}

func (lbaas *LbaasV2) buildPoolCreateOpt(listenerProtocol string, service *corev1.Service, svcConf *serviceConfig, name string) v2pools.CreateOpts {
	// By default, use the protocol of the listener
	poolProto := lbaas.poolProtocol(v2pools.Protocol(listenerProtocol), svcConf)

	affinity := service.Spec.SessionAffinity
	var persistence *v2pools.SessionPersistence
	switch affinity {
	case corev1.ServiceAffinityNone:
		persistence = nil
	case corev1.ServiceAffinityClientIP:
		persistence = &v2pools.SessionPersistence{Type: "SOURCE_IP"}
	}

	var lbMethod v2pools.LBMethod
	if svcConf.poolLbMethod != "" {
		lbMethod = v2pools.LBMethod(svcConf.poolLbMethod)
	} else {
		// if LBMethod is not defined, fallback on default OCCM's default method
		lbMethod = v2pools.LBMethod(lbaas.opts.LBMethod)
	}

	return v2pools.CreateOpts{
		Name:        name,
		Protocol:    poolProto,
		LBMethod:    lbMethod,
		Persistence: persistence,
	}
}

// buildBatchUpdateMemberOpts returns v2pools.BatchUpdateMemberOpts array for Services and Nodes alongside a list of member names
func (lbaas *LbaasV2) buildBatchUpdateMemberOpts(ctx context.Context, port corev1.ServicePort, nodes []*corev1.Node, svcConf *serviceConfig) ([]v2pools.BatchUpdateMemberOpts, sets.Set[string], error) {
	var members []v2pools.BatchUpdateMemberOpts
	newMembers := sets.New[string]()

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

		memberSubnetID := &svcConf.lbMemberSubnetID
		if memberSubnetID != nil && *memberSubnetID == "" {
			memberSubnetID = nil
		}

		if port.NodePort != 0 { // It's 0 when AllocateLoadBalancerNodePorts=False
			member := v2pools.BatchUpdateMemberOpts{
				Address:      addr,
				ProtocolPort: int(port.NodePort),
				Name:         &node.Name,
				SubnetID:     memberSubnetID,
			}
			if svcConf.healthCheckNodePort > 0 && lbaas.canUseHTTPMonitor(ctx, port) {
				member.MonitorPort = &svcConf.healthCheckNodePort
			}
			members = append(members, member)
			newMembers.Insert(fmt.Sprintf("%s-%s-%d-%d", node.Name, addr, member.ProtocolPort, svcConf.healthCheckNodePort))
		}
	}
	return members, newMembers, nil
}

func (lbaas *LbaasV2) buildCreateMemberOpts(ctx context.Context, port corev1.ServicePort, nodes []*corev1.Node, svcConf *serviceConfig) ([]v2pools.CreateMemberOpts, sets.Set[string], error) {
	batchUpdateMemberOpts, newMembers, err := lbaas.buildBatchUpdateMemberOpts(ctx, port, nodes, svcConf)
	if err != nil {
		return nil, nil, err
	}

	createMemberOpts := make([]v2pools.CreateMemberOpts, len(batchUpdateMemberOpts))
	for i := range batchUpdateMemberOpts {
		createMemberOpts[i] = v2pools.CreateMemberOpts{
			Address:        batchUpdateMemberOpts[i].Address,
			ProtocolPort:   batchUpdateMemberOpts[i].ProtocolPort,
			Name:           ptr.Deref(batchUpdateMemberOpts[i].Name, ""),
			ProjectID:      batchUpdateMemberOpts[i].ProjectID,
			Weight:         batchUpdateMemberOpts[i].Weight,
			SubnetID:       ptr.Deref(batchUpdateMemberOpts[i].SubnetID, ""),
			AdminStateUp:   batchUpdateMemberOpts[i].AdminStateUp,
			Backup:         batchUpdateMemberOpts[i].Backup,
			MonitorAddress: ptr.Deref(batchUpdateMemberOpts[i].MonitorAddress, ""),
			MonitorPort:    batchUpdateMemberOpts[i].MonitorPort,
			Tags:           batchUpdateMemberOpts[i].Tags,
		}
	}
	return createMemberOpts, newMembers, nil
}

// Make sure the listener is created for Service
func (lbaas *LbaasV2) ensureOctaviaListener(ctx context.Context, lbID string, name string, curListenerMapping map[listenerKey]*listeners.Listener, port corev1.ServicePort, svcConf *serviceConfig) (*listeners.Listener, error) {
	listener, isPresent := curListenerMapping[listenerKey{
		Protocol: getListenerProtocol(port.Protocol, svcConf),
		Port:     int(port.Port),
	}]
	if !isPresent {
		return lbaas.createListener(ctx, lbID, name, port, svcConf)
	}
	return lbaas.updateListener(ctx, lbID, listener, svcConf)
}

// createListener creates a new listener on the load balancer for the given Service port.
func (lbaas *LbaasV2) createListener(ctx context.Context, lbID string, name string, port corev1.ServicePort, svcConf *serviceConfig) (*listeners.Listener, error) {
	listenerCreateOpt := lbaas.buildListenerCreateOpt(ctx, port, svcConf, name)
	listenerCreateOpt.LoadbalancerID = lbID

	klog.V(2).Infof("Creating listener for port %d using protocol %s", int(port.Port), listenerCreateOpt.Protocol)

	listener, err := openstackutil.CreateListener(ctx, lbaas.lb, lbID, listenerCreateOpt)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener for loadbalancer %s: %w", lbID, err)
	}

	klog.V(2).Infof("Listener %s created for loadbalancer %s", listener.ID, lbID)

	return listener, nil
}

// updateListener reconciles an existing listener with the Service configuration, issuing an update only
// when one of its properties (tags, connection limit, headers, TLS, timeouts, allowed CIDRs) has changed.
func (lbaas *LbaasV2) updateListener(ctx context.Context, lbID string, listener *listeners.Listener, svcConf *serviceConfig) (*listeners.Listener, error) {
	listenerChanged := false
	updateOpts := listeners.UpdateOpts{}

	if svcConf.supportLBTags && !slices.Contains(listener.Tags, svcConf.lbName) {
		newTags := append(slices.Clone(listener.Tags), svcConf.lbName)
		updateOpts.Tags = &newTags
		listenerChanged = true
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
	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureTimeout, lbaas.opts.LBProvider) {
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
	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureVIPACL, lbaas.opts.LBProvider) {
		if !cpoutil.StringListEqual(svcConf.allowedCIDR, listener.AllowedCIDRs) {
			updateOpts.AllowedCIDRs = &svcConf.allowedCIDR
			listenerChanged = true
		}
	}

	if listenerChanged {
		klog.InfoS("Updating listener", "listenerID", listener.ID, "lbID", lbID, "updateOpts", updateOpts)
		if err := openstackutil.UpdateListener(ctx, lbaas.lb, lbID, listener.ID, updateOpts); err != nil {
			return nil, fmt.Errorf("failed to update listener %s of loadbalancer %s: %w", listener.ID, lbID, err)
		}
		klog.InfoS("Updated listener", "listenerID", listener.ID, "lbID", lbID)
	}

	return listener, nil
}

// buildListenerCreateOpt returns listeners.CreateOpts for a specific Service port and configuration
func (lbaas *LbaasV2) buildListenerCreateOpt(ctx context.Context, port corev1.ServicePort, svcConf *serviceConfig, name string) listeners.CreateOpts {
	listenerCreateOpt := listeners.CreateOpts{
		Name:         name,
		Protocol:     listeners.Protocol(port.Protocol),
		ProtocolPort: int(port.Port),
		ConnLimit:    &svcConf.connLimit,
	}

	if svcConf.supportLBTags {
		listenerCreateOpt.Tags = []string{svcConf.lbName}
	}

	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureTimeout, lbaas.opts.LBProvider) {
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

	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureVIPACL, lbaas.opts.LBProvider) {
		if len(svcConf.allowedCIDR) > 0 {
			listenerCreateOpt.AllowedCIDRs = svcConf.allowedCIDR
		}
	}
	return listenerCreateOpt
}

// resolveLBConfig returns the first non-empty value from the Service annotation, the loadbalancer class, or defaultValue.
func (lbaas *LbaasV2) resolveLBConfig(service *corev1.Service, annotationKey string, classGetter func(*LBClass) string, defaultValue string) (string, error) {
	if annotationValue := getStringFromServiceAnnotation(service, annotationKey, ""); annotationValue != "" {
		return annotationValue, nil
	}

	if configClassName := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, ""); configClassName != "" {
		lbClass := lbaas.opts.LBClasses[configClassName]
		if lbClass == nil {
			return "", fmt.Errorf("invalid loadbalancer class %q", configClassName)
		}
		if classValue := classGetter(lbClass); classValue != "" {
			return classValue, nil
		}
	}

	return defaultValue, nil
}

// getSubnetID gets the configured subnet-id from the different possible sources.
func (lbaas *LbaasV2) getSubnetID(service *corev1.Service) (string, error) {
	return lbaas.resolveLBConfig(service, ServiceAnnotationLoadBalancerSubnetID,
		func(c *LBClass) string { return c.SubnetID }, lbaas.opts.SubnetID)
}

// getNetworkID gets the configured network-id from the different possible sources.
func (lbaas *LbaasV2) getNetworkID(service *corev1.Service) (string, error) {
	return lbaas.resolveLBConfig(service, ServiceAnnotationLoadBalancerNetworkID,
		func(c *LBClass) string { return c.NetworkID }, lbaas.opts.NetworkID)
}

// getMemberSubnetID gets the configured member-subnet-id from the different possible sources.
func (lbaas *LbaasV2) getMemberSubnetID(service *corev1.Service) (string, error) {
	return lbaas.resolveLBConfig(service, ServiceAnnotationLoadBalancerMemberSubnetID,
		func(c *LBClass) string { return c.MemberSubnetID }, lbaas.opts.MemberSubnetID)
}

func (lbaas *LbaasV2) checkServiceUpdate(ctx context.Context, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) error {
	if len(service.Spec.Ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}
	serviceName := serviceName(service)

	setPreferredIPFamily(service, svcConf)

	memberSubnetID, err := lbaas.resolveMemberSubnetID(ctx, service, nodes, svcConf)
	if err != nil {
		return err
	}
	svcConf.lbMemberSubnetID = memberSubnetID

	return lbaas.makeSvcConf(ctx, serviceName, service, svcConf)
}

// resolveMemberSubnetID determines the subnet used to register members, trying in order: the member-subnet-id
// annotation, the configured SubnetID, the load balancer class, the subnet-id annotation, and finally the
// first node's subnet. It may set svcConf.configClassName as a side effect.
func (lbaas *LbaasV2) resolveMemberSubnetID(ctx context.Context, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) (string, error) {
	memberSubnetID, err := lbaas.getMemberSubnetID(service)
	if err != nil {
		return "", fmt.Errorf("unable to get member-subnet-id, %w", err)
	}
	if memberSubnetID != "" {
		return memberSubnetID, nil
	}

	if lbaas.opts.SubnetID != "" {
		return lbaas.opts.SubnetID, nil
	}

	svcConf.configClassName = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, "")
	if svcConf.configClassName != "" {
		lbClass := lbaas.opts.LBClasses[svcConf.configClassName]
		if lbClass == nil {
			return "", fmt.Errorf("invalid loadbalancer class %q", svcConf.configClassName)
		}
		return lbClass.SubnetID, nil
	}

	subnetID := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerSubnetID, lbaas.opts.SubnetID)
	if subnetID != "" {
		return subnetID, nil
	}
	if len(nodes) == 0 {
		return "", nil
	}

	subnetID, err = getSubnetIDForLB(ctx, lbaas.network, *nodes[0], svcConf.preferredIPFamily)
	if err != nil {
		return "", fmt.Errorf("no subnet-id found for service %s: %w", serviceName(service), err)
	}

	return subnetID, nil
}

func (lbaas *LbaasV2) checkServiceDelete(ctx context.Context, service *corev1.Service, svcConf *serviceConfig) error {
	svcConf.lbID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")
	svcConf.supportLBTags = openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureTags, lbaas.opts.LBProvider)

	// This affects the protocol of listener and pool
	svcConf.keepClientIP = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerXForwardedFor, false)
	svcConf.proxyProtocolVersion = getProxyProtocolFromServiceAnnotation(service)
	svcConf.tlsContainerRef = getStringFromServiceAnnotation(service, ServiceAnnotationTlsContainerRef, lbaas.opts.TlsContainerRef)

	return nil
}

func (lbaas *LbaasV2) checkService(ctx context.Context, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) error {
	serviceName := serviceName(service)

	if len(nodes) == 0 {
		return fmt.Errorf("there are no available nodes for LoadBalancer service %s", serviceName)
	}
	ports := service.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no service ports provided")
	}

	setPreferredIPFamily(service, svcConf)

	// If in the config file internal-lb=true, user is not allowed to create external service.
	if lbaas.opts.InternalLB {
		if !getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerInternal, false) {
			klog.V(3).InfoS("Enforcing internal LB", "annotation", true, "config", false)
		}
		svcConf.internal = true
	} else {
		svcConf.internal = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerInternal, lbaas.opts.InternalLB)
	}

	if err := lbaas.validateTLSConfig(ctx, service, svcConf); err != nil {
		return err
	}

	if err := lbaas.resolveNetworkConfig(ctx, service, nodes, svcConf); err != nil {
		return err
	}

	if err := lbaas.resolveFloatingIPConfig(ctx, service, svcConf); err != nil {
		return err
	}

	return lbaas.makeSvcConf(ctx, serviceName, service, svcConf)
}

// resolveNetworkConfig resolves the load balancer network, subnet, and member subnet for the service,
// falling back to the first node's subnet when neither network nor subnet is configured.
func (lbaas *LbaasV2) resolveNetworkConfig(ctx context.Context, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig) error {
	serviceName := serviceName(service)

	lbNetworkID, err := lbaas.getNetworkID(service)
	if err != nil {
		return fmt.Errorf("failed to get network id to create load balancer for service %s: %w", serviceName, err)
	}
	svcConf.lbNetworkID = lbNetworkID

	lbSubnetID, err := lbaas.getSubnetID(service)
	if err != nil {
		return fmt.Errorf("failed to get subnet to create load balancer for service %s: %w", serviceName, err)
	}
	svcConf.lbSubnetID = lbSubnetID

	// The member subnet defaults to the configured SubnetID, otherwise the LB subnet.
	svcConf.lbMemberSubnetID = svcConf.lbSubnetID
	if lbaas.opts.SubnetID != "" {
		svcConf.lbMemberSubnetID = lbaas.opts.SubnetID
	}

	// When neither network nor subnet is configured, autodetect the subnet from the first node
	// and use it for both the LB and its members.
	if svcConf.lbNetworkID == "" && svcConf.lbSubnetID == "" {
		subnetID, err := getSubnetIDForLB(ctx, lbaas.network, *nodes[0], svcConf.preferredIPFamily)
		if err != nil {
			return fmt.Errorf("failed to get subnet to create load balancer for service %s: %w", serviceName, err)
		}
		svcConf.lbSubnetID = subnetID
		svcConf.lbMemberSubnetID = subnetID
	}

	// An explicit member-subnet-id annotation overrides everything above.
	memberSubnetID, err := lbaas.getMemberSubnetID(service)
	if err != nil {
		return fmt.Errorf("unable to get member-subnet-id, %w", err)
	}
	if memberSubnetID != "" {
		svcConf.lbMemberSubnetID = memberSubnetID
	}

	return nil
}

// resolveFloatingIPConfig resolves the floating network and subnet configuration for an external load balancer.
func (lbaas *LbaasV2) resolveFloatingIPConfig(ctx context.Context, service *corev1.Service, svcConf *serviceConfig) error {
	if svcConf.internal {
		klog.V(4).Infof("Ensure an internal loadbalancer service")
		return nil
	}

	klog.V(4).Infof("Ensure an external loadbalancer service")

	lbClass, err := lbaas.resolveLBClass(service, svcConf)
	if err != nil {
		return err
	}

	serviceName := serviceName(service)
	floatingNetworkID := lbaas.resolveFloatingNetworkID(ctx, service, serviceName, lbClass)
	floatingSubnet := lbaas.resolveFloatingSubnet(service, lbClass)

	// A configured subnet must belong to the floating network; a mismatch is fatal.
	if err := lbaas.validateSubnetInNetwork(ctx, floatingNetworkID, floatingSubnet); err != nil {
		return err
	}

	svcConf.lbPublicNetworkID = floatingNetworkID
	if floatingSubnet.Configured() {
		klog.V(4).Infof("using subnet spec %+v for %s", floatingSubnet, serviceName)
		svcConf.lbPublicSubnetSpec = &floatingSubnet
	} else {
		klog.V(4).Infof("no subnet spec found for %s", serviceName)
	}

	return nil
}

// resolveLBClass returns the LBClass named by the loadbalancer-class annotation, or nil if unset.
func (lbaas *LbaasV2) resolveLBClass(service *corev1.Service, svcConf *serviceConfig) (*LBClass, error) {
	svcConf.configClassName = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerClass, "")
	if svcConf.configClassName == "" {
		return nil, nil
	}

	lbClass := lbaas.opts.LBClasses[svcConf.configClassName]
	if lbClass == nil {
		return nil, fmt.Errorf("invalid loadbalancer class %q", svcConf.configClassName)
	}

	klog.V(4).Infof("Found loadbalancer class %q with %+v", svcConf.configClassName, lbClass)

	return lbClass, nil
}

// resolveFloatingNetworkID resolves the floating network ID from the LB class, annotation, config, or autodetection.
func (lbaas *LbaasV2) resolveFloatingNetworkID(ctx context.Context, service *corev1.Service, serviceName string, lbClass *LBClass) string {
	if lbClass != nil && lbClass.FloatingNetworkID != "" {
		return lbClass.FloatingNetworkID
	}

	if id := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingNetworkID, lbaas.opts.FloatingNetworkID); id != "" {
		return id
	}

	id, err := openstackutil.GetFloatingNetworkID(ctx, lbaas.network)
	if err != nil {
		msg := "Failed to find floating-network-id for Service %s: %v"
		lbaas.eventRecorder.Eventf(service, corev1.EventTypeWarning, eventLBExternalNetworkSearchFailed, msg, serviceName, err)
		klog.Warningf(msg, serviceName, err)
		return ""
	}

	return id
}

// resolveFloatingSubnet resolves the floating subnet spec from the LB class, annotation, or config.
func (lbaas *LbaasV2) resolveFloatingSubnet(service *corev1.Service, lbClass *LBClass) floatingSubnetSpec {
	if lbClass != nil {
		if subnet := newFloatingSubnetSpec(lbClass.FloatingSubnetID, lbClass.FloatingSubnet, lbClass.FloatingSubnetTags); subnet.Configured() {
			return subnet
		}
	}

	subnet := newFloatingSubnetSpec(
		getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingSubnetID, ""),
		getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingSubnet, ""),
		getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFloatingSubnetTags, ""),
	)
	if subnet.Configured() {
		return subnet
	}

	return newFloatingSubnetSpec(lbaas.opts.FloatingSubnetID, lbaas.opts.FloatingSubnet, lbaas.opts.FloatingSubnetTags)
}

// validateSubnetInNetwork verifies that a configured floating subnet belongs to the floating network.
func (lbaas *LbaasV2) validateSubnetInNetwork(ctx context.Context, floatingNetworkID string, floatingSubnet floatingSubnetSpec) error {
	if floatingNetworkID == "" || floatingSubnet.subnetID == "" {
		return nil
	}

	mc := metrics.NewMetricContext("subnet", "get")
	subnet, err := subnets.Get(ctx, lbaas.network, floatingSubnet.subnetID).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf("failed to find subnet %q: %w", floatingSubnet.subnetID, err)
	}

	if subnet.NetworkID != floatingNetworkID {
		return fmt.Errorf("floating IP subnet %q doesn't belong to the network %q", floatingSubnet.subnetID, subnet.NetworkID)
	}

	return nil
}

// newFloatingSubnetSpec builds a floatingSubnetSpec, preferring an explicit subnet ID over a name + tags.
func newFloatingSubnetSpec(subnetID, subnet, subnetTags string) floatingSubnetSpec {
	if subnetID != "" {
		return floatingSubnetSpec{subnetID: subnetID}
	}

	return floatingSubnetSpec{subnet: subnet, subnetTags: subnetTags}
}

// validateTLSConfig resolves the TLS container reference and, for the barbican
// container store, verifies that the referenced container or secret exists.
func (lbaas *LbaasV2) validateTLSConfig(ctx context.Context, service *corev1.Service, svcConf *serviceConfig) error {
	serviceName := serviceName(service)

	svcConf.tlsContainerRef = getStringFromServiceAnnotation(service, ServiceAnnotationTlsContainerRef, lbaas.opts.TlsContainerRef)
	if svcConf.tlsContainerRef == "" {
		return nil
	}

	if lbaas.secret == nil {
		return fmt.Errorf("failed to create a TLS Terminated loadbalancer because openstack keymanager client is not "+
			"initialized and default-tls-container-ref %q is set", svcConf.tlsContainerRef)
	}

	// check if container or secret exists for 'barbican' container store
	// tls container ref has the format: https://{keymanager_host}/v1/containers/{uuid} or https://{keymanager_host}/v1/secrets/{uuid}
	if lbaas.opts.ContainerStore == "barbican" {
		slice := strings.Split(svcConf.tlsContainerRef, "/")
		if len(slice) < 2 {
			return fmt.Errorf("invalid tlsContainerRef for service %s", serviceName)
		}

		barbicanUUID := slice[len(slice)-1]
		barbicanType := slice[len(slice)-2]
		switch barbicanType {
		case "containers":
			container, err := containers.Get(ctx, lbaas.secret, barbicanUUID).Extract()
			if err != nil {
				return fmt.Errorf("failed to get tls container %q: %w", svcConf.tlsContainerRef, err)
			}
			klog.V(4).Infof("Default TLS container %q found", container.ContainerRef)
		case "secrets":
			secret, err := secrets.Get(ctx, lbaas.secret, barbicanUUID).Extract()
			if err != nil {
				return fmt.Errorf("failed to get tls secret %q: %w", svcConf.tlsContainerRef, err)
			}
			klog.V(4).Infof("Default TLS secret %q found", secret.SecretRef)
		default:
			return fmt.Errorf("failed to validate tlsContainerRef for service %s: tlsContainerRef type %s unknown", serviceName, barbicanType)
		}
	}

	return nil
}

func (lbaas *LbaasV2) makeSvcConf(ctx context.Context, serviceName string, service *corev1.Service, svcConf *serviceConfig) error {
	svcConf.connLimit = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerConnLimit, -1)
	svcConf.lbID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")
	svcConf.poolLbMethod = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerLbMethod, "")
	svcConf.supportLBTags = openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureTags, lbaas.opts.LBProvider)

	// Get service node-selector annotations
	svcConf.nodeSelectors = getKeyValueFromServiceAnnotation(service, ServiceAnnotationLoadBalancerNodeSelector, lbaas.opts.NodeSelector)
	for key, value := range svcConf.nodeSelectors {
		if value == "" {
			klog.V(3).Infof("Target node label %s key is set to LoadBalancer service %s", key, serviceName)
		} else {
			klog.V(3).Infof("Target node label %s=%s is set to LoadBalancer service %s", key, value, serviceName)
		}
	}

	keepClientIP := getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerXForwardedFor, false)
	svcConf.proxyProtocolVersion = getProxyProtocolFromServiceAnnotation(service)
	if svcConf.proxyProtocolVersion != nil && keepClientIP {
		return fmt.Errorf("annotation %s and %s cannot be used together", ServiceAnnotationLoadBalancerProxyEnabled, ServiceAnnotationLoadBalancerXForwardedFor)
	}
	svcConf.keepClientIP = keepClientIP

	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureTimeout, lbaas.opts.LBProvider) {
		svcConf.timeoutClientData = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutClientData, 50000)
		svcConf.timeoutMemberConnect = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutMemberConnect, 5000)
		svcConf.timeoutMemberData = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutMemberData, 50000)
		svcConf.timeoutTCPInspect = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerTimeoutTCPInspect, 0)
	}

	sourceRanges, err := GetLoadBalancerSourceRanges(service, svcConf.preferredIPFamily)
	if err != nil {
		return fmt.Errorf("failed to get source ranges for loadbalancer service %s: %v", serviceName, err)
	}
	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureVIPACL, lbaas.opts.LBProvider) {
		klog.V(4).Info("LoadBalancerSourceRanges is suppported")
		svcConf.allowedCIDR = sourceRanges.StringSlice()
	} else if lbaas.opts.LBProvider == "ovn" && lbaas.opts.ManageSecurityGroups {
		klog.V(4).Info("LoadBalancerSourceRanges will be enforced on the SG created and attached to LB members")
		svcConf.allowedCIDR = sourceRanges.StringSlice()
	} else {
		msg := "LoadBalancerSourceRanges are ignored for Service %s because Octavia provider does not support it"
		lbaas.eventRecorder.Eventf(service, corev1.EventTypeWarning, eventLBSourceRangesIgnored, msg, serviceName)
		klog.Warningf(msg, serviceName)
	}

	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureFlavors, lbaas.opts.LBProvider) {
		svcConf.flavorID = getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerFlavorID, lbaas.opts.FlavorID)
	}

	availabilityZone := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerAvailabilityZone, lbaas.opts.AvailabilityZone)
	if openstackutil.IsOctaviaFeatureSupported(ctx, lbaas.lb, openstackutil.OctaviaFeatureAvailabilityZones, lbaas.opts.LBProvider) {
		svcConf.availabilityZone = availabilityZone
	} else if availabilityZone != "" {
		msg := "LoadBalancer Availability Zones aren't supported. Please, upgrade Octavia API to version 2.14 or later (Ussuri release) to use them for Service %s"
		lbaas.eventRecorder.Eventf(service, corev1.EventTypeWarning, eventLBAZIgnored, msg, serviceName)
		klog.Warningf(msg, serviceName)
	}

	svcConf.tlsContainerRef = getStringFromServiceAnnotation(service, ServiceAnnotationTlsContainerRef, lbaas.opts.TlsContainerRef)
	svcConf.enableMonitor = getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerEnableHealthMonitor, lbaas.opts.CreateMonitor)
	if svcConf.enableMonitor && service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal && service.Spec.HealthCheckNodePort > 0 {
		svcConf.healthCheckNodePort = int(service.Spec.HealthCheckNodePort)
	}
	svcConf.healthMonitorDelay = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorDelay, int(lbaas.opts.MonitorDelay.Seconds()))
	svcConf.healthMonitorTimeout = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorTimeout, int(lbaas.opts.MonitorTimeout.Seconds()))
	svcConf.healthMonitorMaxRetries = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorMaxRetries, int(lbaas.opts.MonitorMaxRetries))
	svcConf.healthMonitorMaxRetriesDown = getIntFromServiceAnnotation(service, ServiceAnnotationLoadBalancerHealthMonitorMaxRetriesDown, int(lbaas.opts.MonitorMaxRetriesDown))
	return nil
}

// checkListenerPorts checks if there is conflict for ports.
func (lbaas *LbaasV2) checkListenerPorts(service *corev1.Service, curListenerMapping map[listenerKey]*listeners.Listener, isLBOwner bool, lbName string) error {
	for _, svcPort := range service.Spec.Ports {
		key := listenerKey{Protocol: listeners.Protocol(svcPort.Protocol), Port: int(svcPort.Port)}

		if listener, isPresent := curListenerMapping[key]; isPresent {
			// The listener is used by this Service if LB name is in the tags, or
			// the listener was created by this Service.
			if isListenerOwnedByService(*listener, isLBOwner, lbName) {
				continue
			} else {
				return fmt.Errorf("the listener port %d already exists", svcPort.Port)
			}
		}
	}

	return nil
}

func (lbaas *LbaasV2) updateServiceAnnotation(service *corev1.Service, key, value string) {
	if service.Annotations == nil {
		service.Annotations = map[string]string{}
	}
	service.Annotations[key] = value
}

// checkLoadBalancerActive returns an error if the load balancer is not ACTIVE.
func checkLoadBalancerActive(lb *loadbalancers.LoadBalancer) error {
	if lb.ProvisioningStatus != activeStatus {
		return fmt.Errorf("load balancer %s is not ACTIVE, current provisioning status: %s", lb.ID, lb.ProvisioningStatus)
	}

	return nil
}

// createLoadBalancerStatus creates the loadbalancer status from the different possible sources
func (lbaas *LbaasV2) createLoadBalancerStatus(service *corev1.Service, svcConf *serviceConfig, addr string) *corev1.LoadBalancerStatus {
	status := &corev1.LoadBalancerStatus{}
	// If hostname is explicetly set
	if hostname := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerLoadbalancerHostname, ""); hostname != "" {
		status.Ingress = []corev1.LoadBalancerIngress{{Hostname: hostname}}
		return status
	}

	ipMode := corev1.LoadBalancerIPModeVIP
	if svcConf.proxyProtocolVersion != nil {
		// If the load balancer is using the PROXY protocol, expose its IP address via
		// the Hostname field to prevent kube-proxy from injecting an iptables bypass.
		// Setting must be removed by the user to allow the use of the LoadBalancerIPModeProxy.
		if lbaas.opts.EnableIngressHostname {
			fakeHostname := fmt.Sprintf("%s.%s", addr, lbaas.opts.IngressHostnameSuffix)
			status.Ingress = []corev1.LoadBalancerIngress{{Hostname: fakeHostname}}
			return status
		}
		// Set the LoadBalancerIPMode to Proxy to prevent kube-proxy from injecting an iptables bypass.
		// https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/1860-kube-proxy-IP-node-binding
		ipMode = corev1.LoadBalancerIPModeProxy
	}
	// Default to IP
	status.Ingress = []corev1.LoadBalancerIngress{{
		IP:     addr,
		IPMode: &ipMode,
	}}
	return status
}

// getOctaviaLoadBalancer looks up a load balancer by annotation ID or by name/legacyName.
// Returns cpoerrors.ErrNotFound if the load balancer does not exist.
func (lbaas *LbaasV2) getOctaviaLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (lb *loadbalancers.LoadBalancer, err error) {
	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.getLoadBalancerLegacyName(service)
	lbID := getStringFromServiceAnnotation(service, ServiceAnnotationLoadBalancerID, "")

	if lbID != "" {
		lb, err = openstackutil.GetLoadbalancerByID(ctx, lbaas.lb, lbID)
	} else {
		lb, err = getLoadbalancerByName(ctx, lbaas.lb, name, legacyName)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get loadbalancer %w", err)
	}

	return lb, nil
}

// acquireLoadBalancer returns the Octavia load balancer for the Service, creating it when needed, and reports ownership and whether it was newly created.
func (lbaas *LbaasV2) acquireLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node, svcConf *serviceConfig, lbName string) (lb *loadbalancers.LoadBalancer, isLBOwner bool, createNewLB bool, err error) {
	// Persist the LB ID on the Service as soon as we have one, regardless of which path produced it.
	defer func() {
		if lb != nil {
			lbaas.updateServiceAnnotation(service, ServiceAnnotationLoadBalancerID, lb.ID)
		}
	}()

	lb, err = lbaas.getOctaviaLoadBalancer(ctx, clusterName, service)
	if err != nil && !errors.Is(err, cpoerrors.ErrNotFound) {
		return nil, false, false, fmt.Errorf("failed to get load balancer for Service %s: %w", serviceName(service), err)
	}

	if errors.Is(err, cpoerrors.ErrNotFound) {
		klog.InfoS("Creating loadbalancer", "lbName", lbName, "service", klog.KObj(service))
		lb, err = lbaas.createOctaviaLoadBalancer(ctx, lbName, clusterName, service, nodes, svcConf)
		if err != nil {
			return nil, false, false, fmt.Errorf("failed to create loadbalancer %s: %w", lbName, err)
		}
		return lb, true, true, nil
	}

	// Rename the LB if its name still carries an old cluster name.
	if lbHasOldClusterName(lb, clusterName) {
		msg := "Loadbalancer %s has a name of %s with incorrect cluster-name component. Renaming it to %s."
		klog.Infof(msg, lb.ID, lb.Name, lbName)
		lbaas.eventRecorder.Eventf(service, corev1.EventTypeWarning, eventLBRename, msg, lb.ID, lb.Name, lbName)

		lb, err = renameLoadBalancer(ctx, lbaas.lb, lb, lbName, clusterName)
		if err != nil {
			return nil, false, false, fmt.Errorf("failed to update load balancer %s with an updated name: %w", svcConf.lbID, err)
		}
	}

	// The Service owns the LB only when the LB uses its default generated name.
	isLBOwner = lb.Name == lbName

	if !svcConf.supportLBTags && !isLBOwner {
		return nil, false, false, fmt.Errorf("shared load balancer is only supported with the tag feature in the cloud load balancer service")
	}

	if svcConf.supportLBTags {
		sharedCount := 0
		for _, tag := range lb.Tags {
			if strings.HasPrefix(tag, servicePrefix) {
				sharedCount++
			}
		}

		isNewSharer := !isLBOwner && !slices.Contains(lb.Tags, lbName)
		if isNewSharer && sharedCount+1 > lbaas.opts.MaxSharedLB {
			return nil, false, false, fmt.Errorf("load balancer %s already shared with %d Services", lb.ID, sharedCount)
		}

		// An internal Service must not attach to a shared LB — the owner Service could later become external and expose it.
		if !isLBOwner && svcConf.internal {
			return nil, false, false, fmt.Errorf("internal Service cannot share a load balancer")
		}
	}

	if err = checkLoadBalancerActive(lb); err != nil {
		return nil, false, false, err
	}

	return lb, isLBOwner, false, nil
}

// reconcileListeners converges the LB's listeners, pools, and health monitors to match the Service's ports.
func (lbaas *LbaasV2) reconcileListeners(ctx context.Context, service *corev1.Service, svcConf *serviceConfig, loadbalancer *loadbalancers.LoadBalancer, filteredNodes []*corev1.Node, isLBOwner bool, lbName string) error {
	curListeners := loadbalancer.Listeners
	curListenerMapping := make(map[listenerKey]*listeners.Listener)
	for i, l := range curListeners {
		key := listenerKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
		curListenerMapping[key] = &curListeners[i]
	}
	klog.V(4).InfoS("Existing listeners", "portProtocolMapping", curListenerMapping)

	// Check port conflicts
	if err := lbaas.checkListenerPorts(service, curListenerMapping, isLBOwner, lbName); err != nil {
		return err
	}

	for portIndex, port := range service.Spec.Ports {
		listener, err := lbaas.ensureOctaviaListener(ctx, loadbalancer.ID, cpoutil.Sprintf255(listenerFormat, portIndex, lbName), curListenerMapping, port, svcConf)
		if err != nil {
			return err
		}

		pool, err := lbaas.ensureOctaviaPool(ctx, loadbalancer.ID, cpoutil.Sprintf255(poolFormat, portIndex, lbName), listener, service, port, filteredNodes, svcConf)
		if err != nil {
			return err
		}

		if err := lbaas.ensureOctaviaHealthMonitor(ctx, loadbalancer.ID, cpoutil.Sprintf255(monitorFormat, portIndex, lbName), pool, port, svcConf); err != nil {
			return err
		}

		// After all ports have been processed, remaining listeners are removed if they were created by this Service.
		// The remove of the listener must always happen at the end of the loop to avoid wrong assignment.
		// Modifying the curListeners would also change the mapping.
		curListeners = popListener(curListeners, listener.ID)
	}

	// Deal with the remaining listeners, delete the listener if it was created by this Service previously.
	if err := lbaas.deleteServiceListeners(ctx, loadbalancer.ID, curListeners, isLBOwner, lbName); err != nil {
		return err
	}

	return nil
}

// ensureLoadBalancerAddress resolves the LB's external address, using the VIP for IPv6 or provisioning a floating IP otherwise.
func (lbaas *LbaasV2) ensureLoadBalancerAddress(ctx context.Context, clusterName string, service *corev1.Service, loadbalancer *loadbalancers.LoadBalancer, svcConf *serviceConfig, isLBOwner bool) (string, error) {
	addr := loadbalancer.VipAddress
	// IPv6 Load Balancers have no support for Floating IP.
	if netutils.IsIPv6String(addr) {
		serviceName := serviceName(service)
		msg := "Floating IP not supported for IPv6 Service %s. Using IPv6 address instead %s."
		lbaas.eventRecorder.Eventf(service, corev1.EventTypeWarning, eventLBFloatingIPSkipped, msg, serviceName, addr)
		klog.Infof(msg, serviceName, addr)
	} else {
		var err error
		addr, err = lbaas.ensureFloatingIP(ctx, clusterName, service, loadbalancer, svcConf, isLBOwner)
		if err != nil {
			return "", err
		}
	}

	return addr, nil
}

// reconcileSecurityGroups reconciles the Service's security groups — creating/updating when management is enabled, deleting otherwise.
func (lbaas *LbaasV2) reconcileSecurityGroups(ctx context.Context, clusterName string, service *corev1.Service, filteredNodes []*corev1.Node, svcConf *serviceConfig) error {
	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.ensureAndUpdateOctaviaSecurityGroup(ctx, clusterName, service, filteredNodes, svcConf)
		if err != nil {
			return fmt.Errorf("failed to reconcile security groups for LB service %v/%v: %w", service.Namespace, service.Name, err)
		}
	} else {
		// Attempt to delete the SG if `manage-security-groups` is disabled. When CPO is reconfigured to enable it we
		// will reconcile the LB and create the SG. This is to make sure it works the same in the opposite direction.
		if err := lbaas.ensureSecurityGroupDeleted(ctx, service); err != nil {
			return err
		}
	}

	return nil
}

func (lbaas *LbaasV2) ensureOctaviaLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (lbs *corev1.LoadBalancerStatus, err error) {
	svcConf := new(serviceConfig)

	// Update the service annotations(e.g. add loadbalancer.openstack.org/load-balancer-id) in the end if it doesn't exist.
	patcher := newServicePatcher(lbaas.kclient, service)
	defer func() { err = patcher.Patch(ctx, err) }()

	if err := lbaas.checkService(ctx, service, nodes, svcConf); err != nil {
		return nil, err
	}

	// apply node-selector to a list of nodes
	filteredNodes := filterNodes(nodes, svcConf.nodeSelectors)

	// Use more meaningful name for the load balancer but still need to check the legacy name for backward compatibility.
	lbName := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	svcConf.lbName = lbName

	loadbalancer, isLBOwner, createNewLB, err := lbaas.acquireLoadBalancer(ctx, clusterName, service, filteredNodes, svcConf, lbName)
	if err != nil {
		return nil, err
	}

	loadbalancer.Listeners, err = openstackutil.GetListenersByLoadBalancerID(ctx, lbaas.lb, loadbalancer.ID)
	if err != nil {
		return nil, err
	}

	klog.V(4).InfoS("Load balancer ensured", "lbID", loadbalancer.ID, "isLBOwner", isLBOwner, "createNewLB", createNewLB)

	// This is an existing load balancer, either created by occm for other Services or by the user outside of cluster, or
	// a newly created, unpopulated loadbalancer that needs populating.
	if !createNewLB || (lbaas.opts.ProviderRequiresSerialAPICalls && createNewLB) {
		if err := lbaas.reconcileListeners(ctx, service, svcConf, loadbalancer, filteredNodes, isLBOwner, lbName); err != nil {
			return nil, err
		}
	}

	addr, err := lbaas.ensureLoadBalancerAddress(ctx, clusterName, service, loadbalancer, svcConf, isLBOwner)
	if err != nil {
		return nil, err
	}

	// save address into the annotation
	lbaas.updateServiceAnnotation(service, ServiceAnnotationLoadBalancerAddress, addr)

	// add LB name to load balancer tags.
	if svcConf.supportLBTags {
		lbTags := loadbalancer.Tags
		if !slices.Contains(lbTags, lbName) {
			lbTags = append(lbTags, lbName)
			klog.InfoS("Updating load balancer tags", "lbID", loadbalancer.ID, "tags", lbTags)
			if err := openstackutil.UpdateLoadBalancerTags(ctx, lbaas.lb, loadbalancer.ID, lbTags); err != nil {
				return nil, err
			}
		}
	}

	// Create status the load balancer
	status := lbaas.createLoadBalancerStatus(service, svcConf, addr)

	if err := lbaas.reconcileSecurityGroups(ctx, clusterName, service, filteredNodes, svcConf); err != nil {
		return status, err
	}

	return status, nil
}

// EnsureLoadBalancer creates a new load balancer or updates the existing one.
func (lbaas *LbaasV2) EnsureLoadBalancer(ctx context.Context, clusterName string, apiService *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	mc := metrics.NewMetricContext("loadbalancer", "ensure")
	klog.InfoS("EnsureLoadBalancer", "cluster", clusterName, "service", klog.KObj(apiService))
	status, err := lbaas.ensureOctaviaLoadBalancer(ctx, clusterName, apiService, nodes)
	return status, mc.ObserveReconcile(err)
}

func (lbaas *LbaasV2) listSubnetsForNetwork(ctx context.Context, networkID string, tweak ...TweakSubNetListOpsFunction) ([]subnets.Subnet, error) {
	var opts = subnets.ListOpts{NetworkID: networkID}
	for _, f := range tweak {
		if f != nil {
			f(&opts)
		}
	}
	mc := metrics.NewMetricContext("subnet", "list")
	allPages, err := subnets.List(lbaas.network, opts).AllPages(ctx)
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

func (lbaas *LbaasV2) updateOctaviaLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	svcConf := new(serviceConfig)
	var err error
	if err := lbaas.checkServiceUpdate(ctx, service, nodes, svcConf); err != nil {
		return err
	}

	// apply node-selector to a list of nodes
	filteredNodes := filterNodes(nodes, svcConf.nodeSelectors)

	serviceName := serviceName(service)
	klog.V(2).Infof("Updating %d nodes for Service %s in cluster %s", len(filteredNodes), serviceName, clusterName)

	// Get load balancer
	var loadbalancer *loadbalancers.LoadBalancer
	if svcConf.lbID != "" {
		loadbalancer, err = openstackutil.GetLoadbalancerByID(ctx, lbaas.lb, svcConf.lbID)
		if err != nil {
			return fmt.Errorf("failed to get load balancer %s: %v", svcConf.lbID, err)
		}
	} else {
		// This is a Service created before shared LB is supported.
		name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
		legacyName := lbaas.getLoadBalancerLegacyName(service)
		loadbalancer, err = getLoadbalancerByName(ctx, lbaas.lb, name, legacyName)
		if err != nil {
			return err
		}
	}
	if err := checkLoadBalancerActive(loadbalancer); err != nil {
		return err
	}

	loadbalancer.Listeners, err = openstackutil.GetListenersByLoadBalancerID(ctx, lbaas.lb, loadbalancer.ID)
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

		pool, err := lbaas.ensureOctaviaPool(ctx, loadbalancer.ID, cpoutil.Sprintf255(poolFormat, portIndex, loadbalancer.Name), &listener, service, port, filteredNodes, svcConf)
		if err != nil {
			return err
		}

		err = lbaas.ensureOctaviaHealthMonitor(ctx, loadbalancer.ID, cpoutil.Sprintf255(monitorFormat, portIndex, loadbalancer.Name), pool, port, svcConf)
		if err != nil {
			return err
		}
	}

	if lbaas.opts.ManageSecurityGroups {
		err := lbaas.ensureAndUpdateOctaviaSecurityGroup(ctx, clusterName, service, filteredNodes, svcConf)
		if err != nil {
			return fmt.Errorf("failed to update Security Group for loadbalancer service %s: %v", serviceName, err)
		}
	}
	// We don't try to lookup and delete the SG here when `manage-security-group=false` as `UpdateLoadBalancer()` is
	// only called on changes to the list of the Nodes. Deletion of the SG on reconfiguration will be handled by
	// EnsureLoadBalancer() that is the true LB reconcile function.

	return nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
func (lbaas *LbaasV2) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	mc := metrics.NewMetricContext("loadbalancer", "update")
	err := lbaas.updateOctaviaLoadBalancer(ctx, clusterName, service, nodes)
	return mc.ObserveReconcile(err)
}

// EnsureLoadBalancerDeleted deletes the specified load balancer
func (lbaas *LbaasV2) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	mc := metrics.NewMetricContext("loadbalancer", "delete")
	err := lbaas.ensureLoadBalancerDeleted(ctx, clusterName, service)
	return mc.ObserveReconcile(err)
}

func (lbaas *LbaasV2) deleteFIPIfCreatedByProvider(ctx context.Context, fip *floatingips.FloatingIP, portID string, service *corev1.Service) (bool, error) {
	matched, err := regexp.Match("Floating IP for Kubernetes external service", []byte(fip.Description))
	if err != nil {
		return false, err
	}

	if !matched {
		// It's not a FIP created by us, don't touch it.
		return false, nil
	}
	klog.InfoS("Deleting floating IP for service", "floatingIP", fip.FloatingIP, "service", klog.KObj(service))
	mc := metrics.NewMetricContext("floating_ip", "delete")
	err = floatingips.Delete(ctx, lbaas.network, fip.ID).ExtractErr()
	if mc.ObserveRequest(err) != nil {
		return false, fmt.Errorf("failed to delete floating IP %s for loadbalancer VIP port %s: %v", fip.FloatingIP, portID, err)
	}
	klog.InfoS("Deleted floating IP for service", "floatingIP", fip.FloatingIP, "service", klog.KObj(service))
	return true, nil
}

// deleteLoadBalancer removes the LB and its children either by using Octavia cascade deletion or manually
func (lbaas *LbaasV2) deleteLoadBalancer(ctx context.Context, loadbalancer *loadbalancers.LoadBalancer, service *corev1.Service, svcConf *serviceConfig, needDeleteLB bool) error {
	if needDeleteLB && lbaas.opts.CascadeDelete {
		klog.InfoS("Deleting load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
		if err := openstackutil.DeleteLoadbalancer(ctx, lbaas.lb, loadbalancer.ID, true); err != nil {
			return err
		}
		klog.InfoS("Deleted load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
		return nil
	}

	listenerList, err := openstackutil.GetListenersByLoadBalancerID(ctx, lbaas.lb, loadbalancer.ID)
	if err != nil {
		return fmt.Errorf("failed to get LB %s listeners: %w", loadbalancer.ID, err)
	}

	// When keeping a shared load balancer, only remove the listeners owned by this Service.
	if !needDeleteLB {
		listenerList = lbaas.filterServiceListeners(listenerList, service, svcConf)
	}

	if err = lbaas.deleteListenersAndMonitors(ctx, loadbalancer.ID, listenerList); err != nil {
		return err
	}

	if needDeleteLB {
		// Delete the load balancer without cascade.
		klog.InfoS("Deleting load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
		if err := openstackutil.DeleteLoadbalancer(ctx, lbaas.lb, loadbalancer.ID, false); err != nil {
			return err
		}
		klog.InfoS("Deleted load balancer", "lbID", loadbalancer.ID, "service", klog.KObj(service))
	}

	return nil
}

// filterServiceListeners returns the listeners of a shared load balancer that are owned by this Service.
func (lbaas *LbaasV2) filterServiceListeners(listenerList []listeners.Listener, service *corev1.Service, svcConf *serviceConfig) []listeners.Listener {
	curListenerMapping := make(map[listenerKey]*listeners.Listener)
	for i, l := range listenerList {
		key := listenerKey{Protocol: listeners.Protocol(l.Protocol), Port: l.ProtocolPort}
		curListenerMapping[key] = &listenerList[i]
	}

	var listenersToDelete []listeners.Listener
	for _, port := range service.Spec.Ports {
		proto := getListenerProtocol(port.Protocol, svcConf)
		listener, isPresent := curListenerMapping[listenerKey{Protocol: proto, Port: int(port.Port)}]
		if isPresent && slices.Contains(listener.Tags, svcConf.lbName) {
			listenersToDelete = append(listenersToDelete, *listener)
		}
	}

	return listenersToDelete
}

// deleteListenersAndMonitors deletes the given listeners along with their health monitors.
func (lbaas *LbaasV2) deleteListenersAndMonitors(ctx context.Context, lbID string, listenerList []listeners.Listener) error {
	// Collect the health monitors of the listeners' pools.
	var monitorIDs []string
	for _, listener := range listenerList {
		pool, err := openstackutil.GetPoolByListener(ctx, lbaas.lb, lbID, listener.ID)
		if err != nil && !errors.Is(err, cpoerrors.ErrNotFound) {
			return fmt.Errorf("failed to get pool for listener %s: %w", listener.ID, err)
		}
		if pool != nil && pool.MonitorID != "" {
			monitorIDs = append(monitorIDs, pool.MonitorID)
		}
	}

	for _, monitorID := range monitorIDs {
		klog.InfoS("Deleting health monitor", "monitorID", monitorID, "lbID", lbID)
		if err := openstackutil.DeleteHealthMonitor(ctx, lbaas.lb, monitorID, lbID); err != nil {
			return err
		}
		klog.InfoS("Deleted health monitor", "monitorID", monitorID, "lbID", lbID)
	}

	return lbaas.deleteListeners(ctx, lbID, listenerList)
}

func (lbaas *LbaasV2) ensureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	lbName := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.getLoadBalancerLegacyName(service)
	var err error
	var loadbalancer *loadbalancers.LoadBalancer
	isSharedLB := false
	updateLBTag := false
	isCreatedByOCCM := false

	svcConf := new(serviceConfig)
	if err := lbaas.checkServiceDelete(ctx, service, svcConf); err != nil {
		return err
	}
	svcConf.lbName = lbName

	if svcConf.lbID != "" {
		loadbalancer, err = openstackutil.GetLoadbalancerByID(ctx, lbaas.lb, svcConf.lbID)
	} else {
		// This may happen when this Service creation was failed previously.
		loadbalancer, err = getLoadbalancerByName(ctx, lbaas.lb, lbName, legacyName)
	}
	if err != nil && !cpoerrors.IsNotFound(err) {
		return err
	}
	if loadbalancer == nil {
		return nil
	}

	if loadbalancer.ProvisioningStatus != activeStatus && loadbalancer.ProvisioningStatus != errorStatus {
		return fmt.Errorf("load balancer %s is in immutable status, current provisioning status: %s", loadbalancer.ID, loadbalancer.ProvisioningStatus)
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
	needDeleteLB := !isSharedLB && isCreatedByOCCM

	klog.V(4).InfoS("Deleting service", "service", klog.KObj(service), "needDeleteLB", needDeleteLB, "isSharedLB", isSharedLB, "updateLBTag", updateLBTag, "isCreatedByOCCM", isCreatedByOCCM)

	if needDeleteLB {
		if err = lbaas.deleteFloatingIPForLB(ctx, service, loadbalancer); err != nil {
			return err
		}
	}

	if err = lbaas.deleteLoadBalancer(ctx, loadbalancer, service, svcConf, needDeleteLB); err != nil {
		return err
	}

	// Remove the Service's tag from the load balancer.
	if !needDeleteLB && updateLBTag {
		if err = lbaas.removeServiceFromLBTags(ctx, loadbalancer, lbName); err != nil {
			return err
		}
	}

	// Delete the Security Group. We're doing that even if `manage-security-groups` is disabled to make sure we don't
	// orphan created SGs even if CPO got reconfigured.
	if err := lbaas.ensureSecurityGroupDeleted(ctx, service); err != nil {
		return err
	}

	return nil
}

// deleteFloatingIPForLB deletes the LB's floating IP if it was created by the controller and not kept.
func (lbaas *LbaasV2) deleteFloatingIPForLB(ctx context.Context, service *corev1.Service, loadbalancer *loadbalancers.LoadBalancer) error {
	if getBoolFromServiceAnnotation(service, ServiceAnnotationLoadBalancerKeepFloatingIP, false) {
		return nil
	}
	if loadbalancer.VipPortID == "" {
		return nil
	}

	portID := loadbalancer.VipPortID
	fip, err := openstackutil.GetFloatingIPByPortID(ctx, lbaas.network, portID)
	if err != nil {
		return fmt.Errorf("failed to get floating IP for loadbalancer VIP port %s: %w", portID, err)
	}
	if fip == nil {
		return nil
	}

	_, err = lbaas.deleteFIPIfCreatedByProvider(ctx, fip, portID, service)
	return err
}

// removeServiceFromLBTags removes the Service's tag from a shared load balancer's tag list.
func (lbaas *LbaasV2) removeServiceFromLBTags(ctx context.Context, loadbalancer *loadbalancers.LoadBalancer, lbName string) error {
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
	if err := openstackutil.UpdateLoadBalancerTags(ctx, lbaas.lb, loadbalancer.ID, newTags); err != nil {
		return err
	}
	klog.InfoS("Updated load balancer tags", "lbID", loadbalancer.ID)

	return nil
}

// GetLoadBalancerSourceRanges first try to parse and verify LoadBalancerSourceRanges field from a service.
// If the field is not specified, turn to parse and verify the AnnotationLoadBalancerSourceRangesKey annotation from a service,
// extracting the source ranges to allow, and if not present returns a default (allow-all) value.
func GetLoadBalancerSourceRanges(service *corev1.Service, preferredIPFamily corev1.IPFamily) (netsets.IPNet, error) {
	// if SourceRange field is specified, ignore sourceRange annotation
	if len(service.Spec.LoadBalancerSourceRanges) > 0 {
		specs := service.Spec.LoadBalancerSourceRanges
		ipnets, err := netsets.ParseIPNets(specs...)

		if err != nil {
			return nil, fmt.Errorf("service.Spec.LoadBalancerSourceRanges: %v is not valid. Expecting a list of IP ranges. For example, 10.0.0.0/24. Error msg: %v", specs, err)
		}

		return ipnets, nil
	}

	val := service.Annotations[corev1.AnnotationLoadBalancerSourceRangesKey]
	val = strings.TrimSpace(val)
	if val == "" {
		if preferredIPFamily == corev1.IPv6Protocol {
			val = defaultLoadBalancerSourceRangesIPv6
		} else {
			val = defaultLoadBalancerSourceRangesIPv4
		}
	}
	specs := cpoutil.SplitTrim(val, ',')
	ipnets, err := netsets.ParseIPNets(specs...)
	if err != nil {
		return nil, fmt.Errorf("%s: %s is not valid. Expecting a comma-separated list of source IP ranges. For example, 10.0.0.0/24,192.168.2.0/24", corev1.AnnotationLoadBalancerSourceRangesKey, val)
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

	if e, ok := rawError.(gophercloud.ErrUnexpectedResponseCode); ok {
		switch e.Actual {
		case http.StatusBadRequest:
		case http.StatusUnauthorized:
			details = e.Body
		case http.StatusForbidden:
		case http.StatusNotFound:
			details = e.Body
		case http.StatusMethodNotAllowed:
			details = e.Body
		case http.StatusRequestTimeout:
			details = e.Body
		case http.StatusConflict:
		case http.StatusTooManyRequests:
			details = e.Body
		case http.StatusInternalServerError:
			details = e.Body
		case http.StatusServiceUnavailable:
			details = e.Body
		default:
			return rawError
		}
	} else {
		return rawError
	}

	if details != nil {
		return fmt.Errorf("%s: %s", rawError, details)
	}
	return rawError
}

// serviceName returns the namespaced name of a Service.
func serviceName(service *corev1.Service) string {
	return fmt.Sprintf("%s/%s", service.Namespace, service.Name)
}

// setPreferredIPFamily sets svcConf.preferredIPFamily to the Service's first declared IP family, if any.
func setPreferredIPFamily(service *corev1.Service, svcConf *serviceConfig) {
	if len(service.Spec.IPFamilies) > 0 {
		svcConf.preferredIPFamily = service.Spec.IPFamilies[0]
	}
}

// filterNodes uses node labels to filter the nodes that should be targeted by the LB,
// ensuring that all the labels provided in an annotation are present on the nodes
func filterNodes(nodes []*corev1.Node, filterLabels map[string]string) []*corev1.Node {
	if len(filterLabels) == 0 {
		return nodes
	}

	filteredNodes := make([]*corev1.Node, 0, len(nodes))
	for _, node := range nodes {
		if matchNodeLabels(node, filterLabels) {
			filteredNodes = append(filteredNodes, node)
		}
	}

	return filteredNodes
}

// matchNodeLabels checks if a node has all the labels in filterLabels with matching values
func matchNodeLabels(node *corev1.Node, filterLabels map[string]string) bool {
	if node == nil || len(node.Labels) == 0 {
		return false
	}

	for k, v := range filterLabels {
		if nodeLabelValue, ok := node.Labels[k]; !ok || (v != "" && nodeLabelValue != v) {
			return false
		}
	}

	return true
}
