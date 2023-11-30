package openstack

import (
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
	apiv1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"

	cpoutil "k8s.io/cloud-provider-openstack/pkg/util"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
)

func memberExists(members []pools.Member, addr string, port int) bool {
	for _, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			return true
		}
	}

	return false
}

func popMember(members []pools.Member, addr string, port int) []pools.Member {
	for i, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			members[i] = members[len(members)-1]
			members = members[:len(members)-1]
		}
	}

	return members
}

func getNodeAddressForLB(node *apiv1.Node) (string, error) {
	addrs := node.Status.Addresses
	if len(addrs) == 0 {
		return "", fmt.Errorf("no address found for host")
	}

	for _, addr := range addrs {
		if addr.Type == apiv1.NodeInternalIP {
			return addr.Address, nil
		}
	}

	return addrs[0].Address, nil
}

func SeriallyReconcilePoolMembers(client *gophercloud.ServiceClient, pool *pools.Pool, nodePort int, lbID string, nodes []*apiv1.Node) error {

	members, err := GetMembersbyPool(client, pool.ID)
	if err != nil && !cpoerrors.IsNotFound(err) {
		return fmt.Errorf("error getting pool members %s: %v", pool.ID, err)
	}

	for _, node := range nodes {
		addr, err := getNodeAddressForLB(node)
		if err != nil {
			if err == cpoerrors.ErrNotFound {
				// Node failure, do not create member
				klog.Warningf("Failed to create LB pool member for node %s: %v", node.Name, err)
				continue
			} else {
				return fmt.Errorf("error getting address for node %s: %v", node.Name, err)
			}
		}
		if !memberExists(members, addr, nodePort) {
			klog.V(2).Infof("Creating member for pool %s", pool.ID)
			_, err := pools.CreateMember(client, pool.ID, pools.CreateMemberOpts{
				Name:         cpoutil.CutString255(fmt.Sprintf("member_%s_%s_%d", node.Name, addr, nodePort)),
				ProtocolPort: nodePort,
				Address:      addr,
			}).Extract()
			if err != nil {
				return fmt.Errorf("error creating LB pool member for node: %s, %v", node.Name, err)
			}
			if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
				return err
			}
		} else {
			// After all members have been processed, remaining members are deleted as obsolete.
			members = popMember(members, addr, nodePort)
		}
		klog.V(2).Infof("Ensured pool %s has member for %s at %s", pool.ID, node.Name, addr)
	}
	for _, member := range members {
		klog.V(2).Infof("Deleting obsolete member %s for pool %s address %s", member.ID, pool.ID, member.Address)
		err := pools.DeleteMember(client, pool.ID, member.ID).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return fmt.Errorf("error deleting obsolete member %s for pool %s address %s: %v", member.ID, pool.ID, member.Address, err)
		}
		if _, err := WaitActiveAndGetLoadBalancer(client, lbID); err != nil {
			return err
		}
	}
	return nil
}
