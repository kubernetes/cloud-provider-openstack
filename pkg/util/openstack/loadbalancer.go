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
	// Octavia feature flags for version checking
	OctaviaFeatureTags              = 0 // Tag support (v2.5+)
	OctaviaFeatureVIPACL            = 1 // VIP ACL support (v2.12+, not supported on OVN)
	OctaviaFeatureFlavors           = 2 // Flavor support (v2.6+, not supported on OVN)
	OctaviaFeatureTimeout           = 3 // Timeout support (v2.1+, not supported on OVN)
	OctaviaFeatureAvailabilityZones = 4 // Availability zone support (v2.14+, not supported on OVN)
	OctaviaFeatureHTTPMonitorsOnUDP = 5 // HTTP health monitors on UDP pools (v2.16+, not supported on OVN)

	// Wait configuration for load balancer operations
	waitLoadbalancerInitDelay   = 1 * time.Second // Initial delay before first check
	waitLoadbalancerFactor      = 1.2             // Exponential backoff multiplier
	waitLoadbalancerActiveSteps = 23              // Max steps for ACTIVE status (~5 min total)
	waitLoadbalancerDeleteSteps = 24              // Max steps for DELETE (~2 min total)

	// Load balancer provisioning states
	activeStatus = "ACTIVE" // Load balancer is operational
	errorStatus  = "ERROR"  // Load balancer is in error state
)

var (
	octaviaVersion string
)

