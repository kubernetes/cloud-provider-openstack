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
	"errors"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/utils"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/klog/v2"
)

// ChangedBlocksMicroversion is the Block Storage API microversion that
// introduced the os-list_changed_blocks volume action.
const ChangedBlocksMicroversion = "3.72"

// ErrChangedBlocksUnsupported is returned when the driver configuration
// forbids the microversion that os-list_changed_blocks requires.
var ErrChangedBlocksUnsupported = errors.New("changed block tracking requires microversion " + ChangedBlocksMicroversion + " but ignore-volume-microversion is set")

// ChangedBlock is one extent reported by os-list_changed_blocks. Offset and
// Length are bytes within the target snapshot. Exists is false for a range
// the backend knows to hold no data, which a consumer may skip.
type ChangedBlock struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
	Exists bool  `json:"exists"`
}

// ChangedBlocksPage is one page of extents. NextMarker is nil when Cinder
// reported no further extents for the requested window.
type ChangedBlocksPage struct {
	Blocks     []ChangedBlock
	NextMarker *int64
}

// ListChangedBlocksOpts selects what os-list_changed_blocks compares and
// which part of the result to return. Zero values mean "omit the field":
// Offset 0 and Marker 0 equal omission by definition, and Length 0 and
// Limit 0 are invalid on the wire and so mean "not set".
type ListChangedBlocksOpts struct {
	// BaseSnapshotID is the older snapshot. Empty requests the allocation
	// map of the target snapshot instead of a delta.
	BaseSnapshotID string
	// TargetSnapshotID is the newer snapshot and is required.
	TargetSnapshotID string
	// Offset and Length bound the byte window that is compared.
	Offset int64
	Length int64
	// Marker and Limit page through the extents of the window.
	Marker int64
	Limit  int
}

// changedBlocksAction mirrors the wire body. The Cinder schema rejects
// unknown keys, so optional fields are omitted rather than sent as zero.
type changedBlocksAction struct {
	BaseSnapshotID   string `json:"base_snapshot_id,omitempty"`
	TargetSnapshotID string `json:"target_snapshot_id"`
	Offset           int64  `json:"offset,omitempty"`
	Length           int64  `json:"length,omitempty"`
	Marker           int64  `json:"marker,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

type changedBlocksRequest struct {
	Action changedBlocksAction `json:"os-list_changed_blocks"`
}

type changedBlocksResponse struct {
	ChangedBlocks []ChangedBlock `json:"changed_blocks"`
	NextMarker    *int64         `json:"changed_blocks_next_marker"`
}

// ListChangedBlocks returns one page of the blocks that differ between two
// snapshots of volumeID, or of the allocated blocks of the target snapshot
// when no base is given. Errors from gophercloud are returned unchanged so
// that callers can map the HTTP status code to their own error model.
func (os *OpenStack) ListChangedBlocks(ctx context.Context, volumeID string, opts ListChangedBlocksOpts) (*ChangedBlocksPage, error) {
	if os.bsOpts.IgnoreVolumeMicroversion {
		return nil, ErrChangedBlocksUnsupported
	}

	// The two structs share their field list; only the JSON tags differ.
	body := changedBlocksRequest{Action: changedBlocksAction(opts)}

	client := os.blockStorageClientWithMicroversion(ChangedBlocksMicroversion)

	var result changedBlocksResponse
	mc := metrics.NewMetricContext("volume", "list_changed_blocks")
	_, err := client.Post(ctx, client.ServiceURL("volumes", volumeID, "action"), body, &result, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	})
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	klog.V(4).Infof("Listed %d changed blocks for volume %s", len(result.ChangedBlocks), volumeID)
	return &ChangedBlocksPage{
		Blocks:     result.ChangedBlocks,
		NextMarker: result.NextMarker,
	}, nil
}

// ChangedBlocksSupported reports whether the Block Storage endpoint accepts
// the microversion that carries os-list_changed_blocks.
func (os *OpenStack) ChangedBlocksSupported(ctx context.Context) (bool, error) {
	if os.bsOpts.IgnoreVolumeMicroversion {
		return false, nil
	}

	// Cinder serves its version document at the versioned root of the
	// endpoint; a catalog URL that carries the project id has nothing there.
	discovery, err := utils.BaseVersionedEndpoint(os.blockstorage.Endpoint)
	if err != nil {
		return false, err
	}
	client := *os.blockstorage
	client.Endpoint = discovery
	client.ResourceBase = discovery

	supported, err := utils.GetSupportedMicroversions(ctx, &client)
	if err != nil {
		return false, err
	}

	return supported.IsSupported(ChangedBlocksMicroversion)
}
