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

package cinder

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	"k8s.io/klog/v2"
)

// changedBlocksWindowBytes bounds the byte range that one series of
// os-list_changed_blocks requests covers, so that a backend never has to
// materialise the change map of a whole multi-terabyte snapshot at once. It
// is a variable so that tests can walk several windows over a small volume.
var changedBlocksWindowBytes int64 = 64 << 30

const (
	// defaultMetadataBatchSize is the number of extents per streamed message
	// when the caller leaves max_results at zero.
	defaultMetadataBatchSize = 512

	// changedBlocksFallbackLimit is the page size the 413 retry starts
	// halving from when no limit had been sent; it matches Cinder's default
	// changed_blocks_max_limit.
	changedBlocksFallbackLimit = 1000

	// cinderErrorBodyLimit bounds how much of a raw Cinder error body is
	// copied into a gRPC status message.
	cinderErrorBodyLimit = 300

	// webobNotFoundMessage is the explanation webob supplies for an
	// HTTPNotFound raised without one. Cinder answers that way when the
	// action exists but not at the requested microversion, whereas a missing
	// volume or snapshot is always reported with a message that names it.
	webobNotFoundMessage = "The resource could not be found."

	// noSuchActionPrefix starts Cinder's 400 for a volume action its API does
	// not know at all.
	noSuchActionPrefix = "There is no such action"
)

type snapshotMetadataServer struct {
	Driver *Driver
	Clouds map[string]openstack.IOpenStack
	csi.UnimplementedSnapshotMetadataServer
}

// NewSnapshotMetadataServer returns the CSI SnapshotMetadata service backed
// by the Cinder os-list_changed_blocks volume action.
func NewSnapshotMetadataServer(d *Driver, clouds map[string]openstack.IOpenStack) *snapshotMetadataServer {
	return &snapshotMetadataServer{
		Driver: d,
		Clouds: clouds,
	}
}

// metadataQuery is the part of a GetMetadataAllocated or GetMetadataDelta
// request that the shared engine works from. An empty base snapshot asks
// Cinder for the allocation map of the target instead of a delta.
type metadataQuery struct {
	op               string
	delta            bool
	baseSnapshotID   string
	targetSnapshotID string
	startingOffset   int64
	maxResults       int32
	secrets          map[string]string
}

// metadataSender delivers one message of the RPC-specific response type.
type metadataSender func(capacity int64, blocks []*csi.BlockMetadata) error

func (s *snapshotMetadataServer) GetMetadataAllocated(req *csi.GetMetadataAllocatedRequest, stream csi.SnapshotMetadata_GetMetadataAllocatedServer) error {
	klog.V(4).Infof("GetMetadataAllocated: called with args %+v", protosanitizer.StripSecrets(req))

	query := metadataQuery{
		op:               "GetMetadataAllocated",
		targetSnapshotID: req.GetSnapshotId(),
		startingOffset:   req.GetStartingOffset(),
		maxResults:       req.GetMaxResults(),
		secrets:          req.GetSecrets(),
	}

	return s.streamMetadata(stream.Context(), query, func(capacity int64, blocks []*csi.BlockMetadata) error {
		return stream.Send(&csi.GetMetadataAllocatedResponse{
			BlockMetadataType:   csi.BlockMetadataType_VARIABLE_LENGTH,
			VolumeCapacityBytes: capacity,
			BlockMetadata:       blocks,
		})
	})
}

func (s *snapshotMetadataServer) GetMetadataDelta(req *csi.GetMetadataDeltaRequest, stream csi.SnapshotMetadata_GetMetadataDeltaServer) error {
	klog.V(4).Infof("GetMetadataDelta: called with args %+v", protosanitizer.StripSecrets(req))

	query := metadataQuery{
		op:               "GetMetadataDelta",
		delta:            true,
		baseSnapshotID:   req.GetBaseSnapshotId(),
		targetSnapshotID: req.GetTargetSnapshotId(),
		startingOffset:   req.GetStartingOffset(),
		maxResults:       req.GetMaxResults(),
		secrets:          req.GetSecrets(),
	}

	return s.streamMetadata(stream.Context(), query, func(capacity int64, blocks []*csi.BlockMetadata) error {
		return stream.Send(&csi.GetMetadataDeltaResponse{
			BlockMetadataType:   csi.BlockMetadataType_VARIABLE_LENGTH,
			VolumeCapacityBytes: capacity,
			BlockMetadata:       blocks,
		})
	})
}

