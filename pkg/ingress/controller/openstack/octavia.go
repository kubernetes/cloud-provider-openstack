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
	"reflect"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/l7policies"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/cloud-provider-openstack/pkg/ingress/utils"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

const (
	loadbalancerActiveInitDealy = 3 * time.Second
	loadbalancerActiveFactor    = 1
	loadbalancerActiveSteps     = 240

	activeStatus = "ACTIVE"
	errorStatus  = "ERROR"
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

type IngPolicy struct {
	RedirectPoolName string
	Opts             l7policies.CreateOpts
	RulesOpts        []l7policies.CreateRuleOpts
}

type ExistingPolicy struct {
	Policy l7policies.L7Policy
	Rules  []l7policies.Rule
}

type IngPool struct {
	Name        string
	Opts        pools.CreateOptsBuilder
	PoolMembers []pools.BatchUpdateMemberOpts
}

// ResourceTracker tracks the resources created for Ingress.
type ResourceTracker struct {
	client *gophercloud.ServiceClient
	logger *log.Entry

	lbID       string
	listenerID string

	newPoolNames sets.String
	newPools     []IngPool
	newPolicies  []IngPolicy
	// A map from rule hash key to pool ID.
	newPolicyRuleMapping map[string]string

	// A map from pool name to pool ID
	oldPoolMapping map[string]string
	oldPools       []pools.Pool
	// A map from rule hash key to policy.
	oldPolicyMapping map[string]ExistingPolicy
}

func NewResourceTracker(ingressName string, client *gophercloud.ServiceClient, lbID string, listenerID string, newPools []IngPool, newPolicies []IngPolicy, oldPools []pools.Pool, oldPolicies []ExistingPolicy) *ResourceTracker {
	newPoolNames := sets.NewString()
	oldPoolMapping := make(map[string]string)
	for _, pool := range newPools {
		newPoolNames.Insert(pool.Name)
	}
	for _, pool := range oldPools {
		oldPoolMapping[pool.Name] = pool.ID
	}

	oldPolicyMapping := make(map[string]ExistingPolicy)
	for _, policy := range oldPolicies {
		ruleIden := sets.NewString()
		for _, rule := range policy.Rules {
			ruleIden.Insert(rule.RuleType, rule.CompareType, rule.Value)
		}
		rulesKey := utils.Hash(strings.Join(ruleIden.List(), ","))
		oldPolicyMapping[rulesKey] = policy
	}

	logger := log.WithFields(log.Fields{"ingress": ingressName, "lbID": lbID})

	var oldPoolIDs []string
	for _, poolID := range oldPoolMapping {
		oldPoolIDs = append(oldPoolIDs, poolID)
	}
	logger.Debugf("Existing pools: %v", oldPoolIDs)

	oldPoliciesTmp := make(map[string]string)
	for key, policy := range oldPolicyMapping {
		oldPoliciesTmp[key] = policy.Policy.RedirectPoolID
	}
	logger.Debugf("Existing l7 policies: %v", oldPoliciesTmp)

	rt := &ResourceTracker{
		client:               client,
		logger:               logger,
		lbID:                 lbID,
		listenerID:           listenerID,
		newPoolNames:         newPoolNames,
		newPools:             newPools,
		newPolicies:          newPolicies,
		newPolicyRuleMapping: make(map[string]string),
		oldPools:             oldPools,
		oldPoolMapping:       oldPoolMapping,
		oldPolicyMapping:     oldPolicyMapping,
	}

	return rt
}

// createResources only creates resources when necessary.
func (rt *ResourceTracker) CreateResources() error {
	poolMapping := make(map[string]string)
	for _, pool := range rt.newPools {
		// Different ingress paths may configure the same service, but we only need to create one pool.
		if _, isPresent := poolMapping[pool.Name]; isPresent {
			continue
		}

		poolID, isPresent := rt.oldPoolMapping[pool.Name]
		if !isPresent {
			rt.logger.WithFields(log.Fields{"poolName": pool.Name}).Info("creating pool")
			newPool, err := openstackutil.CreatePool(rt.client, pool.Opts, rt.lbID)
			if err != nil {
				return fmt.Errorf("failed to create pool %s, error: %v", pool.Name, err)
			}

			poolID = newPool.ID
			rt.logger.WithFields(log.Fields{"poolName": pool.Name, "poolID": poolID}).Info("pool created")
		}

		poolMapping[pool.Name] = poolID

		rt.logger.WithFields(log.Fields{"poolName": pool.Name, "poolID": poolID}).Info("updating pool members")
		if err := openstackutil.BatchUpdatePoolMembers(rt.client, rt.lbID, poolID, pool.PoolMembers); err != nil {
			return fmt.Errorf("failed to update pool members, error: %v", err)
		}
		rt.logger.WithFields(log.Fields{"poolName": pool.Name, "poolID": poolID}).Info("pool members updated ")
	}

	var curPoolIDs []string
	for _, id := range poolMapping {
		curPoolIDs = append(curPoolIDs, id)
	}
	rt.logger.Debugf("Current pools: %v", curPoolIDs)

	for _, policy := range rt.newPolicies {
		newRuleIden := sets.NewString()
		for _, opt := range policy.RulesOpts {
			newRuleIden.Insert(string(opt.RuleType), string(opt.CompareType), opt.Value)
		}
		rulesKey := utils.Hash(strings.Join(newRuleIden.List(), ","))

		poolID := poolMapping[policy.RedirectPoolName]

		oldPolicy, isPresent := rt.oldPolicyMapping[rulesKey]
		if !isPresent || oldPolicy.Policy.RedirectPoolID != poolID {
			// Create new policy with rules
			rt.logger.WithFields(log.Fields{"listenerID": rt.listenerID, "poolID": poolID}).Info("creating l7 policy")
			policy.Opts.RedirectPoolID = poolID
			newPolicy, err := openstackutil.CreateL7Policy(rt.client, policy.Opts, rt.lbID)
			if err != nil {
				return fmt.Errorf("failed to create l7policy, error: %v", err)
			}
			rt.logger.WithFields(log.Fields{"listenerID": rt.listenerID, "poolID": poolID}).Info("l7 policy created")

			rt.logger.WithFields(log.Fields{"listenerID": rt.listenerID, "policyID": newPolicy.ID}).Info("creating l7 rules")
			for _, opt := range policy.RulesOpts {
				if err := openstackutil.CreateL7Rule(rt.client, newPolicy.ID, opt, rt.lbID); err != nil {
					return fmt.Errorf("failed to create l7 rules for policy %s, error: %v", newPolicy.ID, err)
				}
			}
			rt.logger.WithFields(log.Fields{"listenerID": rt.listenerID, "policyID": newPolicy.ID}).Info("l7 rules created")
		}

		rt.newPolicyRuleMapping[rulesKey] = poolID
	}

	rt.logger.Debugf("Current l7 policies: %v", rt.newPolicyRuleMapping)

	return nil
}

func (rt *ResourceTracker) CleanupResources() error {
	for key, oldPolicy := range rt.oldPolicyMapping {
		poolID, isPresent := rt.newPolicyRuleMapping[key]
		if !isPresent || poolID != oldPolicy.Policy.RedirectPoolID {
			// Delete invalid policy
			rt.logger.WithFields(log.Fields{"policyID": oldPolicy.Policy.ID}).Info("deleting policy")
			if err := openstackutil.DeleteL7policy(rt.client, oldPolicy.Policy.ID, rt.lbID); err != nil {
				return fmt.Errorf("failed to delete l7 policy %s, error: %v", oldPolicy.Policy.ID, err)
			}
			rt.logger.WithFields(log.Fields{"policyID": oldPolicy.Policy.ID}).Info("policy deleted")
		}
	}

	for _, pool := range rt.oldPools {
		if !rt.newPoolNames.Has(pool.Name) {
			// Delete unused pool
			rt.logger.WithFields(log.Fields{"poolID": pool.ID}).Info("deleting pool")
			if err := openstackutil.DeletePool(rt.client, pool.ID, rt.lbID); err != nil {
				return fmt.Errorf("failed to delete pool %s, error: %v", pool.ID, err)
			}
			rt.logger.WithFields(log.Fields{"poolID": pool.ID}).Info("pool deleted")
		}
	}

	return nil
}

func (os *OpenStack) waitLoadbalancerActiveProvisioningStatus(loadbalancerID string) (string, error) {
	backoff := wait.Backoff{
		Duration: loadbalancerActiveInitDealy,
		Factor:   loadbalancerActiveFactor,
		Steps:    loadbalancerActiveSteps,
	}

	var provisioningStatus string
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		loadbalancer, err := loadbalancers.Get(os.Octavia, loadbalancerID).Extract()
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

// EnsureLoadBalancer creates a loadbalancer in octavia if it does not exist, wait for the loadbalancer to be ACTIVE.
func (os *OpenStack) EnsureLoadBalancer(name string, subnetID string, ingNamespace string, ingName string, clusterName string) (*loadbalancers.LoadBalancer, error) {
	logger := log.WithFields(log.Fields{"ingress": fmt.Sprintf("%s/%s", ingNamespace, ingName)})

	loadbalancer, err := openstackutil.GetLoadbalancerByName(os.Octavia, name)
	if err != nil {
		if err != cpoerrors.ErrNotFound {
			return nil, fmt.Errorf("error getting loadbalancer %s: %v", name, err)
		}

		var provider string
		if os.config.Octavia.Provider == "" {
			provider = "octavia"
		} else {
			provider = os.config.Octavia.Provider
		}

		createOpts := loadbalancers.CreateOpts{
			Name:        name,
			Description: fmt.Sprintf("Kubernetes ingress %s in namespace %s from cluster %s", ingName, ingNamespace, clusterName),
			VipSubnetID: subnetID,
			Provider:    provider,
		}
		loadbalancer, err = loadbalancers.Create(os.Octavia, createOpts).Extract()
		if err != nil {
			return nil, fmt.Errorf("error creating loadbalancer %v: %v", createOpts, err)
		}

		logger.WithFields(log.Fields{"lbName": name, "lbID": loadbalancer.ID}).Info("creating loadbalancer")
	} else {
		logger.WithFields(log.Fields{"lbName": name, "lbID": loadbalancer.ID}).Debug("loadbalancer exists")
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("loadbalancer %s not in ACTIVE status, error: %v", loadbalancer.ID, err)
	}

	return loadbalancer, nil
}

// UpdateLoadBalancerDescription updates the load balancer description field.
func (os *OpenStack) UpdateLoadBalancerDescription(lbID string, newDescription string) error {
	_, err := loadbalancers.Update(os.Octavia, lbID, loadbalancers.UpdateOpts{
		Description: &newDescription,
	}).Extract()
	if err != nil {
		return fmt.Errorf("failed to update loadbalancer description: %v", err)
	}

	log.WithFields(log.Fields{"lbID": lbID}).Debug("loadbalancer description updated")
	return nil
}

// EnsureListener creates a loadbalancer listener in octavia if it does not exist, wait for the loadbalancer to be ACTIVE.
func (os *OpenStack) EnsureListener(name string, lbID string, secretRefs []string, listenerAllowedCIDRs []string) (*listeners.Listener, error) {
	listener, err := openstackutil.GetListenerByName(os.Octavia, name, lbID)
	if err != nil {
		if err != cpoerrors.ErrNotFound {
			return nil, fmt.Errorf("error getting listener %s: %v", name, err)
		}

		log.WithFields(log.Fields{"lbID": lbID, "listenerName": name}).Info("creating listener")

		opts := listeners.CreateOpts{
			Name:           name,
			Protocol:       "HTTP",
			ProtocolPort:   80, // Ingress Controller only supports http/https for now
			LoadbalancerID: lbID,
		}
		if len(secretRefs) > 0 {
			opts.DefaultTlsContainerRef = secretRefs[0]
			opts.SniContainerRefs = secretRefs
			opts.ProtocolPort = 443
			opts.Protocol = "TERMINATED_HTTPS"
		}
		if len(listenerAllowedCIDRs) > 0 {
			opts.AllowedCIDRs = listenerAllowedCIDRs
		}
		listener, err = listeners.Create(os.Octavia, opts).Extract()
		if err != nil {
			return nil, fmt.Errorf("error creating listener: %v", err)
		}

		log.WithFields(log.Fields{"lbID": lbID, "listenerName": name}).Info("listener created")
	} else {
		if len(listenerAllowedCIDRs) > 0 && !reflect.DeepEqual(listener.AllowedCIDRs, listenerAllowedCIDRs) {
			_, err := listeners.Update(os.Octavia, listener.ID, listeners.UpdateOpts{
				AllowedCIDRs: &listenerAllowedCIDRs,
			}).Extract()
			if err != nil {
				return nil, fmt.Errorf("failed to update listener allowed CIDRs: %v", err)
			}

			log.WithFields(log.Fields{"listenerID": listener.ID}).Debug("listener allowed CIDRs updated")
		}
	}

	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return nil, fmt.Errorf("loadbalancer %s not in ACTIVE status after creating listener, error: %v", lbID, err)
	}

	return listener, nil
}

// EnsurePoolMembers ensure the pool and its members exist if deleted flag is not set, delete the pool and all its members otherwise.
func (os *OpenStack) EnsurePoolMembers(deleted bool, poolName string, lbID string, listenerID string, nodePort *int, nodes []*apiv1.Node) (*string, error) {
	logger := log.WithFields(log.Fields{"lbID": lbID, "listenerID": listenerID, "poolName": poolName})

	if deleted {
		pool, err := openstackutil.GetPoolByName(os.Octavia, poolName, lbID)
		if err != nil {
			if err != cpoerrors.ErrNotFound {
				return nil, fmt.Errorf("error getting pool %s: %v", poolName, err)
			}
			return nil, nil
		}

		// Delete the existing pool, members are deleted automatically
		err = pools.Delete(os.Octavia, pool.ID).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, fmt.Errorf("error deleting pool %s: %v", pool.ID, err)
		}

		_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
		if err != nil {
			return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
		}

		return nil, nil
	}

	pool, err := openstackutil.GetPoolByName(os.Octavia, poolName, lbID)
	if err != nil {
		if err != cpoerrors.ErrNotFound {
			return nil, fmt.Errorf("error getting pool %s: %v", poolName, err)
		}

		logger.Info("creating pool")

		// Create new pool
		var opts pools.CreateOptsBuilder
		if listenerID != "" {
			opts = pools.CreateOpts{
				Name:        poolName,
				Protocol:    "HTTP",
				LBMethod:    pools.LBMethodRoundRobin,
				ListenerID:  listenerID,
				Persistence: nil,
			}
		} else {
			opts = pools.CreateOpts{
				Name:           poolName,
				Protocol:       "HTTP",
				LBMethod:       pools.LBMethodRoundRobin,
				LoadbalancerID: lbID,
				Persistence:    nil,
			}
		}
		pool, err = pools.Create(os.Octavia, opts).Extract()
		if err != nil {
			return nil, fmt.Errorf("error creating pool: %v", err)
		}

		logger.Info("pool created")

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
			logger.WithFields(log.Fields{"nodeName": node.Name, "error": err}).Warn("failed to create LB pool member for node")
			continue
		}

		nodeName := node.Name
		member := pools.BatchUpdateMemberOpts{
			Name:         &nodeName,
			Address:      addr,
			ProtocolPort: *nodePort,
		}
		members = append(members, member)
	}
	// only allow >= 1 members or it will lead to openstack octavia issue
	if len(members) == 0 {
		return nil, fmt.Errorf("error because no members in pool: %s", pool.ID)
	}

	if err := pools.BatchUpdateMembers(os.Octavia, pool.ID, members).ExtractErr(); err != nil {
		return nil, fmt.Errorf("error batch updating members for pool %s: %v", pool.ID, err)
	}
	_, err = os.waitLoadbalancerActiveProvisioningStatus(lbID)
	if err != nil {
		return nil, fmt.Errorf("error waiting for loadbalancer %s to be active: %v", lbID, err)
	}

	logger.Info("pool members updated")

	return &pool.ID, nil
}

// UpdateLoadbalancerMembers update members for all the pools in the specified load balancer.
func (os *OpenStack) UpdateLoadbalancerMembers(lbID string, nodes []*apiv1.Node) error {
	lbPools, err := openstackutil.GetPools(os.Octavia, lbID)
	if err != nil {
		return err
	}

	for _, pool := range lbPools {
		log.WithFields(log.Fields{"poolID": pool.ID}).Debug("Starting to update pool members")

		members, err := openstackutil.GetMembersbyPool(os.Octavia, pool.ID)
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
