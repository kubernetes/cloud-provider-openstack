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
	"k8s.io/klog"
)

const (
	waitForAvailableShareTimeout = 3
	waitForAvailableShareRetries = 10

	shareCreating      = "creating"
	shareDeleting      = "deleting"
	shareError         = "error"
	shareErrorDeleting = "error_deleting"
	shareAvailable     = "available"

	shareDescription = "provisioned-by=manila.csi.openstack.org"
)

// getOrCreateShare first retrieves an existing share with name=shareName, or creates a new one if it doesn't exist yet.
// Once the share is created, an exponential back-off is used to wait till the status of the share is "available".
func getOrCreateShare(shareName string, createOpts *shares.CreateOpts, manilaClient *gophercloud.ServiceClient) (*shares.Share, manilaError, error) {
	var (
		share *shares.Share
		err   error
	)

	// First, check if the share already exists or needs to be created

	if share, err = getShareByName(shareName, manilaClient); err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
			// It doesn't exist, create it

			var createErr error
			if share, createErr = shares.Create(manilaClient, createOpts).Extract(); createErr != nil {
				return nil, 0, createErr
			}
		} else {
			// Something else is wrong
			return nil, 0, fmt.Errorf("failed to probe for a share named %s: %v", shareName, err)
		}
	} else {
		klog.V(4).Infof("a share named %s already exists", shareName)
	}

	// It exists, wait till it's Available

	if share.Status == shareAvailable {
		return share, 0, nil
	}

	return waitForShareStatus(share.ID, shareCreating, shareAvailable, false, manilaClient)
}

func deleteShare(shareID string, manilaClient *gophercloud.ServiceClient) error {
	if err := shares.Delete(manilaClient, shareID).ExtractErr(); err != nil {
		if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
			klog.V(4).Infof("share %s not found, assuming it to be already deleted", shareID)
		} else {
			return err
		}
	}

	return nil
}

func tryDeleteShare(share *shares.Share, manilaClient *gophercloud.ServiceClient) {
	if share == nil {
		return
	}

	if err := deleteShare(share.ID, manilaClient); err != nil {
		// TODO failure to delete a share in an error state needs proper monitoring support
		klog.Errorf("couldn't delete share %s in a roll-back procedure: %v", share.ID, err)
		return
	}

	_, _, err := waitForShareStatus(share.ID, shareDeleting, "", true, manilaClient)
	if err != nil && err != wait.ErrWaitTimeout {
		klog.Errorf("couldn't retrieve share %s in a roll-back procedure: %v", share.ID, err)
	}
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

func waitForShareStatus(shareID, currentStatus, desiredStatus string, successOnNotFound bool, manilaClient *gophercloud.ServiceClient) (*shares.Share, manilaError, error) {
	var (
		backoff = wait.Backoff{
			Duration: time.Second * waitForAvailableShareTimeout,
			Factor:   1.2,
			Steps:    waitForAvailableShareRetries,
		}

		share         *shares.Share
		manilaErrCode manilaError
		err           error
	)

	return share, manilaErrCode, wait.ExponentialBackoff(backoff, func() (bool, error) {
		share, err = getShareByID(shareID, manilaClient)

		if err != nil {
			if _, ok := err.(gophercloud.ErrDefault404); ok && successOnNotFound {
				return true, nil
			}

			return false, err
		}

		var isAvailable bool

		switch share.Status {
		case currentStatus:
			isAvailable = false
		case desiredStatus:
			isAvailable = true
		case shareError:
			manilaErrMsg, err := lastResourceError(shareID, manilaClient)
			if err != nil {
				return false, fmt.Errorf("share %s is in error state, error description could not be retrieved: %v", shareID, err)
			}

			manilaErrCode = manilaErrMsg.errCode
			return false, fmt.Errorf("share %s is in error state: %s", shareID, manilaErrMsg.message)
		default:
			return false, fmt.Errorf("share %s is in an unexpected state: wanted either %s or %s, got %s", shareID, currentStatus, desiredStatus, share.Status)
		}

		return isAvailable, nil
	})
}