// streamMetadata validates the request, resolves the target snapshot and
// streams its changed or allocated blocks in max_results-sized messages.
func (s *snapshotMetadataServer) streamMetadata(ctx context.Context, q metadataQuery, send metadataSender) error {
	cloud, ok := s.Clouds[q.secrets["cloud"]]
	if !ok {
		return status.Errorf(codes.InvalidArgument, "[%s] specified cloud undefined", q.op)
	}

	if err := q.validate(); err != nil {
		return err
	}

	snap, err := lookupSnapshot(ctx, cloud, q.targetSnapshotID, q.op)
	if err != nil {
		return err
	}

	if q.delta {
		// Cinder would reject a bad base with a 400, but resolving it here
		// gives the base the same status mapping as the target: a vanished
		// base is NOT_FOUND and one in flight is retried, not treated as a
		// malformed request.
		base, err := lookupSnapshot(ctx, cloud, q.baseSnapshotID, q.op)
		if err != nil {
			return err
		}
		if base.VolumeID != snap.VolumeID {
			return status.Errorf(codes.InvalidArgument, "[%s] base snapshot %s belongs to volume %s but target snapshot %s belongs to volume %s", q.op, base.ID, base.VolumeID, snap.ID, snap.VolumeID)
		}
	}

	capacity := int64(snap.Size) << 30
	if q.startingOffset >= capacity {
		return status.Errorf(codes.OutOfRange, "[%s] starting offset %d is beyond the %d byte capacity of snapshot %s", q.op, q.startingOffset, capacity, snap.ID)
	}

	batch := int(q.maxResults)
	if batch <= 0 {
		batch = defaultMetadataBatchSize
	}

	return walkChangedBlocks(ctx, cloud, q, snap, capacity, batch, send)
}

func (q metadataQuery) validate() error {
	switch {
	case q.delta && q.targetSnapshotID == "":
		return status.Errorf(codes.InvalidArgument, "[%s] missing target snapshot ID", q.op)
	case q.delta && q.baseSnapshotID == "":
		return status.Errorf(codes.InvalidArgument, "[%s] missing base snapshot ID", q.op)
	case q.delta && q.baseSnapshotID == q.targetSnapshotID:
		return status.Errorf(codes.InvalidArgument, "[%s] base and target snapshot IDs must differ", q.op)
	case !q.delta && q.targetSnapshotID == "":
		return status.Errorf(codes.InvalidArgument, "[%s] missing snapshot ID", q.op)
	case q.startingOffset < 0:
		return status.Errorf(codes.OutOfRange, "[%s] starting offset %d is negative", q.op, q.startingOffset)
	case q.maxResults < 0:
		return status.Errorf(codes.InvalidArgument, "[%s] max results %d is negative", q.op, q.maxResults)
	}

	return nil
}

// lookupSnapshot fetches a snapshot and checks that changed blocks can be
// asked about it.
func lookupSnapshot(ctx context.Context, cloud openstack.IOpenStack, id, op string) (*snapshots.Snapshot, error) {
	snap, err := cloud.GetSnapshotByID(ctx, id)
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "[%s] snapshot %s not found", op, id)
		}
		return nil, changedBlocksStatus(err, op)
	}

	if err := snapshotDiffable(snap, op); err != nil {
		return nil, err
	}

	return snap, nil
}

// snapshotDiffable maps the snapshot's Cinder state onto the code a caller
// should see before any changed block request is made: a vanishing snapshot
// is NOT_FOUND, a broken one cannot be compared, and one still in flight
// should be retried later.
func snapshotDiffable(snap *snapshots.Snapshot, op string) error {
	switch st := snap.Status; {
	case st == "available":
	case st == "deleting" || st == "deleted":
		return status.Errorf(codes.NotFound, "[%s] snapshot %s is %s", op, snap.ID, st)
	case strings.HasPrefix(st, "error"):
		return status.Errorf(codes.FailedPrecondition, "[%s] snapshot %s is in status %q and cannot be compared", op, snap.ID, st)
	default:
		return status.Errorf(codes.Unavailable, "[%s] snapshot %s is %s, retry once it is available", op, snap.ID, st)
	}

	if snap.VolumeID == "" {
		return status.Errorf(codes.Internal, "[%s] snapshot %s has no volume ID", op, snap.ID)
	}
	if snap.Size <= 0 {
		return status.Errorf(codes.Internal, "[%s] snapshot %s reports a size of %d GiB", op, snap.ID, snap.Size)
	}

	return nil
}