// getOctaviaVersion returns the current Octavia API version.
func getOctaviaVersion(ctx context.Context, client *gophercloud.ServiceClient) (string, error) {
	if octaviaVersion != "" {
		return octaviaVersion, nil
	}

	var defaultVer = "0.0"
	mc := metrics.NewMetricContext("version", "list")
	allPages, err := apiversions.List(client).AllPages(ctx)
	if mc.ObserveRequest(err) != nil {
		return defaultVer, err
	}
	versions, err := apiversions.ExtractAPIVersions(allPages)
	if mc.ObserveRequest(err) != nil {
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

// IsOctaviaFeatureSupported checks if a specific Octavia feature is available
// based on the deployed API version and load balancer provider.
//
// The function compares the current Octavia version against the minimum required
// version for each feature. Some features are not supported on certain providers
// (e.g., OVN provider does not support VIP ACL, flavors, timeouts, etc.).
//
// Returns false if:
// - The Octavia version cannot be determined
// - The feature is not supported by the provider
// - The version is below the minimum required version
func IsOctaviaFeatureSupported(ctx context.Context, client *gophercloud.ServiceClient, feature int, lbProvider string) bool {
	octaviaVer, err := getOctaviaVersion(ctx, client)
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
	default:
		klog.Warningf("Feature %d not recognized", feature)
	}

	return false
}

// ==============================================================================
// Internal Helper Functions
// ==============================================================================

// executeAndWaitActive executes an operation with metrics tracking and waits for
// the load balancer to return to ACTIVE state. Used for operations that don't
// return a result (delete, update without returning updated object).
func executeAndWaitActive(ctx context.Context, client *gophercloud.ServiceClient,
	lbID, resourceType, operation string, fn func() error) error {

	_, err := executeExtractAndWaitActive(ctx, client, lbID, resourceType, operation,
		func() (*struct{}, error) {
			if err := fn(); err != nil {
				return nil, err
			}
			return &struct{}{}, nil
		})
	return err
}

// executeExtractAndWaitActive executes an operation with metrics tracking, extracts
// the result, and waits for the load balancer to return to ACTIVE state.
// Used for create operations that return the created resource.
// For delete operations, NotFound errors are logged but not returned as errors.
func executeExtractAndWaitActive[T any](ctx context.Context, client *gophercloud.ServiceClient,
	lbID, resourceType, operation string, fn func() (T, error)) (T, error) {

	var zero T
	mc := metrics.NewMetricContext(resourceType, operation)
	result, err := fn()

	// For delete operations, treat NotFound as success (already deleted)
	if err != nil && operation == "delete" && cpoerrors.IsNotFound(err) {
		klog.V(2).Infof("%s was already deleted", resourceType)
		_ = mc.ObserveRequest(nil)
		// Still wait for load balancer to be active for consistency
		if _, waitErr := WaitActiveAndGetLoadBalancer(ctx, client, lbID); waitErr != nil {
			return zero, fmt.Errorf("failed to wait for load balancer %s ACTIVE after %s %s: %v",
				lbID, operation, resourceType, waitErr)
		}
		return zero, nil
	}

	// Observe the request result (error or success)
	if mc.ObserveRequest(err) != nil {
		return zero, fmt.Errorf("failed to %s %s: %v", operation, resourceType, err)
	}

	if _, waitErr := WaitActiveAndGetLoadBalancer(ctx, client, lbID); waitErr != nil {
		return zero, fmt.Errorf("failed to wait for load balancer %s ACTIVE after %s %s: %v",
			lbID, operation, resourceType, waitErr)
	}
	return result, nil
}

// list performs pagination and returns all results with metrics tracking.
func list[T any](ctx context.Context, resourceType, operation string,
	pager pagination.Pager, extractFn func(pagination.Page) ([]T, error)) ([]T, error) {

	mc := metrics.NewMetricContext(resourceType, operation)
	allPages, err := pager.AllPages(ctx)
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	results, err := extractFn(allPages)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// listWithUniqueResult performs pagination and expects exactly one result.
// Returns ErrNotFound if no results, ErrMultipleResults if more than one.
// Stops pagination early if multiple results are detected.
func listWithUniqueResult[T any](ctx context.Context, resourceType, operation string,
	pager pagination.Pager, extractFn func(pagination.Page) ([]T, error)) (*T, error) {

	mc := metrics.NewMetricContext(resourceType, operation)
	var results []T
	err := pager.EachPage(ctx, func(_ context.Context, page pagination.Page) (bool, error) {
		items, err := extractFn(page)
		if err != nil {
			return false, err
		}
		results = append(results, items...)
		// Stop early if we found more than one result
		if len(results) > 1 {
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

	if len(results) == 0 {
		return nil, cpoerrors.ErrNotFound
	}
	return &results[0], nil
}

// getSingleResource executes a single resource retrieval operation with metrics tracking.
// Used for Get operations that retrieve a resource by ID.
func getSingleResource[T any](ctx context.Context, resourceType, operation string,
	fn func() (T, error)) (T, error) {

	mc := metrics.NewMetricContext(resourceType, operation)
	result, err := fn()
	if mc.ObserveRequest(err) != nil {
		var zero T
		return zero, fmt.Errorf("failed to %s %s: %v", operation, resourceType, err)
	}
	return result, nil
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

// ==============================================================================
// Load Balancer Operations
// ==============================================================================

// WaitActiveAndGetLoadBalancer waits for a load balancer to reach ACTIVE status
// and returns the updated load balancer object. It uses exponential backoff and
// will timeout after the configured number of steps (default 23, approximately 5 minutes).
//
// Returns an error if:
// - The load balancer enters ERROR state
// - The timeout is exceeded
// - API calls fail for reasons other than transient status checks
//
// The timeout can be customized using the OCCM_WAIT_LB_ACTIVE_STEPS environment variable.
func WaitActiveAndGetLoadBalancer(ctx context.Context, client *gophercloud.ServiceClient, loadbalancerID string) (*loadbalancers.LoadBalancer, error) {
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
		loadbalancer, err = loadbalancers.Get(ctx, client, loadbalancerID).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Failed to fetch loadbalancer status from OpenStack (lbID %q): %s", loadbalancerID, err)
			return false, nil
		}
		switch loadbalancer.ProvisioningStatus {
		case activeStatus:
			klog.InfoS("Load balancer ACTIVE", "lbID", loadbalancerID)
			return true, nil
		case errorStatus:
			return true, fmt.Errorf("loadbalancer %s has gone into ERROR state", loadbalancerID)
		default:
			return false, nil
		}

	})

	if wait.Interrupted(err) {
		err = fmt.Errorf("timeout waiting for the loadbalancer %s %s", loadbalancerID, activeStatus)
	}

	return loadbalancer, err
}

// GetLoadBalancers retrieves all load balancers matching the provided filters.
// Use ListOpts to filter by name, project ID, tags, or other attributes.
func GetLoadBalancers(ctx context.Context, client *gophercloud.ServiceClient, opts loadbalancers.ListOpts) ([]loadbalancers.LoadBalancer, error) {
	pager := loadbalancers.List(client, opts)
	return list(ctx, "loadbalancer", "list", pager, loadbalancers.ExtractLoadBalancers)
}

// GetLoadbalancerByID retrieves a load balancer by ID.
func GetLoadbalancerByID(ctx context.Context, client *gophercloud.ServiceClient, lbID string) (*loadbalancers.LoadBalancer, error) {
	return getSingleResource(ctx, "loadbalancer", "get",
		func() (*loadbalancers.LoadBalancer, error) {
			return loadbalancers.Get(ctx, client, lbID).Extract()
		})
}

// GetLoadbalancerByName retrieves a load balancer by name.
func GetLoadbalancerByName(ctx context.Context, client *gophercloud.ServiceClient, name string) (*loadbalancers.LoadBalancer, error) {
	pager := loadbalancers.List(client, loadbalancers.ListOpts{Name: name})
	return listWithUniqueResult(ctx, "loadbalancer", "list", pager, loadbalancers.ExtractLoadBalancers)
}

// UpdateLoadBalancerTags updates tags for a load balancer and waits for it to become active.
func UpdateLoadBalancerTags(ctx context.Context, client *gophercloud.ServiceClient, lbID string, tags []string) error {
	updateOpts := loadbalancers.UpdateOpts{
		Tags: &tags,
	}

	_, err := UpdateLoadBalancer(ctx, client, lbID, updateOpts)

	return err
}

// UpdateLoadBalancer updates a load balancer and waits for it to become active.
func UpdateLoadBalancer(ctx context.Context, client *gophercloud.ServiceClient, lbID string, updateOpts loadbalancers.UpdateOpts) (*loadbalancers.LoadBalancer, error) {
	return executeExtractAndWaitActive(ctx, client, lbID, "loadbalancer", "update",
		func() (*loadbalancers.LoadBalancer, error) {
			return loadbalancers.Update(ctx, client, lbID, updateOpts).Extract()
		})
}

func waitLoadBalancerDeleted(ctx context.Context, client *gophercloud.ServiceClient, loadbalancerID string) error {
	klog.V(4).InfoS("Waiting for load balancer deleted", "lbID", loadbalancerID)
	backoff := wait.Backoff{
		Duration: waitLoadbalancerInitDelay,
		Factor:   waitLoadbalancerFactor,
		Steps:    waitLoadbalancerDeleteSteps,
	}
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		mc := metrics.NewMetricContext("loadbalancer", "get")
		_, err := loadbalancers.Get(ctx, client, loadbalancerID).Extract()
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

// DeleteLoadbalancer deletes a load balancer and waits for it to be fully deleted.
func DeleteLoadbalancer(ctx context.Context, client *gophercloud.ServiceClient, lbID string, cascade bool) error {
	opts := loadbalancers.DeleteOpts{}
	if cascade {
		opts.Cascade = true
	}

	mc := metrics.NewMetricContext("loadbalancer", "delete")
	err := loadbalancers.Delete(ctx, client, lbID, opts).ExtractErr()
	if err != nil && !cpoerrors.IsNotFound(err) {
		_ = mc.ObserveRequest(err)
		return fmt.Errorf("error deleting loadbalancer %s: %v", lbID, err)
	}
	_ = mc.ObserveRequest(nil)

	if err := waitLoadBalancerDeleted(ctx, client, lbID); err != nil {
		return err
	}

	return nil
}

// ==============================================================================
// Listener Operations
// ==============================================================================

// UpdateListener updates a listener and waits for the load balancer to become active.
func UpdateListener(ctx context.Context, client *gophercloud.ServiceClient, lbID string, listenerID string, opts listeners.UpdateOpts) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_listener", "update",
		func() error {
			_, err := listeners.Update(ctx, client, listenerID, opts).Extract()
			return err
		})
}

// CreateListener creates a listener and waits for the load balancer to become active.
func CreateListener(ctx context.Context, client *gophercloud.ServiceClient, lbID string, opts listeners.CreateOpts) (*listeners.Listener, error) {
	return executeExtractAndWaitActive(ctx, client, lbID, "loadbalancer_listener", "create",
		func() (*listeners.Listener, error) {
			return listeners.Create(ctx, client, opts).Extract()
		})
}

// DeleteListener deletes a listener and waits for the load balancer to become active.
func DeleteListener(ctx context.Context, client *gophercloud.ServiceClient, listenerID string, lbID string) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_listener", "delete",
		func() error {
			return listeners.Delete(ctx, client, listenerID).ExtractErr()
		})
}

