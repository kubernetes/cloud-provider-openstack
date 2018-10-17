/*
Copyright 2018 The Kubernetes Authors.

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
	"errors"
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/l7policies"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/pagination"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	loadbalancerActiveInitDealy = 3 * time.Second
	loadbalancerActiveFactor    = 1
	loadbalancerActiveSteps     = 240

	activeStatus = "ACTIVE"
	errorStatus  = "ERROR"
)

var (
	// ErrNotFound is used to inform that the object is missing
	ErrNotFound = errors.New("failed to find object")

	// ErrMultipleResults is used when we unexpectedly get back multiple results
	ErrMultipleResults = errors.New("multiple results where only one expected")
)

func getNodeAddressForLB(node *apiv1.Node) (string, error) {
	addrs := node.Status.Addresses
	if len(addrs) == 0 {
		return "", errors.New("no address found for host")
	}

	for _, addr := range addrs {
		if addr.Type == apiv1.NodeInternalIP {
			return addr.Address, nil
		}
	}

	return addrs[0].Address, nil
}

func (os *OpenStack) waitLoadbalancerActiveProvisioningStatus(loadbalancerID string) (string, error) {
	backoff := wait.Backoff{
		Duration: loadbalancerActiveInitDealy,
		Factor:   loadbalancerActiveFactor,
		Steps:    loadbalancerActiveSteps,
	}

	var provisioningStatus string
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		loadbalancer, err := loadbalancers.Get(os.octavia, loadbalancerID).Extract()
		if err != nil {
			return false, err
		}
		provisioningStatus = loadbalancer.ProvisioningStatus
		if loadbalancer.ProvisioningStatus == activeStatus {
			return true, nil
		} else if loadbalancer.ProvisioningStatus == errorStatus {
			return true, fmt.Errorf("loadbalancer has gone into ERROR state")
		} else {
			return false, nil
		}

	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("loadbalancer failed to go into ACTIVE provisioning status within alloted time")
	}
	return provisioningStatus, err
}

// GetLoadbalancerByName retrieves loadbalancer object
func (os *OpenStack) GetLoadbalancerByName(name string) (*loadbalancers.LoadBalancer, error) {
	opts := loadbalancers.ListOpts{
		Name: name,
	}
	pager := loadbalancers.List(os.octavia, opts)
	loadbalancerList := make([]loadbalancers.LoadBalancer, 0, 1)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		v, err := loadbalancers.ExtractLoadBalancers(page)
		if err != nil {
			return false, err
		}
		loadbalancerList = append(loadbalancerList, v...)
		if len(loadbalancerList) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(loadbalancerList) == 0 {
		return nil, ErrNotFound
	}

	return &loadbalancerList[0], nil
}

func (os *OpenStack) getListenerByName(name string, lbID string) (*listeners.Listener, error) {
	opts := listeners.ListOpts{
		Name:           name,
		LoadbalancerID: lbID,
	}
	pager := listeners.List(os.octavia, opts)
	listenerList := make([]listeners.Listener, 0, 1)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		v, err := listeners.ExtractListeners(page)
		if err != nil {
			return false, err
		}
		listenerList = append(listenerList, v...)
		if len(listenerList) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(listenerList) == 0 {
		return nil, ErrNotFound
	}

	return &listenerList[0], nil
}

func (os *OpenStack) getPoolByName(name string, lbID string) (*pools.Pool, error) {
	listenerPools := make([]pools.Pool, 0, 1)
	opts := pools.ListOpts{
		Name:           name,
		LoadbalancerID: lbID,
	}
	err := pools.List(os.octavia, opts).EachPage(func(page pagination.Page) (bool, error) {
		v, err := pools.ExtractPools(page)
		if err != nil {
			return false, err
		}
		listenerPools = append(listenerPools, v...)
		if len(listenerPools) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(listenerPools) == 0 {
		return nil, ErrNotFound
	} else if len(listenerPools) > 1 {
		return nil, ErrMultipleResults
	}

	return &listenerPools[0], nil
}

// GetPools retrives the pools belong to the loadbalancer. If shared is true, only return the shared pools.
func (os *OpenStack) GetPools(lbID string, shared bool) ([]pools.Pool, error) {
	var lbPools []pools.Pool

	opts := pools.ListOpts{
		LoadbalancerID: lbID,
	}
	err := pools.List(os.octavia, opts).EachPage(func(page pagination.Page) (bool, error) {
		v, err := pools.ExtractPools(page)
		if err != nil {
			return false, err
		}
		for _, p := range v {
			if shared && len(p.Listeners) != 0 {
				continue
			}
			lbPools = append(lbPools, p)
		}

		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return lbPools, nil
}

// GetMembers retrieve all the members of the specified pool
func (os *OpenStack) GetMembers(poolID string) ([]pools.Member, error) {
	var members []pools.Member

	opts := pools.ListMembersOpts{}
	err := pools.ListMembers(os.octavia, poolID, opts).EachPage(func(page pagination.Page) (bool, error) {
		v, err := pools.ExtractMembers(page)
		if err != nil {
			return false, err
		}
		members = append(members, v...)
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return members, nil
}

// DeletePool deletes a pool
func (os *OpenStack) DeletePool(poolID string, lbID string) error {
	if err := pools.Delete(os.octavia, poolID).ExtractErr(); err != nil {
		return fmt.Errorf("failed to delete pool %s: %v", poolID, err)
	}

	_, err := os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return fmt.Errorf("failed to wait for loadbalancer to be active: %v", err)
	}

	return nil
}

// GetL7policies retrieves all l7 policies for the given listener.
func (os *OpenStack) GetL7policies(listenerID string) ([]l7policies.L7Policy, error) {
	var policies []l7policies.L7Policy
	opts := l7policies.ListOpts{
		ListenerID: listenerID,
	}
	err := l7policies.List(os.octavia, opts).EachPage(func(page pagination.Page) (bool, error) {
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

func (os *OpenStack) getL7policy(name string, listenerID, poolID string) (*l7policies.L7Policy, error) {
	policies := make([]l7policies.L7Policy, 0, 1)
	opts := l7policies.ListOpts{
		Name:           name,
		ListenerID:     listenerID,
		RedirectPoolID: poolID,
	}
	err := l7policies.List(os.octavia, opts).EachPage(func(page pagination.Page) (bool, error) {
		v, err := l7policies.ExtractL7Policies(page)
		if err != nil {
			return false, err
		}
		policies = append(policies, v...)
		if len(policies) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(policies) == 0 {
		return nil, ErrNotFound
	}

	return &policies[0], nil
}

// DeleteL7policy deletes a l7 policy
func (os *OpenStack) DeleteL7policy(policyID string, lbID string) error {
	if err := l7policies.Delete(os.octavia, policyID).ExtractErr(); err != nil {
		return fmt.Errorf("failed to delete l7 policy: %v", err)
	}

	_, err := os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return fmt.Errorf("failed to wait for loadbalancer to be active: %v", err)
	}

	return nil
}

// DeleteLoadbalancer deletes a loadbalancer with all its child objects.
func (os *OpenStack) DeleteLoadbalancer(lbID string) error {
	err := loadbalancers.Delete(os.octavia, lbID, loadbalancers.DeleteOpts{Cascade: true}).ExtractErr()
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("error deleting loadbalancer %s: %v", lbID, err)
	}

	log.WithFields(log.Fields{"lbID": lbID}).Info("loadbalancer deleted")

	return nil
}

// EnsureLoadBalancer creates a loadbalancer in octavia if it does not exist, wait for the loadbalancer to be ACTIVE.
func (os *OpenStack) EnsureLoadBalancer(name string, subnetID string) (*loadbalancers.LoadBalancer, error) {
	loadbalancer, err := os.GetLoadbalancerByName(name)
	if err != nil {
		if err != ErrNotFound {
			return nil, fmt.Errorf("error getting loadbalancer %s: %v", name, err)
		}

		log.WithFields(log.Fields{"name": name}).Debug("creating loadbalancer")

		createOpts := loadbalancers.CreateOpts{
			Name:        name,
			Description: "Created by Kubernetes",
			VipSubnetID: subnetID,
			Provider:    "octavia",
		}
		loadbalancer, err = loadbalancers.Create(os.octavia, createOpts).Extract()
		if err != nil {
			return nil, fmt.Errorf("error creating loadbalancer %v: %v", createOpts, err)
		}

		log.WithFields(log.Fields{"name": name, "ID": loadbalancer.ID}).Info("loadbalancer created")
	} else {
		log.WithFields(log.Fields{"name": name}).Debug("loadbalancer exists")
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("error creating loadbalancer: %v", err)
	}

	return loadbalancer, nil
}

// UpdateLoadBalancerDescription updates the load balancer description field.
func (os *OpenStack) UpdateLoadBalancerDescription(lbID string, newDescription string) error {
	_, err := loadbalancers.Update(os.octavia, lbID, loadbalancers.UpdateOpts{
		Description: newDescription,
	}).Extract()
	if err != nil {
		return fmt.Errorf("failed to update loadbalancer description: %v", err)
	}

	log.WithFields(log.Fields{"lb": lbID}).Debug("loadbalancer description updated")
	return nil
}

// EnsureListener creates a loadbalancer listener in octavia if it does not exist, wait for the loadbalancer to be ACTIVE.
func (os *OpenStack) EnsureListener(name string, lbID string) (*listeners.Listener, error) {
	listener, err := os.getListenerByName(name, lbID)
	if err != nil {
		if err != ErrNotFound {
			return nil, fmt.Errorf("error getting listener %s: %v", name, err)
		}

		log.WithFields(log.Fields{"lb": lbID, "listenerName": name}).Debug("creating listener")

		listener, err = listeners.Create(os.octavia, listeners.CreateOpts{
			Name:           name,
			Protocol:       "TCP",
			ProtocolPort:   80, // Ingress Controller only supports http/https for now
			LoadbalancerID: lbID,
		}).Extract()
		if err != nil {
			return nil, fmt.Errorf("error creating listener: %v", err)
		}

		log.WithFields(log.Fields{"lb": lbID, "listenerName": name}).Info("listener created")
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return nil, fmt.Errorf("error creating listener: %v", err)
	}

	return listener, nil
}

// EnsurePoolMembers ensure the pool and its members exist if deleted flag is not set, delete the pool and all its members otherwise.
func (os *OpenStack) EnsurePoolMembers(deleted bool, poolName string, lbID string, listenerID string, nodePort *int, nodes []*apiv1.Node) (*string, error) {
	if deleted {
		pool, err := os.getPoolByName(poolName, lbID)
		if err != nil {
			if err != ErrNotFound {
				return nil, fmt.Errorf("error getting pool %s: %v", poolName, err)
			}
			return nil, nil
		}

		// Delete the existing pool, members are deleted automatically
		err = pools.Delete(os.octavia, pool.ID).ExtractErr()
		if err != nil && !isNotFound(err) {
			return nil, fmt.Errorf("error deleting pool %s: %v", pool.ID, err)
		}

		_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
		if err != nil {
			return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
		}

		return nil, nil
	}

	pool, err := os.getPoolByName(poolName, lbID)
	if err != nil {
		if err != ErrNotFound {
			return nil, fmt.Errorf("error getting pool %s: %v", poolName, err)
		} else {
			log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "poolName": poolName}).Debug("creating pool")

			// Create new pool
			var opts pools.CreateOptsBuilder
			if listenerID != "" {
				opts = pools.CreateOpts{
					Name:        poolName,
					Protocol:    "TCP",
					LBMethod:    pools.LBMethodRoundRobin,
					ListenerID:  listenerID,
					Persistence: nil,
				}
			} else {
				opts = pools.CreateOpts{
					Name:           poolName,
					Protocol:       "TCP",
					LBMethod:       pools.LBMethodRoundRobin,
					LoadbalancerID: lbID,
					Persistence:    nil,
				}
			}
			pool, err = pools.Create(os.octavia, opts).Extract()
			if err != nil {
				return nil, fmt.Errorf("error creating pool: %v", err)
			}

			log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "poolName": poolName, "pooID": pool.ID}).Info("pool created")
		}
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
	}

	// Batch update pool members
	var members []pools.BatchUpdateMemberOpts
	for _, node := range nodes {
		addr, err := getNodeAddressForLB(node)
		if err != nil {
			// Node failure, do not create member
			log.WithFields(log.Fields{"node": node.Name, "poolName": poolName, "pooID": pool.ID, "error": err}).Warn("failed to create LB pool member for node")
			continue
		}

		member := pools.BatchUpdateMemberOpts{
			Address:      addr,
			ProtocolPort: *nodePort,
		}
		members = append(members, member)
	}
	if err := pools.BatchUpdateMembers(os.octavia, pool.ID, members).ExtractErr(); err != nil {
		return nil, fmt.Errorf("error batch updating members for pool %s: %v", pool.ID, err)
	}
	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
	}

	log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "poolName": poolName, "pooID": pool.ID}).Info("pool members updated")

	return &pool.ID, nil
}

// CreatePolicyRules creates l7 policy and its rules for the listener
func (os *OpenStack) CreatePolicyRules(lbID, listenerID, poolID, host, path string) error {
	log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID}).Debug("creating policy")

	policy, err := l7policies.Create(os.octavia, l7policies.CreateOpts{
		ListenerID:     listenerID,
		Action:         l7policies.ActionRedirectToPool,
		Description:    "Created by kubernetes ingress",
		RedirectPoolID: poolID,
	}).Extract()
	if err != nil {
		return fmt.Errorf("error creating l7policy: %v", err)
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
	}

	log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "policyID": policy.ID}).Info("policy created")

	if host != "" {
		log.WithFields(log.Fields{"type": l7policies.TypeHostName, "host": host, "policyID": policy.ID, "listenserID": listenerID}).Debug("creating policy rule")

		// Create HOST_NAME type rule
		_, err = l7policies.CreateRule(os.octavia, policy.ID, l7policies.CreateRuleOpts{
			RuleType:    l7policies.TypeHostName,
			CompareType: l7policies.CompareTypeEqual,
			Value:       host,
		}).Extract()
		if err != nil {
			return fmt.Errorf("error creating l7 rule: %v", err)
		}

		_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
		if err != nil {
			return fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
		}

		log.WithFields(log.Fields{"type": l7policies.TypeHostName, "host": host, "policyID": policy.ID, "listenserID": listenerID}).Info("policy rule created")
	}

	if path != "" {
		log.WithFields(log.Fields{"type": l7policies.TypePath, "path": path, "policyID": policy.ID, "listenserID": listenerID}).Debug("creating policy rule")

		// Create PATH type rule
		_, err = l7policies.CreateRule(os.octavia, policy.ID, l7policies.CreateRuleOpts{
			RuleType:    l7policies.TypePath,
			CompareType: l7policies.CompareTypeStartWith,
			Value:       path,
		}).Extract()
		if err != nil {
			return fmt.Errorf("error creating l7 rule: %v", err)
		}

		_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
		if err != nil {
			return fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
		}

		log.WithFields(log.Fields{"type": l7policies.TypePath, "path": path, "policyID": policy.ID, "listenserID": listenerID}).Info("policy rule created")
	}

	return nil
}

// UpdateLoadbalancerMembers update members for all the pools in the specified load balancer.
func (os *OpenStack) UpdateLoadbalancerMembers(lbID string, nodes []*apiv1.Node) error {
	lbPools, err := os.GetPools(lbID, false)
	if err != nil {
		return err
	}

	for _, pool := range lbPools {
		log.WithFields(log.Fields{"poolID": pool.ID}).Debug("Starting to update pool members")

		members, err := os.GetMembers(pool.ID)
		if err != nil {
			log.WithFields(log.Fields{"poolID": pool.ID}).Errorf("Failed to get pool members: %v", err)
			continue
		}

		// Members have the same ProtocolPort
		nodePort := members[0].ProtocolPort

		if _, err = os.EnsurePoolMembers(false, pool.Name, lbID, "", &nodePort, nodes); err != nil {
			return err
		}

		log.WithFields(log.Fields{"poolID": pool.ID, "lbID": lbID}).Info("Finished to update pool members")
	}

	return nil
}
