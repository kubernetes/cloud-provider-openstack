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
	"context"
	"fmt"
	"strconv"
	"strings"

	neutrontags "github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/cloud-provider-openstack/pkg/ingress/utils"
)

func (os *OpenStack) getFloatingIPs(ctx context.Context, listOpts floatingips.ListOpts) ([]floatingips.FloatingIP, error) {
	allPages, err := floatingips.List(os.neutron, listOpts).AllPages(ctx)
	if err != nil {
		return []floatingips.FloatingIP{}, err
	}
	allFIPs, err := floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return []floatingips.FloatingIP{}, err
	}

	return allFIPs, nil
}

func (os *OpenStack) createFloatingIP(ctx context.Context, portID string, floatingNetworkID string, description string) (*floatingips.FloatingIP, error) {
	floatIPOpts := floatingips.CreateOpts{
		PortID:            portID,
		FloatingNetworkID: floatingNetworkID,
		Description:       description,
	}
	return floatingips.Create(ctx, os.neutron, floatIPOpts).Extract()
}

// associateFloatingIP associate an unused floating IP to a given Port
func (os *OpenStack) associateFloatingIP(ctx context.Context, fip *floatingips.FloatingIP, portID string, description string) (*floatingips.FloatingIP, error) {
	updateOpts := floatingips.UpdateOpts{
		PortID:      &portID,
		Description: &description,
	}
	return floatingips.Update(ctx, os.neutron, fip.ID, updateOpts).Extract()
}

// disassociateFloatingIP disassociate a floating IP from a port
func (os *OpenStack) disassociateFloatingIP(ctx context.Context, fip *floatingips.FloatingIP, description string) (*floatingips.FloatingIP, error) {
	updateDisassociateOpts := floatingips.UpdateOpts{
		PortID:      new(string),
		Description: &description,
	}
	return floatingips.Update(ctx, os.neutron, fip.ID, updateDisassociateOpts).Extract()
}

// GetSubnet get a subnet by the given ID.
func (os *OpenStack) GetSubnet(ctx context.Context, subnetID string) (*subnets.Subnet, error) {
	subnet, err := subnets.Get(ctx, os.neutron, subnetID).Extract()
	if err != nil {
		return nil, err
	}
	return subnet, nil
}

// getPorts gets all the filtered ports.
func (os *OpenStack) getPorts(ctx context.Context, listOpts ports.ListOpts) ([]ports.Port, error) {
	allPages, err := ports.List(os.neutron, listOpts).AllPages(ctx)
	if err != nil {
		return []ports.Port{}, err
	}
	allPorts, err := ports.ExtractPorts(allPages)
	if err != nil {
		return []ports.Port{}, err
	}

	return allPorts, nil
}