// GetListenerByName retrieves a listener by name within a specific load balancer.
// Returns ErrNotFound if no listener is found, or ErrMultipleResults if multiple listeners match.
func GetListenerByName(ctx context.Context, client *gophercloud.ServiceClient, name string, lbID string) (*listeners.Listener, error) {
	pager := listeners.List(client, listeners.ListOpts{Name: name, LoadbalancerID: lbID})
	return listWithUniqueResult(ctx, "loadbalancer_listener", "list", pager, listeners.ExtractListeners)
}

// GetListenersByLoadBalancerID retrieves all listeners for a specific load balancer.
// Returns an empty slice if no listeners are found.
func GetListenersByLoadBalancerID(ctx context.Context, client *gophercloud.ServiceClient, lbID string) ([]listeners.Listener, error) {
	pager := listeners.List(client, listeners.ListOpts{LoadbalancerID: lbID})
	return list(ctx, "loadbalancer_listener", "list", pager, listeners.ExtractListeners)
}

// ==============================================================================
// Pool Operations
// ==============================================================================

// CreatePool creates a pool and waits for the load balancer to become active.
func CreatePool(ctx context.Context, client *gophercloud.ServiceClient, opts pools.CreateOptsBuilder, lbID string) (*pools.Pool, error) {
	return executeExtractAndWaitActive(ctx, client, lbID, "loadbalancer_pool", "create",
		func() (*pools.Pool, error) {
			return pools.Create(ctx, client, opts).Extract()
		})
}

