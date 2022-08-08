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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v2/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/startstop"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/volumeattach"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/containerinfra/v1/clusters"
	"github.com/gophercloud/gophercloud/openstack/containerinfra/v1/nodegroups"
	"github.com/gophercloud/gophercloud/openstack/orchestration/v1/stackresources"
	"github.com/gophercloud/gophercloud/openstack/orchestration/v1/stacks"
	"github.com/gophercloud/gophercloud/pagination"
	uuid "github.com/pborman/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"

	"k8s.io/cloud-provider-openstack/pkg/autohealing/config"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/healthcheck"
)

const (
	ProviderName                = "openstack"
	ClusterAutoHealingLabel     = "auto_healing_enabled"
	stackStatusUpdateFailed     = "UPDATE_FAILED"
	stackStatusUpdateInProgress = "UPDATE_IN_PROGRESS"
)

var statusesPreventingRepair = sets.NewString(
	stackStatusUpdateInProgress,
	stackStatusUpdateFailed,
)

// Cache the unhealthy nodes, if it's the first time we found this
// unhealthy node, then we just reboot it and save it in this list. If it's not
// the first time we found this unhealthy node, we will rebuild it.
var unHealthyNodes = make(map[string]healthcheck.NodeInfo)

// revive:disable:exported
// Deprecated: use CloudProvider instead
type OpenStackCloudProvider = CloudProvider

// revive:enable:exported

// OpenStack is an implementation of cloud provider Interface for OpenStack.
type CloudProvider struct {
	KubeClient           kubernetes.Interface
	Nova                 *gophercloud.ServiceClient
	Heat                 *gophercloud.ServiceClient
	Magnum               *gophercloud.ServiceClient
	Cinder               *gophercloud.ServiceClient
	Config               config.Config
	ResourceStackMapping map[string]ResourceStackRelationship
}

type ResourceStackRelationship struct {
	ResourceID   string
	ResourceName string
	StackID      string
	StackName    string
}

func (provider CloudProvider) GetName() string {
	return ProviderName
}

// getStackName finds the name of a stack matching a given ID.
func (provider *CloudProvider) getStackName(stackID string) (string, error) {
	stack, err := stacks.Find(provider.Heat, stackID).Extract()
	if err != nil {
		return "", err
	}

	return stack.Name, nil
}

// getAllStackResourceMapping returns all mapping relationships including
// masters and minions(workers). The key in the map is the server/instance ID
// in Nova and the value is the resource ID and name of the server, and the
// parent stack ID and name.
func (provider *CloudProvider) getAllStackResourceMapping(stackName, stackID string) (m map[string]ResourceStackRelationship, err error) {
	if provider.ResourceStackMapping != nil {
		return provider.ResourceStackMapping, nil
	}

	mapping := make(map[string]ResourceStackRelationship)

	serverPages, err := stackresources.List(provider.Heat, stackName, stackID, stackresources.ListOpts{Depth: 2}).AllPages()
	if err != nil {
		return m, err
	}
	serverResources, err := stackresources.ExtractResources(serverPages)
	if err != nil {
		return m, err
	}

	for _, sr := range serverResources {
		if sr.Type != "OS::Nova::Server" {
			continue
		}

		for _, link := range sr.Links {
			if link.Rel == "self" {
				continue
			}

			paths := strings.Split(link.Href, "/")
			if len(paths) > 2 {
				m := ResourceStackRelationship{
					ResourceID:   sr.PhysicalID,
					ResourceName: sr.Name,
					StackID:      paths[len(paths)-1],
					StackName:    paths[len(paths)-2],
				}

				log.V(4).Infof("resource ID: %s, resource name: %s, parent stack ID: %s, parent stack name: %s", sr.PhysicalID, sr.Name, paths[len(paths)-1], paths[len(paths)-2])

				mapping[sr.PhysicalID] = m
			}
		}
	}
	provider.ResourceStackMapping = mapping

	return provider.ResourceStackMapping, nil
}

