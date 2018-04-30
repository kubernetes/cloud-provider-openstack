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

type empty struct{}

const (
	loadbalancerActiveInitDealy = 5 * time.Second
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

// GetPools retrives the pools belong to the loadbalancer.
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
	log.WithFields(log.Fields{"lbID": lbID}).Info("deleting loadbalancer with all child objects")

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

		log.WithFields(log.Fields{"name": name}).Info("creating loadbalancer")

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
	} else {
		log.WithFields(log.Fields{"name": name}).Info("loadbalancer exists")
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("error creating loadbalancer: %v", err)
	}

	log.WithFields(log.Fields{"name": name, "id": loadbalancer.ID}).Info("loadbalancer created")
	return loadbalancer, nil
}

// EnsureListener creates a loadbalancer listener in octavia if it does not exist, wait for the loadbalancer to be ACTIVE.
func (os *OpenStack) EnsureListener(name string, lbID string) (*listeners.Listener, error) {
	listener, err := os.getListenerByName(name, lbID)
	if err != nil {
		if err != ErrNotFound {
			return nil, fmt.Errorf("error getting listener %s: %v", name, err)
		}

		log.WithFields(log.Fields{"lb": lbID, "listenerName": name}).Info("creating listener")

		listener, err = listeners.Create(os.octavia, listeners.CreateOpts{
			Name:           name,
			Protocol:       "HTTP",
			ProtocolPort:   80, // Ingress Controller only supports http/https for now
			LoadbalancerID: lbID,
		}).Extract()
		if err != nil {
			return nil, fmt.Errorf("error creating listener: %v", err)
		}
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return nil, fmt.Errorf("error creating listener: %v", err)
	}

	log.WithFields(log.Fields{"lb": lbID, "listenerName": name}).Info("listener created")
	return listener, nil
}

// EnsurePoolMembers deletes the old pool with its members, then creates a new pool with members if deleted flag is not set.
func (os *OpenStack) EnsurePoolMembers(deleted bool, poolName, lbID, listenerID string, servicePort *int, nodes []*apiv1.Node) (*string, error) {
	pool, err := os.getPoolByName(poolName, lbID)
	if err != nil {
		if err != ErrNotFound {
			return nil, fmt.Errorf("error getting pool %s: %v", poolName, err)
		}

		if deleted {
			return nil, nil
		}
	} else {
		// Delete the existing pool, members will be deleted automatically
		err = pools.Delete(os.octavia, pool.ID).ExtractErr()
		if err != nil && !isNotFound(err) {
			return nil, fmt.Errorf("error deleting pool %s: %v", pool.ID, err)
		}

		_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
		if err != nil {
			return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
		}

		if deleted {
			log.WithFields(log.Fields{"name": poolName, "ID": pool.ID}).Info("pool deleted")
			return nil, nil
		}
	}

	// Creates new pool
	log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "poolName": poolName}).Info("creating pool")

	var opts pools.CreateOptsBuilder
	if lbID != "" {
		opts = pools.CreateOpts{
			Name:           poolName,
			Protocol:       "HTTP",
			LBMethod:       pools.LBMethodRoundRobin,
			LoadbalancerID: lbID,
			Persistence:    nil,
		}
	} else {
		opts = pools.CreateOpts{
			Name:        poolName,
			Protocol:    "HTTP",
			LBMethod:    pools.LBMethodRoundRobin,
			ListenerID:  listenerID,
			Persistence: nil,
		}
	}
	pool, err = pools.Create(os.octavia, opts).Extract()
	if err != nil {
		return nil, fmt.Errorf("error creating pool for listener %s: %v", listenerID, err)
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
	}

	log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "poolName": poolName, "pooID": pool.ID}).Info("pool created")

	// Adds members to the new pool
	for _, node := range nodes {
		addr, err := getNodeAddressForLB(node)
		if err != nil {
			// Node failure, do not create member
			log.WithFields(log.Fields{"node": node.Name, "poolName": poolName, "error": err}).Warn("failed to create LB pool member for node")
			continue
		}

		log.WithFields(log.Fields{"addr": addr, "poolID": pool.ID, "port": *servicePort}).Info("adding new member to pool")

		_, err = pools.CreateMember(os.octavia, pool.ID, pools.CreateMemberOpts{
			ProtocolPort: *servicePort,
			Address:      addr,
		}).Extract()
		if err != nil {
			return nil, fmt.Errorf("error creating pool member %s: %v", addr, err)
		}

		_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
		if err != nil {
			return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
		}

		log.WithFields(log.Fields{"addr": addr, "poolID": pool.ID, "port": *servicePort}).Info("member added to pool")
	}

	return &pool.ID, nil
}