// GetPoolByName retrieves a pool by name within a specific load balancer.
// Returns ErrNotFound if no pool is found, or ErrMultipleResults if multiple pools match.
func GetPoolByName(ctx context.Context, client *gophercloud.ServiceClient, name string, lbID string) (*pools.Pool, error) {
	pager := pools.List(client, pools.ListOpts{Name: name, LoadbalancerID: lbID})
	return listWithUniqueResult(ctx, "loadbalancer_pool", "list", pager, pools.ExtractPools)
}

// GetPoolByListener retrieves the pool associated with a specific listener.
// It first queries the listener to get its default pool ID, then fetches the pool directly.
// Falls back to using the listener's Pools field if no default pool is set.
// Returns ErrNotFound if no pool is found, or ErrMultipleResults if multiple pools match.
func GetPoolByListener(ctx context.Context, client *gophercloud.ServiceClient, lbID, listenerID string) (*pools.Pool, error) {
	// Get the listener by ID to retrieve its default pool ID
	listener, err := listWithUniqueResult(ctx, "loadbalancer_listener", "list",
		listeners.List(client, listeners.ListOpts{
			LoadbalancerID: lbID,
			ID:             listenerID,
		}),
		listeners.ExtractListeners)
	if err != nil {
		return nil, err
	}

	// If listener has a default pool, get it directly
	if listener.DefaultPoolID != "" {
		return getSingleResource(ctx, "loadbalancer_pool", "get",
			func() (*pools.Pool, error) {
				return pools.Get(ctx, client, listener.DefaultPoolID).Extract()
			})
	}

	// Fallback: use listener's Pools field if no default pool is set
	if len(listener.Pools) == 0 {
		return nil, cpoerrors.ErrNotFound
	}
	if len(listener.Pools) > 1 {
		return nil, cpoerrors.ErrMultipleResults
	}

	// Get the pool by ID from the listener's Pools field
	return getSingleResource(ctx, "loadbalancer_pool", "get",
		func() (*pools.Pool, error) {
			return pools.Get(ctx, client, listener.Pools[0].ID).Extract()
		})
}

// GetPools retrieves all pools for a specific load balancer.
// Returns an empty slice if no pools are found.
func GetPools(ctx context.Context, client *gophercloud.ServiceClient, lbID string) ([]pools.Pool, error) {
	pager := pools.List(client, pools.ListOpts{LoadbalancerID: lbID})
	return list(ctx, "loadbalancer_pool", "list", pager, pools.ExtractPools)
}

// UpdatePool updates a pool and waits for the load balancer to become active.
func UpdatePool(ctx context.Context, client *gophercloud.ServiceClient, lbID string, poolID string, opts pools.UpdateOpts) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_pool", "update",
		func() error {
			_, err := pools.Update(ctx, client, poolID, opts).Extract()
			return err
		})
}

// DeletePool deletes a pool and waits for the load balancer to become active.
func DeletePool(ctx context.Context, client *gophercloud.ServiceClient, poolID string, lbID string) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_pool", "delete",
		func() error {
			return pools.Delete(ctx, client, poolID).ExtractErr()
		})
}

// ==============================================================================
// Pool Member Operations
// ==============================================================================

// GetPoolMembers retrieves all members in a specific pool.
// Returns an empty slice if no members are found.
func GetPoolMembers(ctx context.Context, client *gophercloud.ServiceClient, poolID string) ([]pools.Member, error) {
	pager := pools.ListMembers(client, poolID, pools.ListMembersOpts{})
	return list(ctx, "loadbalancer_member", "list", pager, pools.ExtractMembers)
}

