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

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	clouderrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

const (
	waitForAvailableShareTimeout = 3
	waitForAvailableShareRetries = 10

	shareCreating             = "creating"
	shareCreatingFromSnapshot = "creating_from_snapshot"
	shareDeleting             = "deleting"
	shareExtending            = "extending"
	shareError                = "error"
	shareErrorDeleting        = "error_deleting"
	shareErrorExtending       = "extending_error"
	shareAvailable            = "available"

	shareDescription = "provisioned-by=manila.csi.openstack.org"
)

var (
	shareErrorStatuses = map[string]struct{}{
		shareError:          {},
		shareErrorDeleting:  {},
		shareErrorExtending: {},
	}
)

func isShareInErrorState(s string) bool {
	_, found := shareErrorStatuses[s]
	return found
}

// getOrCreateShare first retrieves an existing share with name=shareName, or creates a new one if it doesn't exist yet.
// Once the share is created, an exponential back-off is used to wait till the status of the share is "available".
func getOrCreateShare(shareName string, createOpts *shares.CreateOpts, manilaClient manilaclient.Interface) (*shares.Share, manilaError, error) {
	var (
		share *shares.Share
		err   error
	)

	// First, check if the share already exists or needs to be created

	if share, err = manilaClient.GetShareByName(shareName); err != nil {
		if clouderrors.IsNotFound(err) {
			// It doesn't exist, create it

			var createErr error
			if share, createErr = manilaClient.CreateShare(createOpts); createErr != nil {
				return nil, 0, createErr
			}
		} else {
			// Something else is wrong
			return nil, 0, fmt.Errorf("failed to retrieve volume %s: %v", shareName, err)
		}
	} else {
		klog.V(4).Infof("volume %s already exists", shareName)
	}

	// It exists, wait till it's Available

	if share.Status == shareAvailable {
		return share, 0, nil
	}

	return waitForShareStatus(share.ID, []string{shareCreating, shareCreatingFromSnapshot}, shareAvailable, false, manilaClient)
}

func deleteShare(shareID string, manilaClient manilaclient.Interface) error {
	if err := manilaClient.DeleteShare(shareID); err != nil {
		if clouderrors.IsNotFound(err) {
			klog.V(4).Infof("volume with share ID %s not found, assuming it to be already deleted", shareID)
		} else {
			return err
		}
	}

	return nil
}

func tryDeleteShare(share *shares.Share, manilaClient manilaclient.Interface) {
	if share == nil {
		return
	}

	if err := manilaClient.DeleteShare(share.ID); err != nil {
		// TODO failure to delete a share in an error state needs proper monitoring support
		klog.Errorf("couldn't delete volume %s in a roll-back procedure: %v", share.Name, err)
		return
	}

	_, _, err := waitForShareStatus(share.ID, []string{shareDeleting}, "", true, manilaClient)
	if err != nil && err != wait.ErrWaitTimeout {
		klog.Errorf("couldn't retrieve volume %s in a roll-back procedure: %v", share.Name, err)
	}
}

func extendShare(share *shares.Share, newSizeInGiB int, manilaClient manilaclient.Interface) error {
	opts := shares.ExtendOpts{
		NewSize: newSizeInGiB,
	}

	if err := manilaClient.ExtendShare(share.ID, opts); err != nil {
		return err
	}

	share, manilaErrCode, err := waitForShareStatus(share.ID, []string{shareExtending}, shareAvailable, false, manilaClient)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			return status.Errorf(codes.DeadlineExceeded, "deadline exceeded while waiting for volume ID %s to become available", share.Name)
		}

		return status.Errorf(manilaErrCode.toRpcErrorCode(), "failed to resize volume %s: %v", share.Name, err)
	}

	return nil
}

func waitForShareStatus(shareID string, validTransientStates []string, desiredStatus string, successOnNotFound bool, manilaClient manilaclient.Interface) (*shares.Share, manilaError, error) {
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

	isInTransientState := func(s string) bool {
		for _, val := range validTransientStates {
			if s == val {
				return true
			}
		}
		return false
	}

	return share, manilaErrCode, wait.ExponentialBackoff(backoff, func() (bool, error) {
		share, err = manilaClient.GetShareByID(shareID)

		if err != nil {
			if clouderrors.IsNotFound(err) && successOnNotFound {
				return true, nil
			}

			return false, err
		}

		if share.Status == desiredStatus {
			return true, nil
		}

		if isInTransientState(share.Status) {
			return false, nil
		}

		if isShareInErrorState(share.Status) {
			manilaErrMsg, err := lastResourceError(shareID, manilaClient)
			if err != nil {
				return false, fmt.Errorf("share %s is in error state, error description could not be retrieved: %v", shareID, err)
			}

			manilaErrCode = manilaErrMsg.errCode
			return false, fmt.Errorf("share %s is in error state \"%s\": %s", shareID, share.Status, manilaErrMsg.message)
		}

		return false, fmt.Errorf("share %s is in an unexpected state: wanted either %v or %s, got %s", shareID, validTransientStates, desiredStatus, share.Status)
	})
}