// EnsurePolicyRules creates l7 policy with rules for listener or delete policy if deleted flag is set.
// For policy creation, the old policy will be deleted anyway.
func (os *OpenStack) EnsurePolicyRules(deleted bool, policyName, lbID, listenerID, poolID, host, path string) error {
	policy, err := os.getL7policy(policyName, listenerID, poolID)
	if err != nil {
		if err != ErrNotFound {
			return fmt.Errorf("error getting policy %s: %v", policyName, err)
		}

		if deleted {
			log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "policyName": policyName}).Info("policy not exists")
			return nil
		}
	} else {
		// Delete old policy first
		err = l7policies.Delete(os.octavia, policy.ID).ExtractErr()
		if err != nil && !isNotFound(err) {
			return fmt.Errorf("error deleting policy %s: %v", policy.ID, err)
		}

		_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
		if err != nil {
			return fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
		}

		if deleted {
			log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "policyName": policyName}).Info("policy deleted")
			return nil
		}
	}

	// Create new policy with rules
	log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "policyName": policyName}).Info("creating policy")

	policy, err = l7policies.Create(os.octavia, l7policies.CreateOpts{
		Name:           policyName,
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

	log.WithFields(log.Fields{"lb": lbID, "listenserID": listenerID, "policyName": policyName}).Info("policy created")

	if host != "" {
		log.WithFields(log.Fields{"type": l7policies.TypeHostName, "host": host, "policyName": policyName}).Info("creating policy rule")

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

		log.WithFields(log.Fields{"type": l7policies.TypeHostName, "host": host, "policyName": policyName}).Info("policy rule created")
	}

	if path != "" {
		log.WithFields(log.Fields{"type": l7policies.TypePath, "path": path, "policyName": policyName}).Info("creating policy rule")

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

		log.WithFields(log.Fields{"type": l7policies.TypePath, "path": path, "policyName": policyName}).Info("policy rule created")
	}

	return nil
}

// UpdateLoadbalancerMembers update members for all the pools in the specified loadbalancer.
func (os *OpenStack) UpdateLoadbalancerMembers(lbID string, nodes []*apiv1.Node) error {
	lbPools, err := os.GetPools(lbID, false)
	if err != nil {
		return err
	}

	addrs := map[string]empty{}
	for _, node := range nodes {
		addr, err := getNodeAddressForLB(node)
		if err != nil {
			log.WithFields(log.Fields{"name": node.ObjectMeta.Name}).Warningf("Failed to get node address: %v", err)
			continue
		}
		addrs[addr] = empty{}
	}

	for _, pool := range lbPools {
		log.WithFields(log.Fields{"poolID": pool.ID}).Info("Starting to update pool members")

		members, err := os.GetMembers(pool.ID)
		if err != nil {
			log.WithFields(log.Fields{"poolID": pool.ID}).Errorf("Failed to get pool members: %v", err)
			continue
		}

		// Members have the same ProtocolPort
		servicePort := members[0].ProtocolPort

		addrMemberMapping := make(map[string]pools.Member)
		for _, member := range members {
			addrMemberMapping[member.Address] = member
		}

		// Add any new members for this port
		for addr := range addrs {
			if _, ok := addrMemberMapping[addr]; ok {
				continue
			}

			log.WithFields(log.Fields{"poolID": pool.ID, "memberAddress": addr}).Info("Adding new member to the pool")

			// This is a new node joined to the cluster
			if _, err = pools.CreateMember(os.octavia, pool.ID, pools.CreateMemberOpts{
				ProtocolPort: servicePort,
				Address:      addr,
			}).Extract(); err != nil {
				return err
			}

			log.WithFields(log.Fields{"poolID": pool.ID, "memberAddress": addr}).Info("Finished to add new member to the pool")

			_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
			if err != nil {
				return fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
			}
		}

		// Remove any old members
		for _, member := range addrMemberMapping {
			if _, ok := addrs[member.Address]; ok {
				continue
			}

			log.WithFields(log.Fields{"poolID": pool.ID, "memberAddress": member.Address}).Info("Deleting old member from the pool")

			err = pools.DeleteMember(os.octavia, pool.ID, member.ID).ExtractErr()
			if err != nil && !isNotFound(err) {
				log.WithFields(log.Fields{"poolID": pool.ID, "memberAddress": member.Address}).Error("Failed to delete old member from the pool")
				return err
			}

			log.WithFields(log.Fields{"poolID": pool.ID, "memberAddress": member.Address}).Info("Finished to delete old member from the pool")

			_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
			if err != nil {
				return fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
			}
		}

		log.WithFields(log.Fields{"poolID": pool.ID}).Info("Finished to update pool members")
	}

	return nil
}