// BatchUpdatePoolMembers updates pool members in batch and waits for the load balancer to become active.
func BatchUpdatePoolMembers(ctx context.Context, client *gophercloud.ServiceClient, lbID string, poolID string, opts []pools.BatchUpdateMemberOpts) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_members", "update",
		func() error {
			return pools.BatchUpdateMembers(ctx, client, poolID, opts).ExtractErr()
		})
}

// ==============================================================================
// L7 Policy and Rule Operations
// ==============================================================================

// GetL7policies retrieves all L7 policies for a specific listener.
// Returns an empty slice if no policies are found.
func GetL7policies(ctx context.Context, client *gophercloud.ServiceClient, listenerID string) ([]l7policies.L7Policy, error) {
	pager := l7policies.List(client, l7policies.ListOpts{ListenerID: listenerID})
	return list(ctx, "loadbalancer_l7policy", "list", pager, l7policies.ExtractL7Policies)
}

// CreateL7Policy creates an L7 policy and waits for the load balancer to become active.
func CreateL7Policy(ctx context.Context, client *gophercloud.ServiceClient, opts l7policies.CreateOpts, lbID string) (*l7policies.L7Policy, error) {
	return executeExtractAndWaitActive(ctx, client, lbID, "loadbalancer_l7policy", "create",
		func() (*l7policies.L7Policy, error) {
			return l7policies.Create(ctx, client, opts).Extract()
		})
}

// DeleteL7policy deletes an L7 policy and waits for the load balancer to become active.
func DeleteL7policy(ctx context.Context, client *gophercloud.ServiceClient, policyID string, lbID string) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_l7policy", "delete",
		func() error {
			return l7policies.Delete(ctx, client, policyID).ExtractErr()
		})
}

// GetL7Rules retrieves all L7 rules for a specific L7 policy.
// Returns an empty slice if no rules are found.
func GetL7Rules(ctx context.Context, client *gophercloud.ServiceClient, policyID string) ([]l7policies.Rule, error) {
	pager := l7policies.ListRules(client, policyID, l7policies.ListRulesOpts{})
	return list(ctx, "loadbalancer_l7rule", "list", pager, l7policies.ExtractRules)
}

// CreateL7Rule creates an L7 rule and waits for the load balancer to become active.
func CreateL7Rule(ctx context.Context, client *gophercloud.ServiceClient, policyID string, opts l7policies.CreateRuleOpts, lbID string) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_l7rule", "create",
		func() error {
			_, err := l7policies.CreateRule(ctx, client, policyID, opts).Extract()
			return err
		})
}

// ==============================================================================
// Health Monitor Operations
// ==============================================================================

// UpdateHealthMonitor updates a health monitor and waits for the load balancer to become active.
func UpdateHealthMonitor(ctx context.Context, client *gophercloud.ServiceClient, monitorID string, opts monitors.UpdateOpts, lbID string) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_healthmonitor", "update",
		func() error {
			_, err := monitors.Update(ctx, client, monitorID, opts).Extract()
			return err
		})
}

// DeleteHealthMonitor deletes a health monitor and waits for the load balancer to become active.
func DeleteHealthMonitor(ctx context.Context, client *gophercloud.ServiceClient, monitorID string, lbID string) error {
	return executeAndWaitActive(ctx, client, lbID, "loadbalancer_healthmonitor", "delete",
		func() error {
			return monitors.Delete(ctx, client, monitorID).ExtractErr()
		})
}

// CreateHealthMonitor creates a health monitor and waits for the load balancer to become active.
func CreateHealthMonitor(ctx context.Context, client *gophercloud.ServiceClient, opts monitors.CreateOpts, lbID string) (*monitors.Monitor, error) {
	return executeExtractAndWaitActive(ctx, client, lbID, "loadbalancer_healthmonitor", "create",
		func() (*monitors.Monitor, error) {
			return monitors.Create(ctx, client, opts).Extract()
		})
}

// GetHealthMonitor retrieves a health monitor by ID.
func GetHealthMonitor(ctx context.Context, client *gophercloud.ServiceClient, monitorID string) (*monitors.Monitor, error) {
	return getSingleResource(ctx, "loadbalancer_healthmonitor", "get",
		func() (*monitors.Monitor, error) {
			return monitors.Get(ctx, client, monitorID).Extract()
		})
}
