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
	"fmt"
	"strconv"
	"strings"

	neutrontags "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/cloud-provider-openstack/pkg/ingress/utils"
)

func (os *OpenStack) getFloatingIPs(listOpts floatingips.ListOpts) ([]floatingips.FloatingIP, error) {
	allPages, err := floatingips.List(os.neutron, listOpts).AllPages()
	if err != nil {
		return []floatingips.FloatingIP{}, err
	}
	allFIPs, err := floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return []floatingips.FloatingIP{}, err
	}

	return allFIPs, nil
}

// GetSubnet get a subnet by the given ID.
func (os *OpenStack) GetSubnet(subnetID string) (*subnets.Subnet, error) {
	subnet, err := subnets.Get(os.neutron, subnetID).Extract()
	if err != nil {
		return nil, err
	}
	return subnet, nil
}

// getPorts gets all the filtered ports.
func (os *OpenStack) getPorts(listOpts ports.ListOpts) ([]ports.Port, error) {
	allPages, err := ports.List(os.neutron, listOpts).AllPages()
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
func (os *OpenStack) EnsureFloatingIP(needDelete bool, portID string, existingfloatingIP string, floatingIPNetwork string, description string) (string, error) {
	listOpts := floatingips.ListOpts{PortID: portID}
	fips, err := os.getFloatingIPs(listOpts)
	if err != nil {
		return "", fmt.Errorf("unable to get floating ips: %w", err)
	}

	// If needed, delete the floating IPs and return.
	if needDelete {
		for _, fip := range fips {
			if err := floatingips.Delete(os.neutron, fip.ID).ExtractErr(); err != nil {
				return "", err
			}
		}

		return "", nil
	}

	if len(fips) > 1 {
		return "", fmt.Errorf("more than one floating IPs for port %s found", portID)
	}

	// check if provided fip is available
	var fip *floatingips.FloatingIP
	if existingfloatingIP != "" {
		// try to find fip
		opts := floatingips.ListOpts{
			FloatingIP:        existingfloatingIP,
			FloatingNetworkID: floatingIPNetwork,
		}
		osFips, err := os.getFloatingIPs(opts)
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
		// check if fip is used
		if osFips[0].PortID != "" {
			return "", fmt.Errorf("floating IP %s already used by port %s", osFips[0].FloatingIP, osFips[0].PortID)
		}
		fip = &osFips[0]
	}

	// if port don't have fip
	if len(fips) == 0 {
		// if user provided fip to use
		if fip != nil {
			updateOpts := floatingips.UpdateOpts{
				PortID:      &portID,
				Description: &description,
			}
			// attach fip to lb vip
			fip, err = floatingips.Update(os.neutron, fip.ID, updateOpts).Extract()
			if err != nil {
				return "", err
			}
		} else {
			floatIPOpts := floatingips.CreateOpts{
				PortID:            portID,
				FloatingNetworkID: floatingIPNetwork,
				Description:       description,
			}
			fip, err = floatingips.Create(os.neutron, floatIPOpts).Extract()
			if err != nil {
				return "", err
			}
		}
	} else {
		// if port exist but the fip binded to it's not the one provided by user
		if fip.FloatingIP != fips[0].FloatingIP {
			// disassociate old fip : if update fip without disassociate
			// Openstack retrun http 409 error
			// "Cannot associate floating IP with port using fixed
			//  IP, as that fixed IP already has a floating IP on
			//  external network"
			updateDisassociateOpts := floatingips.UpdateOpts{
				PortID: new(string),
			}
			_, err = floatingips.Update(os.neutron, fips[0].ID, updateDisassociateOpts).Extract()
			if err != nil {
				return "", err
			}
			// associate new fip
			updateOpts := floatingips.UpdateOpts{
				PortID:      &portID,
				Description: &description,
			}
			fip, err = floatingips.Update(os.neutron, fip.ID, updateOpts).Extract()
			if err != nil {
				return "", err
			}
		}
		fip = &fips[0]
	}

	return fip.FloatingIP, nil
}

// GetSecurityGroups gets all the filtered security groups.
func (os *OpenStack) GetSecurityGroups(listOpts groups.ListOpts) ([]groups.SecGroup, error) {
	allPages, err := groups.List(os.neutron, listOpts).AllPages()
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
func (os *OpenStack) EnsureSecurityGroup(needDelete bool, name string, description string, tags []string) (string, error) {
	tagsString := strings.Join(tags, ",")
	listOpts := groups.ListOpts{Tags: tagsString}
	allGroups, err := os.GetSecurityGroups(listOpts)
	if err != nil {
		return "", err
	}

	// If needed, delete the security groups and return.
	if needDelete {
		for _, group := range allGroups {
			if err := groups.Delete(os.neutron, group.ID).ExtractErr(); err != nil {
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
		group, err = groups.Create(os.neutron, createOpts).Extract()
		if err != nil {
			return "", err
		}

		// Do not use tags replace API until https://bugs.launchpad.net/neutron/+bug/1817238 is resolved.
		//tagReplaceAllOpts := neutrontags.ReplaceAllOpts{Tags: tags}
		//if _, err := neutrontags.ReplaceAll(os.neutron, "security_groups", group.ID, tagReplaceAllOpts).Extract(); err != nil {
		//	return "", fmt.Errorf("failed to add tags %s to security group %s: %v", tagsString, group.ID, err)
		//}

		for _, t := range tags {
			if err := neutrontags.Add(os.neutron, "security_groups", group.ID, t).ExtractErr(); err != nil {
				return "", fmt.Errorf("failed to add tag %s to security group %s: %v", t, group.ID, err)
			}
		}
	} else {
		group = &allGroups[0]
	}

	return group.ID, nil
}

// EnsureSecurityGroupRules ensures the only dstPorts are allowed in the given security group.
func (os *OpenStack) EnsureSecurityGroupRules(sgID string, sourceIP string, dstPorts []int) error {
	listOpts := rules.ListOpts{
		Protocol:       "tcp",
		SecGroupID:     sgID,
		RemoteIPPrefix: sourceIP,
	}
	allPages, err := rules.List(os.neutron, listOpts).AllPages()
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
			if err := rules.Delete(os.neutron, rule.ID).ExtractErr(); err != nil {
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
			if err := rules.Delete(os.neutron, rule.ID).ExtractErr(); err != nil {
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
		if _, err := rules.Create(os.neutron, createOpts).Extract(); err != nil {
			return err
		}
	}

	return nil
}

// EnsurePortSecurityGroup ensures the security group is attached to all the node ports or detached from all the ports
// according to needDelete param.
func (os *OpenStack) EnsurePortSecurityGroup(needDelete bool, sgID string, nodes []*v1.Node) error {
	for _, node := range nodes {
		instanceID, err := utils.GetNodeID(node)
		if err != nil {
			return err
		}
		listOpts := ports.ListOpts{DeviceID: instanceID}
		allPorts, err := os.getPorts(listOpts)
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
				if _, err := ports.Update(os.neutron, port.ID, updateOpts).Extract(); err != nil {
					return err
				}

				log.WithFields(log.Fields{"sgID": sgID, "portID": port.ID}).Debug("security group detached from the port")
			}

			if !sgSet.Has(sgID) && !needDelete {
				// Add sg to the port
				sgSet.Insert(sgID)
				newSGs := sets.List(sgSet)
				updateOpts := ports.UpdateOpts{SecurityGroups: &newSGs}
				if _, err := ports.Update(os.neutron, port.ID, updateOpts).Extract(); err != nil {
					return err
				}

				log.WithFields(log.Fields{"sgID": sgID, "portID": port.ID}).Debug("security group attached to the port")
			}
		}
	}

	return nil
}