func (provider CloudProvider) waitForServerPoweredOff(serverID string, timeout time.Duration) error {
	err := startstop.Stop(provider.Nova, serverID).ExtractErr()
	if err != nil {
		return err
	}

	err = wait.Poll(3*time.Second, timeout,
		func() (bool, error) {
			server, err := servers.Get(provider.Nova, serverID).Extract()
			if err != nil {
				return false, err
			}

			if server.Status == "SHUTOFF" {
				return true, nil
			}

			return false, nil
		})

	return err
}

// waitForClusterStatus checks periodically to see if the cluster has entered a given status.
// Returns when the status is observed or the timeout is reached.
func (provider CloudProvider) waitForClusterComplete(clusterID string, timeout time.Duration) error {
	log.V(2).Infof("Waiting for cluster %s in complete status", clusterID)

	err := wait.Poll(3*time.Second, timeout,
		func() (bool, error) {
			cluster, err := clusters.Get(provider.Magnum, clusterID).Extract()
			if err != nil {
				return false, err
			}
			log.V(5).Infof("Cluster %s in status %s", clusterID, cluster.Status)
			if strings.HasSuffix(cluster.Status, "_COMPLETE") {
				return true, nil
			}
			return false, nil
		})
	return err
}

// waitForServerDetachVolumes will detach all the attached volumes from the given
// server with the timeout. And if there is a root volume of the server, the root
// volume ID will be returned.
func (provider CloudProvider) waitForServerDetachVolumes(serverID string, timeout time.Duration) (string, error) {
	rootVolumeID := ""
	err := volumeattach.List(provider.Nova, serverID).EachPage(func(page pagination.Page) (bool, error) {
		attachments, err := volumeattach.ExtractVolumeAttachments(page)
		if err != nil {
			return false, err
		}
		for _, attachment := range attachments {
			volume, err := volumes.Get(provider.Cinder, attachment.VolumeID).Extract()
			if err != nil {
				return false, fmt.Errorf("failed to get volume %s, error: %s", attachment.VolumeID, err)
			}

			bootable, err := strconv.ParseBool(volume.Bootable)
			if err != nil {
				log.Warningf("Unexpected value for bootable volume %s in volume %s, error %s", volume.Bootable, volume, err)
			}

			log.Infof("volume %s is bootable %t", attachment.VolumeID, bootable)

			if !bootable {
				log.Infof("detaching volume %s for instance %s", attachment.VolumeID, serverID)
				err := volumeattach.Delete(provider.Nova, serverID, attachment.ID).ExtractErr()
				if err != nil {
					return false, fmt.Errorf("failed to detach volume %s from instance %s, error: %s", attachment.VolumeID, serverID, err)
				}
			} else {
				rootVolumeID = attachment.VolumeID
				log.Infof("the root volume for server %s is %s", serverID, attachment.VolumeID)
			}
		}
		return true, err
	})
	if err != nil {
		return rootVolumeID, err
	}
	err = wait.Poll(3*time.Second, timeout,
		func() (bool, error) {
			server, err := servers.Get(provider.Nova, serverID).Extract()
			if err != nil {
				return false, err
			}

			if len(server.AttachedVolumes) == 0 && rootVolumeID == "" {
				return true, nil
			} else if len(server.AttachedVolumes) == 1 && rootVolumeID != "" {
				// Root volume is left
				return true, nil
			}

			return false, nil
		})

	return rootVolumeID, err
}