// EnsureFloatingIP makes sure a floating IP is allocated for the port
func (os *OpenStack) EnsureFloatingIP(ctx context.Context, needDelete bool, portID string, existingfloatingIP string, floatingIPNetwork string, description string) (string, error) {
	listOpts := floatingips.ListOpts{PortID: portID}
	fips, err := os.getFloatingIPs(ctx, listOpts)
	if err != nil {
		return "", fmt.Errorf("unable to get floating ips: %w", err)
	}

	// If needed, delete the floating IPs and return.
	if needDelete {
		for _, fip := range fips {
			if err := floatingips.Delete(ctx, os.neutron, fip.ID).ExtractErr(); err != nil {
				return "", err
			}
		}

		return "", nil
	}

	if len(fips) > 1 {
		return "", fmt.Errorf("more than one floating IPs for port %s found", portID)
	}

	var fip *floatingips.FloatingIP

	if existingfloatingIP == "" {
		if len(fips) == 1 {
			fip = &fips[0]
		} else {
			fip, err = os.createFloatingIP(ctx, portID, floatingIPNetwork, description)
			if err != nil {
				return "", err
			}
		}
	} else {
		// if user provide FIP
		// check if provided fip is available
		opts := floatingips.ListOpts{
			FloatingIP:        existingfloatingIP,
			FloatingNetworkID: floatingIPNetwork,
		}
		osFips, err := os.getFloatingIPs(ctx, opts)
		if err != nil {
			return "", err
		}
		if len(osFips) != 1 {
			return "", fmt.Errorf("error when searching floating IPs %s, %d floating IPs found", existingfloatingIP, len(osFips))
		}
		// check if fip is already attached to the correct port
		if osFips[0].PortID == portID {
			return osFips[0].FloatingIP, nil
		}
		// check if fip is already used by other port
		// We might consider if here we shouldn't detach that FIP instead of returning error
		if osFips[0].PortID != "" {
			return "", fmt.Errorf("floating IP %s already used by port %s", osFips[0].FloatingIP, osFips[0].PortID)
		}

		// if port don't have fip
		if len(fips) == 0 {
			fip, err = os.associateFloatingIP(ctx, &osFips[0], portID, description)
			if err != nil {
				return "", err
			}
		} else if osFips[0].FloatingIP != fips[0].FloatingIP {
			// disassociate old fip : if update fip without disassociate
			// Openstack retrun http 409 error
			// "Cannot associate floating IP with port using fixed
			//  IP, as that fixed IP already has a floating IP on
			//  external network"
			_, err = os.disassociateFloatingIP(ctx, &fips[0], "")
			if err != nil {
				return "", err
			}
			// associate new fip
			fip, err = os.associateFloatingIP(ctx, &osFips[0], portID, description)
			if err != nil {
				return "", err
			}
		} else {
			fip = &fips[0]
		}
	}

	return fip.FloatingIP, nil
}

// GetSecurityGroups gets all the filtered security groups.
func (os *OpenStack) GetSecurityGroups(ctx context.Context, listOpts groups.ListOpts) ([]groups.SecGroup, error) {
	allPages, err := groups.List(os.neutron, listOpts).AllPages(ctx)
	if err != nil {
		return []groups.SecGroup{}, err
	}
	allSGs, err := groups.ExtractGroups(allPages)
	if err != nil {
		return []groups.SecGroup{}, err
	}

	return allSGs, nil
}

// EnsureSecurityGroup make sure the security group with given tags exists or not according to need_delete param.
// Make sure the EnsurePortSecurityGroup function is called before EnsureSecurityGroup if you want to delete the security group.
func (os *OpenStack) EnsureSecurityGroup(ctx context.Context, needDelete bool, name string, description string, tags []string) (string, error) {
	tagsString := strings.Join(tags, ",")
	listOpts := groups.ListOpts{Tags: tagsString}
	allGroups, err := os.GetSecurityGroups(ctx, listOpts)
	if err != nil {
		return "", err
	}

	// If needed, delete the security groups and return.
	if needDelete {
		for _, group := range allGroups {
			if err := groups.Delete(ctx, os.neutron, group.ID).ExtractErr(); err != nil {
				return "", err
			}
		}
		return "", nil
	}

	if len(allGroups) > 1 {
		return "", fmt.Errorf("more than one security groups found")
	}

	// Create security group and add tags.
	var group *groups.SecGroup
	if len(allGroups) == 0 {
		createOpts := groups.CreateOpts{
			Name:        name,
			Description: description,
		}
		group, err = groups.Create(ctx, os.neutron, createOpts).Extract()
		if err != nil {
			return "", err
		}

		// Do not use tags replace API until https://bugs.launchpad.net/neutron/+bug/1817238 is resolved.
		//tagReplaceAllOpts := neutrontags.ReplaceAllOpts{Tags: tags}
		//if _, err := neutrontags.ReplaceAll(os.neutron, "security_groups", group.ID, tagReplaceAllOpts).Extract(); err != nil {
		//	return "", fmt.Errorf("failed to add tags %s to security group %s: %v", tagsString, group.ID, err)
		//}

		for _, t := range tags {
			if err := neutrontags.Add(ctx, os.neutron, "security_groups", group.ID, t).ExtractErr(); err != nil {
				return "", fmt.Errorf("failed to add tag %s to security group %s: %v", t, group.ID, err)
			}
		}
	} else {
		group = &allGroups[0]
	}

	return group.ID, nil
}

