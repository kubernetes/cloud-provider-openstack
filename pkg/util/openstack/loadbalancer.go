/*
Copyright 2019 The Kubernetes Authors.

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
	"os"
	"strconv"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/apiversions"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/l7policies"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/monitors"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/v2/pagination"
	version "github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/util/wait"
	klog "k8s.io/klog/v2"

	"k8s.io/cloud-provider-openstack/pkg/metrics"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
)

const (
	OctaviaFeatureTags               = 0
	OctaviaFeatureVIPACL             = 1
	OctaviaFeatureFlavors            = 2
	OctaviaFeatureTimeout            = 3
	OctaviaFeatureAvailabilityZones  = 4
	OctaviaFeatureHTTPMonitorsOnUDP  = 5
	OctaviaFeaturePrometheusListener = 6

	waitLoadbalancerInitDelay   = 1 * time.Second
	waitLoadbalancerFactor      = 1.2
	waitLoadbalancerActiveSteps = 23
	waitLoadbalancerDeleteSteps = 12

	activeStatus = "ACTIVE"
	errorStatus  = "ERROR"
)

var (
	octaviaVersion string
)

// getOctaviaVersion returns the current Octavia API version.
func getOctaviaVersion(client *gophercloud.ServiceClient) (string, error) {
	if octaviaVersion != "" {
		return octaviaVersion, nil
	}

	var defaultVer = "0.0"
	mc := metrics.NewMetricContext("version", "list")
	allPages, err := apiversions.List(client).AllPages(context.TODO())
	if mc.ObserveRequest(err) != nil {
		return defaultVer, err
	}
	versions, err := apiversions.ExtractAPIVersions(allPages)
	if err != nil {
		return defaultVer, err
	}
	if len(versions) == 0 {
		return defaultVer, fmt.Errorf("API versions for Octavia not found")
	}

	klog.V(4).Infof("Found Octavia API versions: %v", versions)

	// The current version is always the last one in the list
	octaviaVersion = versions[len(versions)-1].ID
	klog.V(4).Infof("The current Octavia API version: %v", octaviaVersion)

	return octaviaVersion, nil
}

// IsOctaviaFeatureSupported returns true if the given feature is supported in the deployed Octavia version.
func IsOctaviaFeatureSupported(client *gophercloud.ServiceClient, feature int, lbProvider string) bool {
	octaviaVer, err := getOctaviaVersion(client)
	if err != nil {
		klog.Warningf("Failed to get current Octavia API version: %v", err)
		return false
	}

	currentVer, _ := version.NewVersion(octaviaVer)

	switch feature {
	case OctaviaFeatureVIPACL:
		if lbProvider == "ovn" {
			return false
		}
		verACL, _ := version.NewVersion("v2.12")
		if currentVer.GreaterThanOrEqual(verACL) {
			return true
		}
	case OctaviaFeatureTags:
		verTags, _ := version.NewVersion("v2.5")
		if currentVer.GreaterThanOrEqual(verTags) {
			return true
		}
	case OctaviaFeatureFlavors:
		if lbProvider == "ovn" {
			return false
		}
		verFlavors, _ := version.NewVersion("v2.6")
		if currentVer.GreaterThanOrEqual(verFlavors) {
			return true
		}
	case OctaviaFeatureTimeout:
		if lbProvider == "ovn" {
			return false
		}
		verFlavors, _ := version.NewVersion("v2.1")
		if currentVer.GreaterThanOrEqual(verFlavors) {
			return true
		}
	case OctaviaFeatureAvailabilityZones:
		if lbProvider == "ovn" {
			return false
		}
		verAvailabilityZones, _ := version.NewVersion("v2.14")
		if currentVer.GreaterThanOrEqual(verAvailabilityZones) {
			return true
		}
	case OctaviaFeatureHTTPMonitorsOnUDP:
		if lbProvider == "ovn" {
			return false
		}
		verHTTPMonitorsOnUDP, _ := version.NewVersion("v2.16")
		if currentVer.GreaterThanOrEqual(verHTTPMonitorsOnUDP) {
			return true
		}
	case OctaviaFeaturePrometheusListener:
		if lbProvider == "ovn" {
			return false
		}
		verACL, _ := version.NewVersion("v2.25")
		if currentVer.GreaterThanOrEqual(verACL) {
			return true
		}
	default:
		klog.Warningf("Feature %d not recognized", feature)
	}

	return false
}

func getTimeoutSteps(name string, steps int) int {
	if v := os.Getenv(name); v != "" {
		s, err := strconv.Atoi(v)
		if err == nil && s >= 0 {
			return s
		}
	}
	return steps
}

// WaitActiveAndGetLoadBalancer wait for LB active then return the LB object for further usage
func WaitActiveAndGetLoadBalancer(client *gophercloud.ServiceClient, loadbalancerID string) (*loadbalancers.LoadBalancer, error) {
	klog.InfoS("Waiting for load balancer ACTIVE", "lbID", loadbalancerID)
	steps := getTimeoutSteps("OCCM_WAIT_LB_ACTIVE_STEPS", waitLoadbalancerActiveSteps)
	backoff := wait.Backoff{
		Duration: waitLoadbalancerInitDelay,
		Factor:   waitLoadbalancerFactor,
		Steps:    steps,
	}

	var loadbalancer *loadbalancers.LoadBalancer
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		mc := metrics.NewMetricContext("loadbalancer", "get")
		var err error
		loadbalancer, err = loadbalancers.Get(context.TODO(), client, loadbalancerID).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Failed to fetch loadbalancer status from OpenStack (lbID %q): %s", loadbalancerID, err)
			return false, nil
		}
		if loadbalancer.ProvisioningStatus == activeStatus {
			klog.InfoS("Load balancer ACTIVE", "lbID", loadbalancerID)
			return true, nil
		} else if loadbalancer.ProvisioningStatus == errorStatus {
			return true, fmt.Errorf("loadbalancer %s has gone into ERROR state", loadbalancerID)
		} else {
			return false, nil
		}

	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("timeout waiting for the loadbalancer %s %s", loadbalancerID, activeStatus)
	}

	return loadbalancer, err
}

// GetLoadBalancers returns all the filtered load balancer.
func GetLoadBalancers(client *gophercloud.ServiceClient, opts loadbalancers.ListOpts) ([]loadbalancers.LoadBalancer, error) {
	mc := metrics.NewMetricContext("loadbalancer", "list")
	allPages, err := loadbalancers.List(client, opts).AllPages(context.TODO())
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	allLoadbalancers, err := loadbalancers.ExtractLoadBalancers(allPages)
	if err != nil {
		return nil, err
	}

	return allLoadbalancers, nil
}

// GetLoadbalancerByID retrieves loadbalancer object
func GetLoadbalancerByID(client *gophercloud.ServiceClient, lbID string) (*loadbalancers.LoadBalancer, error) {
	mc := metrics.NewMetricContext("loadbalancer", "get")
	lb, err := loadbalancers.Get(context.TODO(), client, lbID).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return lb, nil
}

// GetLoadbalancerByName retrieves loadbalancer object
func GetLoadbalancerByName(client *gophercloud.ServiceClient, name string) (*loadbalancers.LoadBalancer, error) {
	opts := loadbalancers.ListOpts{
		Name: name,
	}
	mc := metrics.NewMetricContext("loadbalancer", "list")
	allPages, err := loadbalancers.List(client, opts).AllPages(context.TODO())
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	loadbalancerList, err := loadbalancers.ExtractLoadBalancers(allPages)
	if err != nil {
		return nil, err
	}

	if len(loadbalancerList) > 1 {
		return nil, cpoerrors.ErrMultipleResults
	}
	if len(loadbalancerList) == 0 {
		return nil, cpoerrors.ErrNotFound
	}

	return &loadbalancerList[0], nil
}

// UpdateLoadBalancerTags updates tags for the load balancer
func UpdateLoadBalancerTags(client *gophercloud.ServiceClient, lbID string, tags []string) error {
	updateOpts := loadbalancers.UpdateOpts{
		Tags: &tags,
	}

	_, err := UpdateLoadBalancer(client, lbID, updateOpts)

	return err
}

// UpdateLoadBalancer updates the load balancer
func UpdateLoadBalancer(client *gophercloud.ServiceClient, lbID string, updateOpts loadbalancers.UpdateOpts) (*loadbalancers.LoadBalancer, error) {
	mc := metrics.NewMetricContext("loadbalancer", "update")
	_, err := loadbalancers.Update(context.TODO(), client, lbID, updateOpts).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	lb, err := WaitActiveAndGetLoadBalancer(client, lbID)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for load balancer %s ACTIVE after updating: %v", lbID, err)
	}

	return lb, nil
}

func waitLoadbalancerDeleted(client *gophercloud.ServiceClient, loadbalancerID string) error {
	klog.V(4).InfoS("Waiting for load balancer deleted", "lbID", loadbalancerID)
	backoff := wait.Backoff{
		Duration: waitLoadbalancerInitDelay,
		Factor:   waitLoadbalancerFactor,
		Steps:    waitLoadbalancerDeleteSteps,
	}
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		mc := metrics.NewMetricContext("loadbalancer", "get")
		_, err := loadbalancers.Get(context.TODO(), client, loadbalancerID).Extract()
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				klog.V(4).InfoS("Load balancer deleted", "lbID", loadbalancerID)
				return true, mc.ObserveRequest(nil)
			}
			return false, mc.ObserveRequest(err)
		}
		return false, mc.ObserveRequest(nil)
	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("loadbalancer failed to delete within the allotted time")
	}

	return err
}

// DeleteLoadbalancer deletes a loadbalancer and wait for it's gone.
func DeleteLoadbalancer(client *gophercloud.ServiceClient, lbID string, cascade bool) error {
	opts := loadbalancers.DeleteOpts{}
	if cascade {
		opts.Cascade = true
	}

	mc := metrics.NewMetricContext("loadbalancer", "delete")
	err := loadbalancers.Delete(context.TODO(), client, lbID, opts).ExtractErr()
	if err != nil && !cpoerrors.IsNotFound(err) {
		_ = mc.ObserveRequest(err)
		return fmt.Errorf("error deleting loadbalancer %s: %v", lbID, err)
	}
	_ = mc.ObserveRequest(nil)

	if err := waitLoadbalancerDeleted(client, lbID); err != nil {
		return err
	}

	return nil
}

// UpdateListener updates a listener and wait for the lb active
func UpdateListener(client *gophercloud.ServiceClient, lbID string, listenerID string, opts listeners.UpdateOpts) error {
	mc := metrics.NewMetricContext("loadbalancer_listener", "update")
	_, err := listeners.Update(context.TODO(), client, listenerID, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return err
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after updating listener: %v", lbID, err)
	}

	return nil
}

// CreateListener creates a new listener
func CreateListener(client *gophercloud.ServiceClient, lbID string, opts listeners.CreateOpts) (*listeners.Listener, error) {
	mc := metrics.NewMetricContext("loadbalancer_listener", "create")
	listener, err := listeners.Create(context.TODO(), client, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return nil, fmt.Errorf("failed to wait for load balancer %s ACTIVE after creating listener: %v", lbID, err)
	}

	return listener, nil
}

// DeleteListener deletes a listener.
func DeleteListener(client *gophercloud.ServiceClient, listenerID string, lbID string) error {
	mc := metrics.NewMetricContext("loadbalancer_listener", "delete")
	if err := listeners.Delete(context.TODO(), client, listenerID).ExtractErr(); mc.ObserveRequest(err) != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(2).Infof("Listener %s for load balancer %s was already deleted: %v", listenerID, lbID, err)
		} else {
			_ = mc.ObserveRequest(err)
			return fmt.Errorf("error deleting listener %s for load balancer %s: %v", listenerID, lbID, err)
		}
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after deleting listener: %v", lbID, err)
	}

	return nil
}

// GetListenerByName gets a listener by its name, raise error if not found or get multiple ones.
func GetListenerByName(client *gophercloud.ServiceClient, name string, lbID string) (*listeners.Listener, error) {
	opts := listeners.ListOpts{
		Name:           name,
		LoadbalancerID: lbID,
	}
	mc := metrics.NewMetricContext("loadbalancer_listener", "list")
	pager := listeners.List(client, opts)
	var listenerList []listeners.Listener

	err := pager.EachPage(context.TODO(), func(_ context.Context, page pagination.Page) (bool, error) {
		v, err := listeners.ExtractListeners(page)
		if err != nil {
			return false, err
		}
		listenerList = append(listenerList, v...)
		if len(listenerList) > 1 {
			return false, cpoerrors.ErrMultipleResults
		}
		return true, nil
	})
	if mc.ObserveRequest(err) != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, cpoerrors.ErrNotFound
		}
		return nil, err
	}

	if len(listenerList) == 0 {
		return nil, cpoerrors.ErrNotFound
	}

	return &listenerList[0], nil
}

// GetListenersByLoadBalancerID returns listener list
func GetListenersByLoadBalancerID(client *gophercloud.ServiceClient, lbID string) ([]listeners.Listener, error) {
	mc := metrics.NewMetricContext("loadbalancer_listener", "list")
	var lbListeners []listeners.Listener

	allPages, err := listeners.List(client, listeners.ListOpts{LoadbalancerID: lbID}).AllPages(context.TODO())
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	lbListeners, err = listeners.ExtractListeners(allPages)
	if err != nil {
		return nil, err
	}

	return lbListeners, nil
}

// CreatePool creates a new pool.
func CreatePool(client *gophercloud.ServiceClient, opts pools.CreateOptsBuilder, lbID string) (*pools.Pool, error) {
	mc := metrics.NewMetricContext("loadbalancer_pool", "create")
	pool, err := pools.Create(context.TODO(), client, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	if _, err = WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return nil, fmt.Errorf("failed to wait for load balancer ACTIVE after creating pool: %v", err)
	}

	return pool, nil
}

// GetPoolByName gets a pool by its name, raise error if not found or get multiple ones.
func GetPoolByName(client *gophercloud.ServiceClient, name string, lbID string) (*pools.Pool, error) {
	var listenerPools []pools.Pool

	opts := pools.ListOpts{
		Name:           name,
		LoadbalancerID: lbID,
	}
	mc := metrics.NewMetricContext("loadbalancer_pool", "list")
	err := pools.List(client, opts).EachPage(context.TODO(), func(_ context.Context, page pagination.Page) (bool, error) {
		v, err := pools.ExtractPools(page)
		if err != nil {
			return false, err
		}
		listenerPools = append(listenerPools, v...)
		if len(listenerPools) > 1 {
			return false, cpoerrors.ErrMultipleResults
		}
		return true, nil
	})
	if mc.ObserveRequest(err) != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, cpoerrors.ErrNotFound
		}
		return nil, err
	}

	if len(listenerPools) == 0 {
		return nil, cpoerrors.ErrNotFound
	} else if len(listenerPools) > 1 {
		return nil, cpoerrors.ErrMultipleResults
	}

	return &listenerPools[0], nil
}

// GetPoolByListener finds pool for a listener.
// A listener always has exactly one pool.
func GetPoolByListener(client *gophercloud.ServiceClient, lbID, listenerID string) (*pools.Pool, error) {
	listenerPools := make([]pools.Pool, 0, 1)
	mc := metrics.NewMetricContext("loadbalancer_pool", "list")
	err := pools.List(client, pools.ListOpts{LoadbalancerID: lbID}).EachPage(context.TODO(), func(_ context.Context, page pagination.Page) (bool, error) {
		poolsList, err := pools.ExtractPools(page)
		if err != nil {
			return false, err
		}
		for _, p := range poolsList {
			for _, l := range p.Listeners {
				if l.ID == listenerID {
					listenerPools = append(listenerPools, p)
				}
			}
		}
		if len(listenerPools) > 1 {
			return false, cpoerrors.ErrMultipleResults
		}
		return true, nil
	})
	if mc.ObserveRequest(err) != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, cpoerrors.ErrNotFound
		}
		return nil, err
	}

	if len(listenerPools) == 0 {
		return nil, cpoerrors.ErrNotFound
	}

	return &listenerPools[0], nil
}

// GetPools retrieves the pools belong to the loadbalancer.
func GetPools(client *gophercloud.ServiceClient, lbID string) ([]pools.Pool, error) {
	var lbPools []pools.Pool

	opts := pools.ListOpts{
		LoadbalancerID: lbID,
	}
	allPages, err := pools.List(client, opts).AllPages(context.TODO())
	if err != nil {
		return nil, err
	}

	lbPools, err = pools.ExtractPools(allPages)
	if err != nil {
		return nil, err
	}

	return lbPools, nil
}

// GetMembersbyPool get all the members in the pool.
func GetMembersbyPool(client *gophercloud.ServiceClient, poolID string) ([]pools.Member, error) {
	var members []pools.Member

	mc := metrics.NewMetricContext("loadbalancer_member", "list")
	err := pools.ListMembers(client, poolID, pools.ListMembersOpts{}).EachPage(context.TODO(), func(_ context.Context, page pagination.Page) (bool, error) {
		membersList, err := pools.ExtractMembers(page)
		if err != nil {
			return false, err
		}
		members = append(members, membersList...)

		return true, nil
	})
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return members, nil
}

// UpdatePool updates a pool and wait for the lb active
func UpdatePool(client *gophercloud.ServiceClient, lbID string, poolID string, opts pools.UpdateOpts) error {
	mc := metrics.NewMetricContext("loadbalancer_pool", "update")
	_, err := pools.Update(context.TODO(), client, poolID, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return err
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after updating pool: %v", lbID, err)
	}

	return nil
}

// DeletePool deletes a pool.
func DeletePool(client *gophercloud.ServiceClient, poolID string, lbID string) error {
	mc := metrics.NewMetricContext("loadbalancer_pool", "delete")
	if err := pools.Delete(context.TODO(), client, poolID).ExtractErr(); mc.ObserveRequest(err) != nil {
		if cpoerrors.IsNotFound(err) {
			klog.V(2).Infof("Pool %s for load balancer %s was already deleted: %v", poolID, lbID, err)
		} else {
			return fmt.Errorf("error deleting pool %s for load balancer %s: %v", poolID, lbID, err)
		}
	}
	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after deleting pool: %v", lbID, err)
	}

	return nil
}

// BatchUpdatePoolMembers updates pool members in batch.
func BatchUpdatePoolMembers(client *gophercloud.ServiceClient, lbID string, poolID string, opts []pools.BatchUpdateMemberOpts) error {
	mc := metrics.NewMetricContext("loadbalancer_members", "update")
	err := pools.BatchUpdateMembers(context.TODO(), client, poolID, opts).ExtractErr()
	if mc.ObserveRequest(err) != nil {
		return err
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after updating pool members for %s: %v", lbID, poolID, err)
	}

	return nil
}

// GetL7policies retrieves all l7 policies for the given listener.
func GetL7policies(client *gophercloud.ServiceClient, listenerID string) ([]l7policies.L7Policy, error) {
	var policies []l7policies.L7Policy
	opts := l7policies.ListOpts{
		ListenerID: listenerID,
	}
	err := l7policies.List(client, opts).EachPage(context.TODO(), func(_ context.Context, page pagination.Page) (bool, error) {
		v, err := l7policies.ExtractL7Policies(page)
		if err != nil {
			return false, err
		}
		policies = append(policies, v...)
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return policies, nil
}

// CreateL7Policy creates a l7 policy.
func CreateL7Policy(client *gophercloud.ServiceClient, opts l7policies.CreateOpts, lbID string) (*l7policies.L7Policy, error) {
	mc := metrics.NewMetricContext("loadbalancer_l7policy", "create")
	policy, err := l7policies.Create(context.TODO(), client, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	if _, err = WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return nil, fmt.Errorf("failed to wait for load balancer ACTIVE after creating l7policy: %v", err)
	}

	return policy, nil
}

// DeleteL7policy deletes a l7 policy.
func DeleteL7policy(client *gophercloud.ServiceClient, policyID string, lbID string) error {
	mc := metrics.NewMetricContext("loadbalancer_l7policy", "delete")
	if err := l7policies.Delete(context.TODO(), client, policyID).ExtractErr(); mc.ObserveRequest(err) != nil {
		return err
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after deleting l7policy: %v", lbID, err)
	}

	return nil
}

// GetL7Rules gets all the rules for a l7 policy
func GetL7Rules(client *gophercloud.ServiceClient, policyID string) ([]l7policies.Rule, error) {
	listOpts := l7policies.ListRulesOpts{}
	allPages, err := l7policies.ListRules(client, policyID, listOpts).AllPages(context.TODO())
	if err != nil {
		return nil, err
	}
	allRules, err := l7policies.ExtractRules(allPages)
	if err != nil {
		return nil, err
	}

	return allRules, nil
}

// CreateL7Rule creates a l7 rule.
func CreateL7Rule(client *gophercloud.ServiceClient, policyID string, opts l7policies.CreateRuleOpts, lbID string) error {
	mc := metrics.NewMetricContext("loadbalancer_l7rule", "create")
	_, err := l7policies.CreateRule(context.TODO(), client, policyID, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return err
	}

	if _, err = WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer ACTIVE after creating l7policy rule: %v", err)
	}

	return nil
}

// UpdateHealthMonitor updates a health monitor.
func UpdateHealthMonitor(client *gophercloud.ServiceClient, monitorID string, opts monitors.UpdateOpts, lbID string) error {
	mc := metrics.NewMetricContext("loadbalancer_healthmonitor", "update")
	_, err := monitors.Update(context.TODO(), client, monitorID, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf("failed to update healthmonitor: %v", err)
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after updating healthmonitor: %v", lbID, err)
	}

	return nil
}

// DeleteHealthMonitor deletes a health monitor.
func DeleteHealthMonitor(client *gophercloud.ServiceClient, monitorID string, lbID string) error {
	mc := metrics.NewMetricContext("loadbalancer_healthmonitor", "delete")
	err := monitors.Delete(context.TODO(), client, monitorID).ExtractErr()
	if err != nil && !cpoerrors.IsNotFound(err) {
		return mc.ObserveRequest(err)
	}
	_ = mc.ObserveRequest(nil)
	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return fmt.Errorf("failed to wait for load balancer %s ACTIVE after deleting healthmonitor: %v", lbID, err)
	}

	return nil
}

// CreateHealthMonitor creates a health monitor in a pool.
func CreateHealthMonitor(client *gophercloud.ServiceClient, opts monitors.CreateOpts, lbID string) (*monitors.Monitor, error) {
	mc := metrics.NewMetricContext("loadbalancer_healthmonitor", "create")
	monitor, err := monitors.Create(context.TODO(), client, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, fmt.Errorf("failed to create healthmonitor: %v", err)
	}

	if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
		return nil, fmt.Errorf("failed to wait for load balancer %s ACTIVE after creating healthmonitor: %v", lbID, err)
	}

	return monitor, nil
}

// GetHealthMonitor gets details about loadbalancer health monitor.
func GetHealthMonitor(client *gophercloud.ServiceClient, monitorID string) (*monitors.Monitor, error) {
	mc := metrics.NewMetricContext("loadbalancer_healthmonitor", "get")
	monitor, err := monitors.Get(context.TODO(), client, monitorID).Extract()
	if mc.ObserveRequest(err) != nil {
		return nil, fmt.Errorf("failed to get healthmonitor: %v", err)
	}

	return monitor, nil
}