// FirstTimeRepair Handle the first time repair for a node
//  1. If the node is the first time in error, reboot and uncordon it
//  2. If the node is not the first time in error, check if the last reboot time is in provider.Config.RebuildDelayAfterReboot
//     That said, if the node has been found in broken status before but has been long time since then, the processed variable
//     will be kept as False, which means the node need to be rebuilt to fix it, otherwise it means the has been processed.
//
// The bool type return value means that if the node has been processed from a first time repair PoV
func (provider CloudProvider) firstTimeRepair(n healthcheck.NodeInfo, serverID string, firstTimeRebootNodes map[string]healthcheck.NodeInfo) (bool, error) {
	var firstTimeUnhealthy = true
	for id := range unHealthyNodes {
		log.V(5).Infof("comparing server ID %s with known broken ID %s", serverID, id)
		if id == serverID {
			log.Infof("it is NOT the first time that node %s is being repaired", serverID)
			firstTimeUnhealthy = false
			break
		}
	}

	var processed = false
	if firstTimeUnhealthy {
		log.Infof("rebooting node %s to repair it", serverID)

		if res := servers.Reboot(provider.Nova, serverID, servers.RebootOpts{Type: servers.SoftReboot}); res.Err != nil {
			// Usually it means the node is being rebooted
			log.Warningf("failed to reboot node %s, error: %v", serverID, res.Err)
			if strings.Contains(res.Err.Error(), "reboot_started") {
				// The node has been restarted
				firstTimeRebootNodes[serverID] = n
			}
		} else {
			// Uncordon the node
			if n.IsWorker {
				nodeName := n.KubeNode.Name
				newNode := n.KubeNode.DeepCopy()
				newNode.Spec.Unschedulable = false
				if _, err := provider.KubeClient.CoreV1().Nodes().Update(context.TODO(), newNode, metav1.UpdateOptions{}); err != nil {
					log.Errorf("Failed to cordon node %s, error: %v", nodeName, err)
				} else {
					log.Infof("Node %s is cordoned", nodeName)
				}
			}

			n.RebootAt = time.Now()
			firstTimeRebootNodes[serverID] = n
			unHealthyNodes[serverID] = n
			log.Infof("Node %s has been rebooted at %s", serverID, n.RebootAt)
		}
		processed = true
	} else {
		// If the node was rebooted within mins (defined by RepairDelayAfterReboot), ignore it
		log.Infof("Node %s is found in unhealthy again which was rebooted at %s", serverID, unHealthyNodes[serverID].RebootAt)
		if unHealthyNodes[serverID].RebootAt.After(time.Now().Add(-1 * provider.Config.RebuildDelayAfterReboot)) {
			log.Infof("Node %s is found in unhealthy again, but we're going to defer the repair because it maybe in reboot process", serverID)
			firstTimeRebootNodes[serverID] = n
			processed = true
		}
	}

	return processed, nil
}