// walkChangedBlocks asks Cinder for the extents of every window between the
// starting offset and the end of the snapshot, following page markers inside
// each window, and streams them re-batched to the caller's message size.
func walkChangedBlocks(ctx context.Context, cloud openstack.IOpenStack, q metadataQuery, snap *snapshots.Snapshot, capacity int64, batch int, send metadataSender) error {
	var pending []*csi.BlockMetadata
	messages := 0
	emit := func(blocks []*csi.BlockMetadata) error {
		if err := send(capacity, blocks); err != nil {
			return status.Errorf(codes.Internal, "[%s] failed to send response: %v", q.op, err)
		}
		messages++
		return nil
	}

	// streamEnd is where the last extent handed to the client ends. Later
	// extents are clipped to start at or after it so that the stream stays
	// strictly increasing even if a backend reports a range twice.
	var streamEnd int64
	total := 0

	for offset := q.startingOffset; offset < capacity; {
		length := min(changedBlocksWindowBytes, capacity-offset)
		marker := int64(0)
		limit := 0

		for {
			if err := ctx.Err(); err != nil {
				return status.FromContextError(err).Err()
			}

			page, err := cloud.ListChangedBlocks(ctx, snap.VolumeID, openstack.ListChangedBlocksOpts{
				BaseSnapshotID:   q.baseSnapshotID,
				TargetSnapshotID: q.targetSnapshotID,
				Offset:           offset,
				Length:           length,
				Marker:           marker,
				Limit:            limit,
			})
			if gophercloud.ResponseCodeIs(err, http.StatusRequestEntityTooLarge) {
				// The backend cannot answer a page this large; ask for a
				// smaller one from the same marker.
				if limit == 0 {
					limit = changedBlocksFallbackLimit
				}
				limit /= 2
				if limit < 1 {
					return status.Errorf(codes.Internal, "[%s] Cinder rejected even a single-extent changed blocks page for volume %s", q.op, snap.VolumeID)
				}
				klog.V(4).Infof("%s: retrying changed blocks of volume %s at marker %d with limit %d", q.op, snap.VolumeID, marker, limit)
				continue
			}
			if err != nil {
				return changedBlocksStatus(err, q.op)
			}

			blocks := normaliseChangedBlocks(page.Blocks, streamEnd, capacity)
			if n := len(blocks); n > 0 {
				streamEnd = blocks[n-1].ByteOffset + blocks[n-1].SizeBytes
				total += n
			}

			pending = append(pending, blocks...)
			for len(pending) >= batch {
				if err := emit(pending[:batch]); err != nil {
					return err
				}
				pending = pending[batch:]
			}

			if page.NextMarker == nil {
				break
			}
			next := *page.NextMarker
			if next >= offset+length {
				break
			}
			if next <= marker {
				return status.Errorf(codes.Internal, "[%s] Cinder returned marker %d after marker %d for volume %s", q.op, next, marker, snap.VolumeID)
			}
			marker = next
		}

		offset += length
	}

	for len(pending) > 0 {
		n := min(batch, len(pending))
		if err := emit(pending[:n]); err != nil {
			return err
		}
		pending = pending[n:]
	}

	if messages == 0 {
		// The client still needs the capacity, so an empty result is one
		// message rather than a bare end of stream.
		return emit([]*csi.BlockMetadata{})
	}

	klog.V(4).Infof("%s: streamed %d extents of snapshot %s in %d messages", q.op, total, snap.ID, messages)
	return nil
}

