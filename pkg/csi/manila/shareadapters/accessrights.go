/*
Copyright 2026 The Kubernetes Authors.

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

package shareadapters

import (
	"context"
	"fmt"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/klog/v2"
)

const (
	accessRuleStateActive      = "active"
	accessRuleStateError       = "error"
	accessRuleStateQueuedApply = "queued_to_apply"
	accessRuleStateApplying    = "applying"

	waitForAccessRuleTimeout = 3
	waitForAccessRuleRetries = 10
)

func waitForAccessRuleActive(ctx context.Context, manilaClient manilaclient.Interface, shareID, accessID string) (*shares.AccessRight, error) {
	var (
		backoff = wait.Backoff{
			Duration: time.Second * waitForAccessRuleTimeout,
			Factor:   1.2,
			Steps:    waitForAccessRuleRetries,
		}
		result *shares.AccessRight
	)

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		rights, err := manilaClient.GetAccessRights(ctx, shareID)
		if err != nil {
			return false, fmt.Errorf("failed to get access rights for share %s: %v", shareID, err)
		}

		for i := range rights {
			if rights[i].ID != accessID {
				continue
			}

			switch rights[i].State {
			case accessRuleStateActive:
				result = &rights[i]
				return true, nil
			case accessRuleStateError:
				revokeErr := manilaClient.RevokeAccess(ctx, shareID, accessID)
				if revokeErr != nil {
					klog.Errorf("failed to revoke errored access rule %s for share %s: %v", accessID, shareID, revokeErr)
				}
				return false, fmt.Errorf("access rule %s for share %s is in error state", accessID, shareID)
			case accessRuleStateQueuedApply, accessRuleStateApplying:
				klog.V(4).Infof("access rule %s for share %s is in state %s, retrying...", accessID, shareID, rights[i].State)
				return false, nil
			default:
				return false, fmt.Errorf("access rule %s for share %s is in unexpected state %s", accessID, shareID, rights[i].State)
			}
		}

		return false, fmt.Errorf("access rule %s not found for share %s", accessID, shareID)
	})

	if err != nil {
		if wait.Interrupted(err) {
			revokeErr := manilaClient.RevokeAccess(ctx, shareID, accessID)
			if revokeErr != nil {
				klog.Errorf("failed to revoke timed-out access rule %s for share %s: %v", accessID, shareID, revokeErr)
			}
			return nil, fmt.Errorf("timed out waiting for access rule %s for share %s to become active", accessID, shareID)
		}
		return nil, err
	}

	return result, nil
}