// Repair  For master nodes: detach etcd and docker volumes, find the root
//
//	        volume, then shutdown the VM, marks the both the VM and the root
//	        volume (heat resource) as "unhealthy" then trigger Heat stack update
//	        in order to rebuild the node. The information this function needs:
//	        - Nova VM ID
//	        - Root volume ID
//		       - Heat stack ID and resource ID.
//
// For worker nodes: Call Magnum resize API directly.
func (provider CloudProvider) Repair(nodes []healthcheck.NodeInfo) error {
	if len(nodes) == 0 {
		return nil
	}

	masters := []healthcheck.NodeInfo{}
	workers := []healthcheck.NodeInfo{}

	clusterName := provider.Config.ClusterName
	isWorkerNode := nodes[0].IsWorker
	log.Infof("the node type to be repaired is worker node: %t", isWorkerNode)
	if isWorkerNode {
		workers = nodes
	} else {
		masters = nodes
	}

	firstTimeRebootNodes := make(map[string]healthcheck.NodeInfo)

	err := provider.UpdateHealthStatus(masters, workers)
	if err != nil {
		return fmt.Errorf("failed to update the helath status of cluster %s, error: %v", clusterName, err)
	}

	cluster, err := clusters.Get(provider.Magnum, clusterName).Extract()
	if err != nil {
		return fmt.Errorf("failed to get the cluster %s, error: %v", clusterName, err)
	}

	if isWorkerNode {
		for _, n := range nodes {
			nodesToReplace := sets.NewString()
			machineID := uuid.Parse(n.KubeNode.Status.NodeInfo.MachineID)
			if machineID == nil {
				log.Warningf("Failed to get the correct server ID for server %s", n.KubeNode.Name)
				continue
			}
			serverID := machineID.String()

			if processed, err := provider.firstTimeRepair(n, serverID, firstTimeRebootNodes); err != nil {
				log.Warningf("Failed to process if the node %s is in first time repair , error: %v", serverID, err)
			} else if processed {
				log.Infof("Node %s has been processed", serverID)
				continue
			}

			if _, err := provider.waitForServerDetachVolumes(serverID, 30*time.Second); err != nil {
				log.Warningf("Failed to detach volumes from server %s, error: %v", serverID, err)
			}

			if err := provider.waitForServerPoweredOff(serverID, 30*time.Second); err != nil {
				log.Warningf("Failed to shutdown the server %s, error: %v", serverID, err)
			}

			nodesToReplace.Insert(serverID)
			ng, err := provider.getNodeGroup(clusterName, n)
			ngName := "default-worker"
			ngNodeCount := &cluster.NodeCount
			if err == nil {
				ngName = ng.Name
				ngNodeCount = &ng.NodeCount
			}

			opts := clusters.ResizeOpts{
				NodeGroup:     ngName,
				NodeCount:     ngNodeCount,
				NodesToRemove: nodesToReplace.List(),
			}

			clusters.Resize(provider.Magnum, clusterName, opts)
			// Wait 10 seconds to make sure Magnum has already got the request
			// to avoid sending all of the resize API calls at the same time.
			time.Sleep(10 * time.Second)
			// TODO: Ignore the result value until https://github.com/gophercloud/gophercloud/pull/1649 is merged.
			//if ret.Err != nil {
			//	return fmt.Errorf("failed to resize cluster %s, error: %v", clusterName, ret.Err)
			//}

			delete(unHealthyNodes, serverID)
			log.Infof("Cluster %s resized", clusterName)
		}
	} else {
		clusterStackName, err := provider.getStackName(cluster.StackID)
		if err != nil {
			return fmt.Errorf("failed to get the Heat stack for cluster %s, error: %v", clusterName, err)
		}

		// In order to rebuild the nodes by Heat stack update, we need to know the parent stack ID of the resources and
		// mark them unhealthy first.
		allMapping, err := provider.getAllStackResourceMapping(clusterStackName, cluster.StackID)
		if err != nil {
			return fmt.Errorf("failed to get the resource stack mapping for cluster %s, error: %v", clusterName, err)
		}

		opts := stackresources.MarkUnhealthyOpts{
			MarkUnhealthy:        true,
			ResourceStatusReason: "Mark resource unhealthy by autohealing service",
		}

		for _, n := range nodes {
			machineID := uuid.Parse(n.KubeNode.Status.NodeInfo.MachineID)
			if machineID == nil {
				log.Warningf("Failed to get the correct server ID for server %s", n.KubeNode.Name)
				continue
			}
			serverID := machineID.String()

			if processed, err := provider.firstTimeRepair(n, serverID, firstTimeRebootNodes); err != nil {
				log.Warningf("Failed to process if the node %s is in first time repair , error: %v", serverID, err)
			} else if processed {
				log.Infof("Node %s has been processed", serverID)
				continue
			}

			if rootVolumeID, err := provider.waitForServerDetachVolumes(serverID, 30*time.Second); err != nil {
				log.Warningf("Failed to detach volumes from server %s, error: %v", serverID, err)
			} else {
				// Mark root volume as unhealthy
				if rootVolumeID != "" {
					err = stackresources.MarkUnhealthy(provider.Heat, allMapping[serverID].StackName, allMapping[serverID].StackID, rootVolumeID, opts).ExtractErr()
					if err != nil {
						log.Errorf("failed to mark resource %s unhealthy, error: %v", rootVolumeID, err)
					}
				}
			}

			if err := provider.waitForServerPoweredOff(serverID, 180*time.Second); err != nil {
				log.Warningf("Failed to shutdown the server %s, error: %v", serverID, err)
				// If the server is failed to delete after 180s, then delete it to avoid the
				// stack update failure later.
				res := servers.ForceDelete(provider.Nova, serverID)
				if res.Err != nil {
					log.Warningf("Failed to delete the server %s, error: %v", serverID, err)
				}
			}

			log.Infof("Marking Nova VM %s(Heat resource %s) unhealthy for Heat stack %s", serverID, allMapping[serverID].ResourceID, cluster.StackID)

			// Mark VM as unhealthy
			err = stackresources.MarkUnhealthy(provider.Heat, allMapping[serverID].StackName, allMapping[serverID].StackID, allMapping[serverID].ResourceID, opts).ExtractErr()
			if err != nil {
				log.Errorf("failed to mark resource %s unhealthy, error: %v", serverID, err)
			}

			delete(unHealthyNodes, serverID)
		}

		if err := stacks.UpdatePatch(provider.Heat, clusterStackName, cluster.StackID, stacks.UpdateOpts{}).ExtractErr(); err != nil {
			return fmt.Errorf("failed to update Heat stack to rebuild resources, error: %v", err)
		}

		log.Infof("Started Heat stack update to rebuild resources, cluster: %s, stack: %s", clusterName, cluster.StackID)
	}

	// Remove the broken nodes from the cluster
	for _, n := range nodes {
		serverID := uuid.Parse(n.KubeNode.Status.NodeInfo.MachineID).String()
		if _, found := firstTimeRebootNodes[serverID]; found {
			log.Infof("Skip node delete for %s because it's repaired by reboot", serverID)
			continue
		}
		if err := provider.KubeClient.CoreV1().Nodes().Delete(context.TODO(), n.KubeNode.Name, metav1.DeleteOptions{}); err != nil {
			log.Errorf("Failed to remove the node %s from cluster, error: %v", n.KubeNode.Name, err)
		}
	}

	return nil
}

