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

package manila

import (
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/klog"
)

const (
	annProvisionedBy = "manila.csi.openstack.org/provisioned-by"

	waitForAvailableShareTimeout = 3
	waitForAvailableShareRetries = 10
)

func (cs *controllerServer) getOrCreateShare(volID volumeID, sizeInGiB int, controllerCtx *options.ControllerVolumeContext, manilaClient *gophercloud.ServiceClient) (*shares.Share, error) {
	var (
		share *shares.Share
		err   error
	)

	// First, check if the share already exists or needs to be created

	if share, err = getShareByName(string(volID), manilaClient); err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
			// It doesn't exist, create it

			req := shares.CreateOpts{
				ShareProto:     controllerCtx.Protocol,
				ShareType:      controllerCtx.Type,
				ShareNetworkID: controllerCtx.ShareNetworkID,
				Name:           string(volID),
				Size:           sizeInGiB,
				Metadata: map[string]string{
					annProvisionedBy: cs.d.name,
				},
			}

			var createErr error
			if share, createErr = shares.Create(manilaClient, req).Extract(); createErr != nil {
				return nil, createErr
			}
		} else {
			// Something else is wrong
			return nil, fmt.Errorf("failed to probe for share: %v", err)
		}
	} else {
		klog.V(4).Infof("volume %s (share ID %s) already exists", volID, share.ID)
	}

	// It exists, wait till it's Available

	const available = "available"

	if share.Status == available {
		return share, nil
	}

	return waitForShareStatus(share.ID, available, manilaClient)
}

func deleteShare(volID volumeID, manilaClient *gophercloud.ServiceClient) error {
	share, err := getShareByName(string(volID), manilaClient)
	if err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
			klog.V(4).Infof("volume %s not found, assuming it to be already deleted", volID)
			return nil
		} else {
			// Something else is wrong
			return fmt.Errorf("failed to get ID for share %s: %v", volID, err)
		}
	}

	return shares.Delete(manilaClient, share.ID).Err
}

func getShareByID(shareID string, manilaClient *gophercloud.ServiceClient) (*shares.Share, error) {
	return shares.Get(manilaClient, shareID).Extract()
}

func getShareByName(shareName string, manilaClient *gophercloud.ServiceClient) (*shares.Share, error) {
	shareID, err := shares.IDFromName(manilaClient, shareName)
	if err != nil {
		return nil, err
	}

	return getShareByID(shareID, manilaClient)
}

func waitForShareStatus(shareID string, desired string, manilaClient *gophercloud.ServiceClient) (*shares.Share, error) {
	var (
		backoff = wait.Backoff{
			Duration: time.Second * waitForAvailableShareTimeout,
			Factor:   1.2,
			Steps:    waitForAvailableShareRetries,
		}

		share *shares.Share
		err   error
	)

	return share, wait.ExponentialBackoff(backoff, func() (bool, error) {
		share, err = getShareByID(shareID, manilaClient)

		if err != nil {
			return false, err
		}

		return share.Status == desired, nil
	})
}