// EnsureSecurityGroupRules ensures the only dstPorts are allowed in the given security group.
func (os *OpenStack) EnsureSecurityGroupRules(ctx context.Context, sgID string, sourceIP string, dstPorts []int) error {
	listOpts := rules.ListOpts{
		Protocol:       "tcp",
		SecGroupID:     sgID,
		RemoteIPPrefix: sourceIP,
	}
	allPages, err := rules.List(os.neutron, listOpts).AllPages(ctx)
	if err != nil {
		return err
	}
	allRules, err := rules.ExtractRules(allPages)
	if err != nil {
		return err
	}

	if len(dstPorts) == 0 {
		// Delete all the rules and return.

		for _, rule := range allRules {
			if err := rules.Delete(ctx, os.neutron, rule.ID).ExtractErr(); err != nil {
				return err
			}
		}

		log.WithFields(log.Fields{"sgID": sgID}).Debug("all the security group rules deleted")
		return nil
	}

	dstPortsSet := sets.NewString()
	for _, p := range dstPorts {
		dstPortsSet.Insert(strconv.Itoa(p))
	}

	// Because the security group is supposed to be managed by octavia-ingress-controller, we assume the `port_range_min`
	// equals to `port_range_max`.
	for _, rule := range allRules {
		if !dstPortsSet.Has(strconv.Itoa(rule.PortRangeMin)) {
			// Delete the rule
			if err := rules.Delete(ctx, os.neutron, rule.ID).ExtractErr(); err != nil {
				return err
			}
		} else {
			dstPortsSet.Delete(strconv.Itoa(rule.PortRangeMin))
		}
	}

	// Now, the ports left in dstPortsSet are all needed for creating rules.
	newPorts := dstPortsSet.List()
	for _, p := range newPorts {
		newPort, err := strconv.Atoi(p)
		if err != nil {
			return err
		}
		createOpts := rules.CreateOpts{
			Direction:      "ingress",
			PortRangeMin:   newPort,
			PortRangeMax:   newPort,
			EtherType:      rules.EtherType4,
			Protocol:       "tcp",
			RemoteIPPrefix: sourceIP,
			SecGroupID:     sgID,
		}
		if _, err := rules.Create(ctx, os.neutron, createOpts).Extract(); err != nil {
			return err
		}
	}

	return nil
}

// EnsurePortSecurityGroup ensures the security group is attached to all the node ports or detached from all the ports
// according to needDelete param.
func (os *OpenStack) EnsurePortSecurityGroup(ctx context.Context, needDelete bool, sgID string, nodes []*v1.Node) error {
	for _, node := range nodes {
		instanceID, err := utils.GetNodeID(node)
		if err != nil {
			return err
		}
		listOpts := ports.ListOpts{DeviceID: instanceID}
		allPorts, err := os.getPorts(ctx, listOpts)
		if err != nil {
			return err
		}

		for _, port := range allPorts {
			sgSet := utils.Convert2Set(port.SecurityGroups)

			if sgSet.Has(sgID) && needDelete {
				// Remove sg from the port
				sgSet.Delete(sgID)
				newSGs := sets.List(sgSet)
				updateOpts := ports.UpdateOpts{SecurityGroups: &newSGs}
				if _, err := ports.Update(ctx, os.neutron, port.ID, updateOpts).Extract(); err != nil {
					return err
				}

				log.WithFields(log.Fields{"sgID": sgID, "portID": port.ID}).Debug("security group detached from the port")
			}

			if !sgSet.Has(sgID) && !needDelete {
				// Add sg to the port
				sgSet.Insert(sgID)
				newSGs := sets.List(sgSet)
				updateOpts := ports.UpdateOpts{SecurityGroups: &newSGs}
				if _, err := ports.Update(ctx, os.neutron, port.ID, updateOpts).Extract(); err != nil {
					return err
				}

				log.WithFields(log.Fields{"sgID": sgID, "portID": port.ID}).Debug("security group attached to the port")
			}
		}
	}

	return nil
}
