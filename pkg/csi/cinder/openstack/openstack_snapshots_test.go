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

package openstack

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestWaitSnapshotDeleted(t *testing.T) {
	ctx := context.Background()
	quick := wait.Backoff{Duration: time.Millisecond, Factor: 1, Steps: 3}

	t.Run("not found", func(t *testing.T) {
		err := waitSnapshotDeleted(ctx, "snap", quick, func(context.Context, string) (*snapshots.Snapshot, error) {
			return nil, gophercloud.ErrUnexpectedResponseCode{Actual: http.StatusNotFound}
		})
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("error deleting", func(t *testing.T) {
		err := waitSnapshotDeleted(ctx, "snap", quick, func(context.Context, string) (*snapshots.Snapshot, error) {
			return &snapshots.Snapshot{Status: "error_deleting"}, nil
		})
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("still deleting", func(t *testing.T) {
		err := waitSnapshotDeleted(ctx, "snap", quick, func(context.Context, string) (*snapshots.Snapshot, error) {
			return &snapshots.Snapshot{Status: "deleting"}, nil
		})
		if err == nil {
			t.Fatal("expected a timeout")
		}
	})
}