// normaliseChangedBlocks turns one page of Cinder extents into CSI block
// metadata confined to [lo, hi): unallocated and empty extents are dropped,
// the rest are clipped to the range, sorted by offset and merged where they
// overlap or touch, so that the result is strictly increasing.
func normaliseChangedBlocks(blocks []openstack.ChangedBlock, lo, hi int64) []*csi.BlockMetadata {
	clipped := make([]openstack.ChangedBlock, 0, len(blocks))
	for _, b := range blocks {
		if !b.Exists || b.Length <= 0 || b.Offset >= hi {
			continue
		}

		start := max(b.Offset, lo)
		end := hi
		if b.Length < hi-b.Offset {
			end = b.Offset + b.Length
		}
		if start >= end {
			continue
		}

		clipped = append(clipped, openstack.ChangedBlock{Offset: start, Length: end - start})
	}

	slices.SortFunc(clipped, func(a, b openstack.ChangedBlock) int {
		return cmp.Compare(a.Offset, b.Offset)
	})

	out := make([]*csi.BlockMetadata, 0, len(clipped))
	for _, b := range clipped {
		if n := len(out); n > 0 {
			last := out[n-1]
			if lastEnd := last.ByteOffset + last.SizeBytes; b.Offset <= lastEnd {
				if end := b.Offset + b.Length; end > lastEnd {
					last.SizeBytes = end - last.ByteOffset
				}
				continue
			}
		}

		out = append(out, &csi.BlockMetadata{ByteOffset: b.Offset, SizeBytes: b.Length})
	}

	return out
}

// changedBlocksStatus maps a Cinder client error onto the gRPC status the
// SnapshotMetadata contract asks for: permanent conditions become
// FAILED_PRECONDITION so that a backup tool falls back to a full copy, and
// transient faults become UNAVAILABLE so that it retries.
func changedBlocksStatus(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	if errors.Is(err, openstack.ErrChangedBlocksUnsupported) {
		return status.Errorf(codes.FailedPrecondition, "[%s] %v", op, err)
	}

	var httpErr gophercloud.ErrUnexpectedResponseCode
	if !errors.As(err, &httpErr) {
		return status.Errorf(codes.Unavailable, "[%s] Cinder request failed: %v", op, err)
	}

	msg := cinderErrorMessage(httpErr.Body)
	if msg == "" {
		msg = http.StatusText(httpErr.Actual)
	}

	switch httpErr.Actual {
	case http.StatusBadRequest:
		if strings.HasPrefix(msg, noSuchActionPrefix) {
			// The cloud accepted the microversion but has no such action:
			// a missing feature, not a malformed request.
			return status.Errorf(codes.FailedPrecondition, "[%s] Cinder has no os-list_changed_blocks action: %s", op, msg)
		}
		return status.Errorf(codes.InvalidArgument, "[%s] %s", op, msg)
	case http.StatusUnauthorized:
		return status.Errorf(codes.Unauthenticated, "[%s] %s", op, msg)
	case http.StatusForbidden:
		return status.Errorf(codes.PermissionDenied, "[%s] %s", op, msg)
	case http.StatusNotFound:
		if msg == webobNotFoundMessage || len(bytes.TrimSpace(httpErr.Body)) == 0 {
			// A 404 that names no object is about the action, not about
			// the snapshots: the action is not served at this microversion.
			return status.Errorf(codes.FailedPrecondition, "[%s] Cinder has no os-list_changed_blocks action at microversion %s", op, openstack.ChangedBlocksMicroversion)
		}
		return status.Errorf(codes.NotFound, "[%s] %s", op, msg)
	case http.StatusNotAcceptable:
		return status.Errorf(codes.FailedPrecondition, "[%s] Block Storage API microversion %s is not supported by this cloud: %s", op, openstack.ChangedBlocksMicroversion, msg)
	case http.StatusConflict:
		return status.Errorf(codes.FailedPrecondition, "[%s] %s", op, msg)
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return status.Errorf(codes.Unavailable, "[%s] %s", op, msg)
	default:
		return status.Errorf(codes.Internal, "[%s] Cinder returned HTTP %d: %s", op, httpErr.Actual, msg)
	}
}

// cinderErrorMessage extracts the human readable message from a Cinder error
// body such as {"badRequest": {"code": 400, "message": "..."}}. It falls back
// to the trimmed raw body, bounded so that a proxy's HTML page does not end
// up in a gRPC status.
func cinderErrorMessage(body []byte) string {
	var envelope map[string]struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		for _, key := range slices.Sorted(maps.Keys(envelope)) {
			if msg := strings.TrimSpace(envelope[key].Message); msg != "" {
				return msg
			}
		}
	}

	raw := strings.TrimSpace(string(body))
	if len(raw) > cinderErrorBodyLimit {
		cut := cinderErrorBodyLimit
		for cut > 0 && !utf8.RuneStart(raw[cut]) {
			cut--
		}
		raw = raw[:cut] + "..."
	}

	return raw
}