func (provider CloudProvider) getNodeGroup(clusterName string, node healthcheck.NodeInfo) (nodegroups.NodeGroup, error) {
	var ng nodegroups.NodeGroup

	ngPages, err := nodegroups.List(provider.Magnum, clusterName, nodegroups.ListOpts{}).AllPages()
	if err == nil {
		ngs, err := nodegroups.ExtractNodeGroups(ngPages)
		if err != nil {
			log.Warningf("Failed to get node group for cluster %s, error: %v", clusterName, err)
			return ng, err
		}
		for _, ng := range ngs {
			ngInfo, err := nodegroups.Get(provider.Magnum, clusterName, ng.UUID).Extract()
			if err != nil {
				log.Warningf("Failed to get node group for cluster %s, error: %v", clusterName, err)
				return ng, err
			}
			log.Infof("Got node addresses %v, node group's node addresses %v ", node.KubeNode.Status.Addresses, ngInfo.NodeAddresses)
			for _, na := range node.KubeNode.Status.Addresses {
				for _, nodeAddress := range ngInfo.NodeAddresses {
					if na.Address == nodeAddress {
						log.Infof("Got matched node group %s", ngInfo.Name)
						return *ngInfo, nil
					}
				}
			}
		}
	}

	return ng, fmt.Errorf("failed to find node group")
}

