/*
Copyright 2024 The Kubernetes Authors.

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
	"regexp"
	"strings"

	"k8s.io/cloud-provider-openstack/pkg/util"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/monitors"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

// lbHasOldClusterName checks if the OCCM LB prefix is present and if so, validates the cluster-name
// component value. Returns true if the cluster-name component of the loadbalancer's name doesn't match
// clusterName.
func lbHasOldClusterName(loadbalancer *loadbalancers.LoadBalancer, clusterName string) bool {
	if !strings.HasPrefix(loadbalancer.Name, servicePrefix) {
		// This one was probably not created by OCCM, let's leave it as is.
		return false
	}
	existingClusterName := getClusterName("", loadbalancer.Name)

	return existingClusterName != clusterName
}

// decomposeLBName returns clusterName based on object name
func getClusterName(resourcePrefix, objectName string) string {
	// This is not 100% bulletproof when string was cut because full name would exceed 255 characters, but honestly
	// it's highly unlikely, because it would mean cluster name, namespace and name would together need to exceed 200
	// characters. As a precaution the _<name> part is treated as optional in the regexp, assuming the longest trim
	// that can happen will reach namespace, but never the clusterName. This fails if there's _ in clusterName, but
	// we can't do nothing about it.
	lbNameRegex := regexp.MustCompile(fmt.Sprintf("%s%s(.+?)_[^_]+(_[^_]+)?$", resourcePrefix, servicePrefix)) // this is static

	matches := lbNameRegex.FindAllStringSubmatch(objectName, -1)
	if matches == nil {
		return ""
	}
	return matches[0][1]
}

// replaceClusterName tries to cut servicePrefix, replaces clusterName and puts back the prefix if it was there
func replaceClusterName(oldClusterName, clusterName, objectName string) string {
	// Remove the prefix first
	var found bool
	objectName, found = strings.CutPrefix(objectName, servicePrefix)
	objectName = strings.Replace(objectName, oldClusterName, clusterName, 1)
	if found {
		// This should always happen because we check that in lbHasOldClusterName, but just for safety.
		objectName = servicePrefix + objectName
	}
	// We need to cut the name or tag to 255 characters, just as regular LB creation does.
	return util.CutString255(objectName)
}

// renameLoadBalancer renames all the children and then the LB itself to match new lbName.
// The purpose is handling a change of clusterName.
func renameLoadBalancer(ctx context.Context, client *gophercloud.ServiceClient, loadbalancer *loadbalancers.LoadBalancer, lbName, clusterName string) (*loadbalancers.LoadBalancer, error) {
	lbListeners, err := openstackutil.GetListenersByLoadBalancerID(ctx, client, loadbalancer.ID)
	if err != nil {
		return nil, err
	}
	for _, listener := range lbListeners {
		if !strings.HasPrefix(listener.Name, listenerPrefix) {
			// It doesn't seem to be ours, let's not touch it.
			continue
		}

		oldClusterName := getClusterName(fmt.Sprintf("%s[0-9]+_", listenerPrefix), listener.Name)

		if oldClusterName != clusterName {
			// First let's handle pool which we assume is a child of the listener. Only one pool per one listener.
			lbPool, err := openstackutil.GetPoolByListener(ctx, client, loadbalancer.ID, listener.ID)
			if err != nil {
				return nil, err
			}
			oldClusterName = getClusterName(fmt.Sprintf("%s[0-9]+_", poolPrefix), lbPool.Name)
			if oldClusterName != clusterName {
				if lbPool.MonitorID != "" {
					// If monitor exists, let's handle it first, as we treat it as child of the pool.
					monitor, err := openstackutil.GetHealthMonitor(ctx, client, lbPool.MonitorID)
					if err != nil {
						return nil, err
					}
					oldClusterName := getClusterName(fmt.Sprintf("%s[0-9]+_", monitorPrefix), monitor.Name)
					if oldClusterName != clusterName {
						monitor.Name = replaceClusterName(oldClusterName, clusterName, monitor.Name)
						err = openstackutil.UpdateHealthMonitor(ctx, client, monitor.ID, monitors.UpdateOpts{Name: &monitor.Name}, loadbalancer.ID)
						if err != nil {
							return nil, err
						}
					}
				}

				// Monitor is handled, let's rename the pool.
				lbPool.Name = replaceClusterName(oldClusterName, clusterName, lbPool.Name)
				err = openstackutil.UpdatePool(ctx, client, loadbalancer.ID, lbPool.ID, pools.UpdateOpts{Name: &lbPool.Name})
				if err != nil {
					return nil, err
				}
			}

			for i, tag := range listener.Tags {
				// There might be tags for shared listeners, that's why we analyze each tag on its own.
				oldClusterNameTag := getClusterName("", tag)
				if oldClusterNameTag != "" && oldClusterNameTag != clusterName {
					listener.Tags[i] = replaceClusterName(oldClusterNameTag, clusterName, tag)
				}
			}
			listener.Name = replaceClusterName(oldClusterName, clusterName, listener.Name)
			err = openstackutil.UpdateListener(ctx, client, loadbalancer.ID, listener.ID, listeners.UpdateOpts{Name: &listener.Name, Tags: &listener.Tags})
			if err != nil {
				return nil, err
			}
		}
	}

	// At last we rename the LB. This is to make sure we only stop retrying to rename the LB once all of the children
	// are handled.
	for i, tag := range loadbalancer.Tags {
		// There might be tags for shared lbs, that's why we analyze each tag on its own.
		oldClusterNameTag := getClusterName("", tag)
		if oldClusterNameTag != "" && oldClusterNameTag != clusterName {
			loadbalancer.Tags[i] = replaceClusterName(oldClusterNameTag, clusterName, tag)
		}
	}
	return openstackutil.UpdateLoadBalancer(ctx, client, loadbalancer.ID, loadbalancers.UpdateOpts{Name: &lbName, Tags: &loadbalancer.Tags})
}
