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

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/pagination"
	log "github.com/sirupsen/logrus"
)

func (os *OpenStack) getFloatingIPByPortID(portID string) (*floatingips.FloatingIP, error) {
	floatingIPList := make([]floatingips.FloatingIP, 0, 1)
	opts := floatingips.ListOpts{
		PortID: portID,
	}
	pager := floatingips.List(os.neutron, opts)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		f, err := floatingips.ExtractFloatingIPs(page)
		if err != nil {
			return false, err
		}
		floatingIPList = append(floatingIPList, f...)
		if len(floatingIPList) > 1 {
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

	if len(floatingIPList) == 0 {
		return nil, ErrNotFound
	}

	return &floatingIPList[0], nil
}

// EnsureFloatingIP makes sure a floating IP is allocated for the port
func (os *OpenStack) EnsureFloatingIP(portID string, floatingIPNetwork string) (string, error) {
	fip, err := os.getFloatingIPByPortID(portID)
	if err != nil {
		if err != ErrNotFound {
			return "", fmt.Errorf("error getting floating ip for port %s: %v", portID, err)
		}

		log.WithFields(log.Fields{"portID": portID}).Debug("creating floating ip for port")

		floatIPOpts := floatingips.CreateOpts{
			FloatingNetworkID: floatingIPNetwork,
			PortID:            portID,
		}
		fip, err = floatingips.Create(os.neutron, floatIPOpts).Extract()
		if err != nil {
			return "", fmt.Errorf("error creating floating ip for port %s: %v", portID, err)
		}

		log.WithFields(log.Fields{"portID": portID, "floatingip": fip.FloatingIP}).Info("floating ip for port created")
	}

	return fip.FloatingIP, nil
}

// DeleteFloatingIP deletes floating ip for the port
func (os *OpenStack) DeleteFloatingIP(portID string) error {
	fip, err := os.getFloatingIPByPortID(portID)
	if err != nil {
		if err != ErrNotFound {
			return fmt.Errorf("error getting floating ip for port %s: %v", portID, err)
		}

		log.WithFields(log.Fields{"portID": portID}).Debug("floating ip not exists")

		return nil
	}

	err = floatingips.Delete(os.neutron, fip.ID).ExtractErr()
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("error deleting floating ip %s: %v", fip.ID, err)
	}

	log.WithFields(log.Fields{"floatingip": fip.FloatingIP}).Info("floating ip deleted")

	return nil
}