// UpdateHealthStatus can update the cluster health status to reflect the
// real-time health status of the k8s cluster.
func (provider CloudProvider) UpdateHealthStatus(masters []healthcheck.NodeInfo, workers []healthcheck.NodeInfo) error {
	log.Infof("start to update cluster health status.")
	clusterName := provider.Config.ClusterName

	healthStatus := "UNHEALTHY"
	healthStatusReasonMap := make(map[string]string)
	healthStatusReasonMap["updated_at"] = time.Now().String()

	if len(masters) == 0 && len(workers) == 0 {
		// No unhealthy node passed in means the cluster is healthy
		healthStatus = "HEALTHY"
		healthStatusReasonMap["api"] = "ok"
		healthStatusReasonMap["nodes"] = "ok"
	} else {
		if len(workers) > 0 {
			for _, n := range workers {
				// TODO: Need to figure out a way to reflect the detailed error information
				healthStatusReasonMap[n.KubeNode.Name+"."+n.FailedCheck] = "error"
			}
		} else {
			// TODO: Need to figure out a way to reflect detailed error information
			healthStatusReasonMap["api"] = "error"
		}
	}

	jsonDumps, err := json.Marshal(healthStatusReasonMap)
	if err != nil {
		return fmt.Errorf("failed to build health status reason for cluster %s, error: %v", clusterName, err)
	}
	healthStatusReason := strings.Replace(string(jsonDumps), "\"", "'", -1)

	updateOpts := []clusters.UpdateOptsBuilder{
		clusters.UpdateOpts{
			Op:    clusters.ReplaceOp,
			Path:  "/health_status",
			Value: healthStatus,
		},
		clusters.UpdateOpts{
			Op:    clusters.ReplaceOp,
			Path:  "/health_status_reason",
			Value: healthStatusReason,
		},
	}

	log.Infof("updating cluster health status as %s for reason %s.", healthStatus, healthStatusReason)
	res := clusters.Update(provider.Magnum, clusterName, updateOpts)

	if res.Err != nil {
		return fmt.Errorf("failed to update the health status of cluster %s error: %v", clusterName, res.Err)
	}

	if err := provider.waitForClusterComplete(clusterName, 30*time.Second); err != nil {
		log.Warningf("failed to wait the cluster %s in complete status, error: %v", clusterName, err)
	}

	return nil
}

// Enabled decides if the repair should be triggered.
// There are  two conditions that we disable the repair:
// - The cluster admin disables the auto healing via OpenStack API.
// - The Magnum cluster is not in stable status.
func (provider CloudProvider) Enabled() bool {
	clusterName := provider.Config.ClusterName

	cluster, err := clusters.Get(provider.Magnum, clusterName).Extract()
	if err != nil {
		log.Warningf("failed to get the cluster %s, error: %v", clusterName, err)
		return false
	}

	if !strings.HasSuffix(cluster.Status, "_COMPLETE") {
		return false
	}

	if _, isPresent := cluster.Labels[ClusterAutoHealingLabel]; !isPresent {
		log.Infof("Autohealing is disalbed for cluster %s", clusterName)
		return false
	}
	autoHealingEnabled, err := strconv.ParseBool(cluster.Labels[ClusterAutoHealingLabel])
	if err != nil {
		log.Warningf("Unexpected value for %s label in cluster %s", ClusterAutoHealingLabel, clusterName)
		return false
	}
	if !autoHealingEnabled {
		log.Infof("Autohealing is disalbed for cluster %s", clusterName)
		return false
	}

	clusterStackName, err := provider.getStackName(cluster.StackID)
	if err != nil {
		log.Warningf("Failed to get the Heat stack ID for cluster %s, error: %v", clusterName, err)
		return false
	}
	stack, err := stacks.Get(provider.Heat, clusterStackName, cluster.StackID).Extract()
	if err != nil {
		log.Warningf("Failed to get Heat stack %s for cluster %s, error: %v", cluster.StackID, clusterName, err)
		return false
	}

	if statusesPreventingRepair.Has(stack.Status) {
		log.Infof("Current cluster stack is in status %s, skip the repair", stack.Status)
		return false
	}

	return true
}
