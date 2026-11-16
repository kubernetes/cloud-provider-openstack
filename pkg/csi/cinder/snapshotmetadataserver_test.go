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
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

const (
	cbtBaseID = "0f6c9f7a-0b2e-4d5a-9c1e-3a2b1c0d9e8f"
	cbtGiB    = int64(1) << 30
	cbtBlock  = int64(4096)
)

var cbtTargetID = FakeSnapshotID

type listFunc = func(ctx context.Context, volumeID string, opts openstack.ListChangedBlocksOpts) (*openstack.ChangedBlocksPage, error)

// listCall is one ListChangedBlocks request seen by the fake cloud.
type listCall struct {
	volumeID string
	opts     openstack.ListChangedBlocksOpts
}

// cbtCloud overrides the calls the SnapshotMetadata service makes and records
// every changed blocks request; the embedded mock supplies the rest of
// IOpenStack.
type cbtCloud struct {
	*openstack.OpenStackMock
	getSnapshot func(ctx context.Context, id string) (*snapshots.Snapshot, error)
	list        listFunc
	supported   func(ctx context.Context) (bool, error)
	calls       []listCall
}

func (c *cbtCloud) GetSnapshotByID(ctx context.Context, id string) (*snapshots.Snapshot, error) {
	if c.getSnapshot != nil {
		return c.getSnapshot(ctx, id)
	}
	return c.OpenStackMock.GetSnapshotByID(ctx, id)
}

func (c *cbtCloud) ListChangedBlocks(ctx context.Context, volumeID string, opts openstack.ListChangedBlocksOpts) (*openstack.ChangedBlocksPage, error) {
	c.calls = append(c.calls, listCall{volumeID: volumeID, opts: opts})
	if c.list != nil {
		return c.list(ctx, volumeID, opts)
	}
	return c.OpenStackMock.ListChangedBlocks(ctx, volumeID, opts)
}

func (c *cbtCloud) ChangedBlocksSupported(ctx context.Context) (bool, error) {
	if c.supported != nil {
		return c.supported(ctx)
	}
	return c.OpenStackMock.ChangedBlocksSupported(ctx)
}

type fakeAllocatedStream struct {
	grpc.ServerStream
	ctx     context.Context
	sent    []*csi.GetMetadataAllocatedResponse
	sendErr error
}

func (s *fakeAllocatedStream) Send(resp *csi.GetMetadataAllocatedResponse) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, resp)
	return nil
}

func (s *fakeAllocatedStream) Context() context.Context { return s.ctx }

type fakeDeltaStream struct {
	grpc.ServerStream
	ctx     context.Context
	sent    []*csi.GetMetadataDeltaResponse
	sendErr error
}

func (s *fakeDeltaStream) Send(resp *csi.GetMetadataDeltaResponse) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, resp)
	return nil
}

func (s *fakeDeltaStream) Context() context.Context { return s.ctx }

// pageResult is what one scripted ListChangedBlocks call answers.
type pageResult struct {
	page *openstack.ChangedBlocksPage
	err  error
}

func pageOf(nextMarker int64, blocks ...openstack.ChangedBlock) pageResult {
	return pageResult{page: &openstack.ChangedBlocksPage{Blocks: blocks, NextMarker: &nextMarker}}
}

func lastPage(blocks ...openstack.ChangedBlock) pageResult {
	return pageResult{page: &openstack.ChangedBlocksPage{Blocks: blocks}}
}

func pageErr(err error) pageResult {
	return pageResult{err: err}
}

func blk(offset, length int64) openstack.ChangedBlock {
	return openstack.ChangedBlock{Offset: offset, Length: length, Exists: true}
}

func hole(offset, length int64) openstack.ChangedBlock {
	return openstack.ChangedBlock{Offset: offset, Length: length}
}

// scripted answers ListChangedBlocks with the given results in order and
// fails the test when called more often than that.
func scripted(t *testing.T, results ...pageResult) listFunc {
	t.Helper()
	next := 0
	return func(context.Context, string, openstack.ListChangedBlocksOpts) (*openstack.ChangedBlocksPage, error) {
		if next >= len(results) {
			t.Fatalf("ListChangedBlocks called %d times, only %d answers scripted", next+1, len(results))
		}
		r := results[next]
		next++
		return r.page, r.err
	}
}

func cbtSnapshot(sizeGiB int, st string) *snapshots.Snapshot {
	return &snapshots.Snapshot{ID: cbtTargetID, VolumeID: FakeVolID, Size: sizeGiB, Status: st}
}

func snapshotLookup(snap *snapshots.Snapshot, err error) func(context.Context, string) (*snapshots.Snapshot, error) {
	return func(context.Context, string) (*snapshots.Snapshot, error) {
		return snap, err
	}
}

// newCBTCloud resolves every snapshot lookup to snap and answers changed
// block requests with list.
func newCBTCloud(snap *snapshots.Snapshot, list listFunc) *cbtCloud {
	return &cbtCloud{
		OpenStackMock: new(openstack.OpenStackMock),
		getSnapshot:   snapshotLookup(snap, nil),
		list:          list,
	}
}

func newCBTServer(cloud openstack.IOpenStack) *snapshotMetadataServer {
	d := NewDriver(&DriverOpts{Endpoint: FakeEndpoint, ClusterID: FakeCluster})
	return NewSnapshotMetadataServer(d, map[string]openstack.IOpenStack{"": cloud})
}

func cinderHTTPError(code int, body string) error {
	return gophercloud.ErrUnexpectedResponseCode{
		URL:      "http://cinder/v3/volumes/" + FakeVolID + "/action",
		Method:   http.MethodPost,
		Expected: []int{http.StatusOK},
		Actual:   code,
		Body:     []byte(body),
	}
}

func deltaRequest() *csi.GetMetadataDeltaRequest {
	return &csi.GetMetadataDeltaRequest{BaseSnapshotId: cbtBaseID, TargetSnapshotId: cbtTargetID}
}

func allocatedRequest() *csi.GetMetadataAllocatedRequest {
	return &csi.GetMetadataAllocatedRequest{SnapshotId: cbtTargetID}
}

func runDelta(ctx context.Context, cloud openstack.IOpenStack, req *csi.GetMetadataDeltaRequest) ([]*csi.GetMetadataDeltaResponse, error) {
	stream := &fakeDeltaStream{ctx: ctx}
	err := newCBTServer(cloud).GetMetadataDelta(req, stream)
	return stream.sent, err
}

func runAllocated(ctx context.Context, cloud openstack.IOpenStack, req *csi.GetMetadataAllocatedRequest) ([]*csi.GetMetadataAllocatedResponse, error) {
	stream := &fakeAllocatedStream{ctx: ctx}
	err := newCBTServer(cloud).GetMetadataAllocated(req, stream)
	return stream.sent, err
}

// metadataMessage is the RPC-independent view of one streamed response.
type metadataMessage struct {
	kind     csi.BlockMetadataType
	capacity int64
	blocks   []*csi.BlockMetadata
}

func deltaMessages(resps []*csi.GetMetadataDeltaResponse) []metadataMessage {
	msgs := make([]metadataMessage, 0, len(resps))
	for _, r := range resps {
		msgs = append(msgs, metadataMessage{kind: r.GetBlockMetadataType(), capacity: r.GetVolumeCapacityBytes(), blocks: r.GetBlockMetadata()})
	}
	return msgs
}

func allocatedMessages(resps []*csi.GetMetadataAllocatedResponse) []metadataMessage {
	msgs := make([]metadataMessage, 0, len(resps))
	for _, r := range resps {
		msgs = append(msgs, metadataMessage{kind: r.GetBlockMetadataType(), capacity: r.GetVolumeCapacityBytes(), blocks: r.GetBlockMetadata()})
	}
	return msgs
}

// checkStream asserts the invariants every SnapshotMetadata stream must hold
// and returns its extents in order as {offset, size} pairs.
func checkStream(t *testing.T, msgs []metadataMessage, capacity int64) [][2]int64 {
	t.Helper()
	if len(msgs) == 0 {
		t.Fatal("stream carried no message")
	}

	extents := [][2]int64{}
	var end int64
	for i, m := range msgs {
		if m.kind != csi.BlockMetadataType_VARIABLE_LENGTH {
			t.Errorf("message %d has block metadata type %s, want VARIABLE_LENGTH", i, m.kind)
		}
		if m.capacity != capacity {
			t.Errorf("message %d reports capacity %d, want %d", i, m.capacity, capacity)
		}
		for _, b := range m.blocks {
			if b.GetSizeBytes() <= 0 {
				t.Errorf("extent at %d has size %d", b.GetByteOffset(), b.GetSizeBytes())
			}
			if b.GetByteOffset() < end {
				t.Errorf("extent at %d overlaps the previous one ending at %d", b.GetByteOffset(), end)
			}
			end = b.GetByteOffset() + b.GetSizeBytes()
			if end > capacity {
				t.Errorf("extent at %d ends at %d, beyond capacity %d", b.GetByteOffset(), end, capacity)
			}
			extents = append(extents, [2]int64{b.GetByteOffset(), b.GetSizeBytes()})
		}
	}

	return extents
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if want == codes.OK {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != want {
		t.Fatalf("got error %v, want gRPC code %s", err, want)
	}
}

func assertExtents(t *testing.T, got, want [][2]int64) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("streamed extents %v, want %v", got, want)
	}
}

func shrinkWindow(t *testing.T, bytes int64) {
	t.Helper()
	old := changedBlocksWindowBytes
	changedBlocksWindowBytes = bytes
	t.Cleanup(func() { changedBlocksWindowBytes = old })
}

func TestSnapshotMetadataValidation(t *testing.T) {
	cases := []struct {
		name      string
		allocated *csi.GetMetadataAllocatedRequest
		delta     *csi.GetMetadataDeltaRequest
		want      codes.Code
	}{
		{
			name:      "allocated undefined cloud",
			allocated: &csi.GetMetadataAllocatedRequest{SnapshotId: cbtTargetID, Secrets: map[string]string{"cloud": "nowhere"}},
			want:      codes.InvalidArgument,
		},
		{
			name:  "delta undefined cloud",
			delta: &csi.GetMetadataDeltaRequest{BaseSnapshotId: cbtBaseID, TargetSnapshotId: cbtTargetID, Secrets: map[string]string{"cloud": "nowhere"}},
			want:  codes.InvalidArgument,
		},
		{
			name:      "allocated empty snapshot id",
			allocated: &csi.GetMetadataAllocatedRequest{},
			want:      codes.InvalidArgument,
		},
		{
			name:  "delta empty target",
			delta: &csi.GetMetadataDeltaRequest{BaseSnapshotId: cbtBaseID},
			want:  codes.InvalidArgument,
		},
		{
			name:  "delta empty base",
			delta: &csi.GetMetadataDeltaRequest{TargetSnapshotId: cbtTargetID},
			want:  codes.InvalidArgument,
		},
		{
			name:  "delta base equals target",
			delta: &csi.GetMetadataDeltaRequest{BaseSnapshotId: cbtTargetID, TargetSnapshotId: cbtTargetID},
			want:  codes.InvalidArgument,
		},
		{
			name:      "allocated negative offset",
			allocated: &csi.GetMetadataAllocatedRequest{SnapshotId: cbtTargetID, StartingOffset: -1},
			want:      codes.OutOfRange,
		},
		{
			name:  "delta negative offset",
			delta: &csi.GetMetadataDeltaRequest{BaseSnapshotId: cbtBaseID, TargetSnapshotId: cbtTargetID, StartingOffset: -1},
			want:  codes.OutOfRange,
		},
		{
			name:      "allocated negative max results",
			allocated: &csi.GetMetadataAllocatedRequest{SnapshotId: cbtTargetID, MaxResults: -1},
			want:      codes.InvalidArgument,
		},
		{
			name:  "delta negative max results",
			delta: &csi.GetMetadataDeltaRequest{BaseSnapshotId: cbtBaseID, TargetSnapshotId: cbtTargetID, MaxResults: -1},
			want:  codes.InvalidArgument,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &cbtCloud{
				OpenStackMock: new(openstack.OpenStackMock),
				getSnapshot: func(context.Context, string) (*snapshots.Snapshot, error) {
					t.Fatal("GetSnapshotByID must not be called for an invalid request")
					return nil, nil
				},
				list: scripted(t),
			}

			var err error
			var sent int
			if tc.allocated != nil {
				var resps []*csi.GetMetadataAllocatedResponse
				resps, err = runAllocated(context.Background(), cloud, tc.allocated)
				sent = len(resps)
			} else {
				var resps []*csi.GetMetadataDeltaResponse
				resps, err = runDelta(context.Background(), cloud, tc.delta)
				sent = len(resps)
			}

			assertCode(t, err, tc.want)
			if sent != 0 {
				t.Errorf("%d messages were sent before the request was rejected", sent)
			}
		})
	}
}

func TestSnapshotMetadataSnapshotLookup(t *testing.T) {
	cases := []struct {
		name      string
		snap      *snapshots.Snapshot
		lookupErr error
		offset    int64
		want      codes.Code
	}{
		{
			name:      "missing snapshot",
			lookupErr: cinderHTTPError(http.StatusNotFound, `{"itemNotFound": {"code": 404, "message": "Snapshot could not be found."}}`),
			want:      codes.NotFound,
		},
		{
			name:      "missing snapshot without body",
			lookupErr: cinderHTTPError(http.StatusNotFound, ""),
			want:      codes.NotFound,
		},
		{
			name:      "lookup service unavailable",
			lookupErr: cinderHTTPError(http.StatusServiceUnavailable, ""),
			want:      codes.Unavailable,
		},
		{
			name:      "lookup transport error",
			lookupErr: errors.New("dial tcp: connection refused"),
			want:      codes.Unavailable,
		},
		{
			name:      "lookup server error",
			lookupErr: cinderHTTPError(http.StatusInternalServerError, ""),
			want:      codes.Internal,
		},
		{name: "creating", snap: cbtSnapshot(1, "creating"), want: codes.Unavailable},
		{name: "backing-up", snap: cbtSnapshot(1, "backing-up"), want: codes.Unavailable},
		{name: "error", snap: cbtSnapshot(1, "error"), want: codes.FailedPrecondition},
		{name: "error_deleting", snap: cbtSnapshot(1, "error_deleting"), want: codes.FailedPrecondition},
		{name: "deleting", snap: cbtSnapshot(1, "deleting"), want: codes.NotFound},
		{name: "deleted", snap: cbtSnapshot(1, "deleted"), want: codes.NotFound},
		{name: "zero size", snap: cbtSnapshot(0, "available"), want: codes.Internal},
		{
			name: "no volume id",
			snap: &snapshots.Snapshot{ID: cbtTargetID, Size: 1, Status: "available"},
			want: codes.Internal,
		},
		{name: "offset at capacity", snap: cbtSnapshot(1, "available"), offset: cbtGiB, want: codes.OutOfRange},
		{name: "offset beyond capacity", snap: cbtSnapshot(1, "available"), offset: 2 * cbtGiB, want: codes.OutOfRange},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &cbtCloud{
				OpenStackMock: new(openstack.OpenStackMock),
				getSnapshot:   snapshotLookup(tc.snap, tc.lookupErr),
				list:          scripted(t),
			}

			req := deltaRequest()
			req.StartingOffset = tc.offset
			resps, err := runDelta(context.Background(), cloud, req)
			assertCode(t, err, tc.want)
			if len(resps) != 0 {
				t.Errorf("%d messages were sent before the request was rejected", len(resps))
			}

			areq := allocatedRequest()
			areq.StartingOffset = tc.offset
			aresps, err := runAllocated(context.Background(), cloud, areq)
			assertCode(t, err, tc.want)
			if len(aresps) != 0 {
				t.Errorf("%d messages were sent before the request was rejected", len(aresps))
			}
		})
	}
}

func TestGetMetadataAllocatedRequestsAllocationMap(t *testing.T) {
	cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(blk(0, cbtBlock))))

	resps, err := runAllocated(context.Background(), cloud, allocatedRequest())
	assertCode(t, err, codes.OK)

	want := []listCall{{volumeID: FakeVolID, opts: openstack.ListChangedBlocksOpts{TargetSnapshotID: cbtTargetID, Length: cbtGiB}}}
	if !reflect.DeepEqual(cloud.calls, want) {
		t.Errorf("changed blocks requests %+v, want %+v", cloud.calls, want)
	}

	assertExtents(t, checkStream(t, allocatedMessages(resps), cbtGiB), [][2]int64{{0, cbtBlock}})
}

func TestGetMetadataDeltaRequestsDelta(t *testing.T) {
	cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(blk(0, cbtBlock))))

	resps, err := runDelta(context.Background(), cloud, deltaRequest())
	assertCode(t, err, codes.OK)

	want := []listCall{{volumeID: FakeVolID, opts: openstack.ListChangedBlocksOpts{BaseSnapshotID: cbtBaseID, TargetSnapshotID: cbtTargetID, Length: cbtGiB}}}
	if !reflect.DeepEqual(cloud.calls, want) {
		t.Errorf("changed blocks requests %+v, want %+v", cloud.calls, want)
	}

	assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{0, cbtBlock}})
}

func TestSnapshotMetadataRespectsStartingOffset(t *testing.T) {
	// An extent that straddles the starting offset may be reported as is;
	// the request to Cinder starts at the offset.
	cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(blk(cbtBlock, 2*cbtBlock))))

	req := deltaRequest()
	req.StartingOffset = 2 * cbtBlock
	resps, err := runDelta(context.Background(), cloud, req)
	assertCode(t, err, codes.OK)

	if got := cloud.calls[0].opts; got.Offset != 2*cbtBlock || got.Length != cbtGiB-2*cbtBlock {
		t.Errorf("window requested at offset %d length %d, want offset %d length %d", got.Offset, got.Length, 2*cbtBlock, cbtGiB-2*cbtBlock)
	}

	assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{cbtBlock, 2 * cbtBlock}})
}

func TestSnapshotMetadataFollowsMarkers(t *testing.T) {
	cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
		pageOf(cbtBlock, blk(0, cbtBlock)),
		pageOf(3*cbtBlock, blk(2*cbtBlock, cbtBlock)),
		lastPage(blk(1<<20, cbtBlock)),
	))

	resps, err := runDelta(context.Background(), cloud, deltaRequest())
	assertCode(t, err, codes.OK)

	wantMarkers := []int64{0, cbtBlock, 3 * cbtBlock}
	if len(cloud.calls) != len(wantMarkers) {
		t.Fatalf("%d changed blocks requests, want %d", len(cloud.calls), len(wantMarkers))
	}
	for i, call := range cloud.calls {
		if call.opts.Marker != wantMarkers[i] {
			t.Errorf("request %d used marker %d, want %d", i, call.opts.Marker, wantMarkers[i])
		}
		if call.opts.Offset != 0 || call.opts.Length != cbtGiB {
			t.Errorf("request %d used window offset %d length %d, want the whole 0-%d volume", i, call.opts.Offset, call.opts.Length, cbtGiB)
		}
		if call.opts.Limit != 0 {
			t.Errorf("request %d sent limit %d, want none", i, call.opts.Limit)
		}
	}

	msgs := deltaMessages(resps)
	if len(msgs) != 1 {
		t.Errorf("%d messages, want the three extents in one", len(msgs))
	}
	assertExtents(t, checkStream(t, msgs, cbtGiB), [][2]int64{{0, cbtBlock}, {2 * cbtBlock, cbtBlock}, {1 << 20, cbtBlock}})
}

func TestSnapshotMetadataStopsAtWindowEnd(t *testing.T) {
	// A marker at or past the end of the window means the window is
	// exhausted even though Cinder did not send null.
	cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, pageOf(cbtGiB, blk(0, cbtBlock))))

	resps, err := runDelta(context.Background(), cloud, deltaRequest())
	assertCode(t, err, codes.OK)
	if len(cloud.calls) != 1 {
		t.Errorf("%d changed blocks requests, want 1", len(cloud.calls))
	}
	assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{0, cbtBlock}})
}

func TestSnapshotMetadataWalksWindows(t *testing.T) {
	const window = int64(256) << 20
	defaultWindow := changedBlocksWindowBytes

	windows := func(t *testing.T, calls []listCall) [][2]int64 {
		t.Helper()
		out := make([][2]int64, 0, len(calls))
		for _, c := range calls {
			if c.opts.Marker != 0 {
				t.Errorf("window at %d was requested with marker %d", c.opts.Offset, c.opts.Marker)
			}
			out = append(out, [2]int64{c.opts.Offset, c.opts.Length})
		}
		return out
	}

	t.Run("from the start", func(t *testing.T) {
		shrinkWindow(t, window)
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
			lastPage(blk(0, cbtBlock)),
			lastPage(),
			lastPage(blk(600<<20, cbtBlock)),
			lastPage(),
		))

		resps, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)

		want := [][2]int64{{0, window}, {window, window}, {2 * window, window}, {3 * window, window}}
		if got := windows(t, cloud.calls); !slices.Equal(got, want) {
			t.Errorf("windows requested %v, want %v", got, want)
		}
		assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{0, cbtBlock}, {600 << 20, cbtBlock}})
	})

	t.Run("from an offset", func(t *testing.T) {
		shrinkWindow(t, window)
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(), lastPage(), lastPage(), lastPage()))

		start := int64(100) << 20
		req := deltaRequest()
		req.StartingOffset = start
		_, err := runDelta(context.Background(), cloud, req)
		assertCode(t, err, codes.OK)

		want := [][2]int64{{start, window}, {start + window, window}, {start + 2*window, window}, {start + 3*window, cbtGiB - start - 3*window}}
		if got := windows(t, cloud.calls); !slices.Equal(got, want) {
			t.Errorf("windows requested %v, want %v", got, want)
		}
	})

	t.Run("extent ending at a window boundary", func(t *testing.T) {
		shrinkWindow(t, window)
		// Cinder clips a straddling extent to each window, so the two
		// halves arrive in consecutive windows and must stay adjacent.
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
			lastPage(blk(window-cbtBlock, cbtBlock)),
			lastPage(blk(window, cbtBlock)),
			lastPage(),
			lastPage(),
		))

		resps, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)
		assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{window - cbtBlock, cbtBlock}, {window, cbtBlock}})
	})

	t.Run("extent re-reported across windows", func(t *testing.T) {
		shrinkWindow(t, window)
		// A backend that ignores the window start would report the tail of
		// the previous window again; only the unseen part may be streamed.
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
			lastPage(blk(window-2*cbtBlock, 2*cbtBlock)),
			lastPage(blk(window-cbtBlock, 2*cbtBlock)),
			lastPage(),
			lastPage(),
		))

		resps, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)
		assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{window - 2*cbtBlock, 2 * cbtBlock}, {window, cbtBlock}})
	})

	if changedBlocksWindowBytes != defaultWindow {
		t.Errorf("changedBlocksWindowBytes is %d after the subtests, want it restored to %d", changedBlocksWindowBytes, defaultWindow)
	}
}

func TestSnapshotMetadataBaseLookup(t *testing.T) {
	target := cbtSnapshot(1, "available")
	baseSnapshot := func(volumeID, st string) *snapshots.Snapshot {
		return &snapshots.Snapshot{ID: cbtBaseID, VolumeID: volumeID, Size: 1, Status: st}
	}

	cases := []struct {
		name    string
		base    *snapshots.Snapshot
		baseErr error
		want    codes.Code
	}{
		{
			name:    "missing base",
			baseErr: cinderHTTPError(http.StatusNotFound, `{"itemNotFound": {"code": 404, "message": "Snapshot `+cbtBaseID+` could not be found."}}`),
			want:    codes.NotFound,
		},
		{name: "base creating", base: baseSnapshot(FakeVolID, "creating"), want: codes.Unavailable},
		{name: "base error", base: baseSnapshot(FakeVolID, "error"), want: codes.FailedPrecondition},
		{name: "base deleting", base: baseSnapshot(FakeVolID, "deleting"), want: codes.NotFound},
		{name: "base of another volume", base: baseSnapshot("261a8b81-3660-43e5-bab8-6470b65ee4e9", "available"), want: codes.InvalidArgument},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloud := &cbtCloud{
				OpenStackMock: new(openstack.OpenStackMock),
				list:          scripted(t),
				getSnapshot: func(_ context.Context, id string) (*snapshots.Snapshot, error) {
					if id == cbtBaseID {
						return tc.base, tc.baseErr
					}
					return target, nil
				},
			}

			resps, err := runDelta(context.Background(), cloud, deltaRequest())
			assertCode(t, err, tc.want)
			if len(resps) != 0 {
				t.Errorf("%d messages were sent before the request was rejected", len(resps))
			}
		})
	}

	t.Run("delta resolves target then base", func(t *testing.T) {
		var lookups []string
		cloud := &cbtCloud{
			OpenStackMock: new(openstack.OpenStackMock),
			list:          scripted(t, lastPage()),
			getSnapshot: func(_ context.Context, id string) (*snapshots.Snapshot, error) {
				lookups = append(lookups, id)
				if id == cbtBaseID {
					return baseSnapshot(FakeVolID, "available"), nil
				}
				return target, nil
			},
		}

		_, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)
		if want := []string{cbtTargetID, cbtBaseID}; !slices.Equal(lookups, want) {
			t.Errorf("snapshots looked up %v, want %v", lookups, want)
		}
	})

	t.Run("allocation map resolves only the snapshot", func(t *testing.T) {
		var lookups []string
		cloud := &cbtCloud{
			OpenStackMock: new(openstack.OpenStackMock),
			list:          scripted(t, lastPage()),
			getSnapshot: func(_ context.Context, id string) (*snapshots.Snapshot, error) {
				lookups = append(lookups, id)
				return target, nil
			},
		}

		_, err := runAllocated(context.Background(), cloud, allocatedRequest())
		assertCode(t, err, codes.OK)
		if want := []string{cbtTargetID}; !slices.Equal(lookups, want) {
			t.Errorf("snapshots looked up %v, want %v", lookups, want)
		}
	})
}

func TestNormaliseChangedBlocks(t *testing.T) {
	cases := []struct {
		name   string
		blocks []openstack.ChangedBlock
		lo, hi int64
		want   [][2]int64
	}{
		{
			name:   "drops unallocated",
			blocks: []openstack.ChangedBlock{hole(0, cbtBlock), blk(cbtBlock, cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{cbtBlock, cbtBlock}},
		},
		{
			name:   "drops empty and negative lengths",
			blocks: []openstack.ChangedBlock{blk(0, 0), blk(cbtBlock, -1)},
			hi:     cbtGiB,
			want:   [][2]int64{},
		},
		{
			name:   "merges overlap",
			blocks: []openstack.ChangedBlock{blk(0, 2*cbtBlock), blk(cbtBlock, 2*cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{0, 3 * cbtBlock}},
		},
		{
			name:   "merges adjacency",
			blocks: []openstack.ChangedBlock{blk(0, cbtBlock), blk(cbtBlock, cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{0, 2 * cbtBlock}},
		},
		{
			name:   "absorbs a contained extent",
			blocks: []openstack.ChangedBlock{blk(0, 4*cbtBlock), blk(cbtBlock, cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{0, 4 * cbtBlock}},
		},
		{
			name:   "keeps a gap",
			blocks: []openstack.ChangedBlock{blk(0, cbtBlock), blk(2*cbtBlock, cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{0, cbtBlock}, {2 * cbtBlock, cbtBlock}},
		},
		{
			name:   "sorts by offset",
			blocks: []openstack.ChangedBlock{blk(2*cbtBlock, cbtBlock), blk(0, cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{0, cbtBlock}, {2 * cbtBlock, cbtBlock}},
		},
		{
			name:   "clips the end to capacity",
			blocks: []openstack.ChangedBlock{blk(cbtGiB-cbtBlock, 2*cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{cbtGiB - cbtBlock, cbtBlock}},
		},
		{
			name:   "drops extents beyond capacity",
			blocks: []openstack.ChangedBlock{blk(cbtGiB, cbtBlock), blk(2*cbtGiB, cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{},
		},
		{
			name:   "clips the start to the stream end",
			blocks: []openstack.ChangedBlock{blk(0, 2*cbtBlock)},
			lo:     cbtBlock,
			hi:     cbtGiB,
			want:   [][2]int64{{cbtBlock, cbtBlock}},
		},
		{
			name:   "drops extents before the stream end",
			blocks: []openstack.ChangedBlock{blk(0, cbtBlock)},
			lo:     cbtBlock,
			hi:     cbtGiB,
			want:   [][2]int64{},
		},
		{
			name:   "clips a negative offset",
			blocks: []openstack.ChangedBlock{blk(-cbtBlock, 2*cbtBlock)},
			hi:     cbtGiB,
			want:   [][2]int64{{0, cbtBlock}},
		},
		{
			name:   "survives an absurd length",
			blocks: []openstack.ChangedBlock{blk(cbtGiB-cbtBlock, math.MaxInt64)},
			hi:     cbtGiB,
			want:   [][2]int64{{cbtGiB - cbtBlock, cbtBlock}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := [][2]int64{}
			for _, b := range normaliseChangedBlocks(tc.blocks, tc.lo, tc.hi) {
				got = append(got, [2]int64{b.GetByteOffset(), b.GetSizeBytes()})
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("normalised to %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSnapshotMetadataNormalisesStream(t *testing.T) {
	t.Run("within a page", func(t *testing.T) {
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(
			hole(0, cbtBlock),
			blk(2*cbtBlock, cbtBlock),
			blk(cbtBlock, cbtBlock),
			blk(20000, 1000),
			blk(20500, 1000),
			blk(cbtGiB-100, 1000),
			blk(2*cbtGiB, 10),
		)))

		resps, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)
		assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{cbtBlock, 2 * cbtBlock}, {20000, 1500}, {cbtGiB - 100, 100}})
	})

	t.Run("across pages", func(t *testing.T) {
		// A page whose first extent overlaps the previous page's last one
		// must not make the stream go backwards.
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
			pageOf(2*cbtBlock, blk(0, 2*cbtBlock)),
			lastPage(blk(cbtBlock, 2*cbtBlock)),
		))

		resps, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)
		assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{0, 2 * cbtBlock}, {2 * cbtBlock, cbtBlock}})
	})
}

func TestSnapshotMetadataBatching(t *testing.T) {
	// extents builds n non-adjacent extents starting at from.
	extents := func(from int64, n int) []openstack.ChangedBlock {
		out := make([]openstack.ChangedBlock, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, blk(from+int64(i)*2*cbtBlock, cbtBlock))
		}
		return out
	}
	sizes := func(msgs []metadataMessage) []int {
		out := make([]int, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, len(m.blocks))
		}
		return out
	}

	t.Run("max results with remainder", func(t *testing.T) {
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(extents(0, 7)...)))

		req := deltaRequest()
		req.MaxResults = 3
		resps, err := runDelta(context.Background(), cloud, req)
		assertCode(t, err, codes.OK)

		msgs := deltaMessages(resps)
		if got := sizes(msgs); !slices.Equal(got, []int{3, 3, 1}) {
			t.Errorf("message sizes %v, want [3 3 1]", got)
		}
		if got := checkStream(t, msgs, cbtGiB); len(got) != 7 {
			t.Errorf("%d extents streamed, want 7", len(got))
		}
	})

	t.Run("default batch across pages", func(t *testing.T) {
		first := extents(0, 515)
		second := extents(first[len(first)-1].Offset+2*cbtBlock, 515)
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
			pageOf(second[0].Offset, first...),
			lastPage(second...),
		))

		resps, err := runAllocated(context.Background(), cloud, allocatedRequest())
		assertCode(t, err, codes.OK)

		msgs := allocatedMessages(resps)
		if got := sizes(msgs); !slices.Equal(got, []int{defaultMetadataBatchSize, defaultMetadataBatchSize, 6}) {
			t.Errorf("message sizes %v, want [%d %d 6]", got, defaultMetadataBatchSize, defaultMetadataBatchSize)
		}
		if got := checkStream(t, msgs, cbtGiB); len(got) != 1030 {
			t.Errorf("%d extents streamed, want 1030", len(got))
		}
	})

	t.Run("one message for few extents", func(t *testing.T) {
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(extents(0, 5)...)))

		resps, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)

		if got := sizes(deltaMessages(resps)); !slices.Equal(got, []int{5}) {
			t.Errorf("message sizes %v, want [5]", got)
		}
	})
}

func TestSnapshotMetadataEmptyResult(t *testing.T) {
	t.Run("delta", func(t *testing.T) {
		cloud := newCBTCloud(cbtSnapshot(2, "available"), scripted(t, lastPage()))

		resps, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.OK)

		msgs := deltaMessages(resps)
		if len(msgs) != 1 || len(msgs[0].blocks) != 0 {
			t.Fatalf("got %d messages with blocks %v, want exactly one empty message", len(msgs), msgs)
		}
		assertExtents(t, checkStream(t, msgs, 2*cbtGiB), [][2]int64{})
	})

	t.Run("allocated", func(t *testing.T) {
		cloud := newCBTCloud(cbtSnapshot(2, "available"), scripted(t, lastPage()))

		resps, err := runAllocated(context.Background(), cloud, allocatedRequest())
		assertCode(t, err, codes.OK)

		msgs := allocatedMessages(resps)
		if len(msgs) != 1 || len(msgs[0].blocks) != 0 {
			t.Fatalf("got %d messages with blocks %v, want exactly one empty message", len(msgs), msgs)
		}
		assertExtents(t, checkStream(t, msgs, 2*cbtGiB), [][2]int64{})
	})
}

func TestSnapshotMetadataCinderErrors(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    codes.Code
		wantMsg string
	}{
		{
			name:    "400 bad request",
			err:     cinderHTTPError(http.StatusBadRequest, `{"badRequest": {"code": 400, "message": "Snapshot `+cbtBaseID+` must be available."}}`),
			want:    codes.InvalidArgument,
			wantMsg: "Snapshot " + cbtBaseID + " must be available.",
		},
		{
			name:    "400 no such action",
			err:     cinderHTTPError(http.StatusBadRequest, `{"badRequest": {"code": 400, "message": "There is no such action: os-list_changed_blocks. Verify the request body and Content-Type header and try again."}}`),
			want:    codes.FailedPrecondition,
			wantMsg: "no such action",
		},
		{
			name:    "404 action absent at this microversion",
			err:     cinderHTTPError(http.StatusNotFound, `{"itemNotFound": {"code": 404, "message": "The resource could not be found."}}`),
			want:    codes.FailedPrecondition,
			wantMsg: "microversion " + openstack.ChangedBlocksMicroversion,
		},
		{
			name:    "microversion ignored by configuration",
			err:     openstack.ErrChangedBlocksUnsupported,
			want:    codes.FailedPrecondition,
			wantMsg: "ignore-volume-microversion",
		},
		{
			name:    "401 unauthorized",
			err:     cinderHTTPError(http.StatusUnauthorized, `{"error": {"code": 401, "message": "The request you have made requires authentication."}}`),
			want:    codes.Unauthenticated,
			wantMsg: "requires authentication",
		},
		{
			name:    "403 forbidden",
			err:     cinderHTTPError(http.StatusForbidden, `{"forbidden": {"code": 403, "message": "Policy doesn't allow volume:list_changed_blocks to be performed."}}`),
			want:    codes.PermissionDenied,
			wantMsg: "Policy doesn't allow",
		},
		{
			name:    "404 with body",
			err:     cinderHTTPError(http.StatusNotFound, `{"itemNotFound": {"code": 404, "message": "Snapshot `+cbtBaseID+` could not be found."}}`),
			want:    codes.NotFound,
			wantMsg: "could not be found",
		},
		{
			name:    "404 without body",
			err:     cinderHTTPError(http.StatusNotFound, "  \n"),
			want:    codes.FailedPrecondition,
			wantMsg: "os-list_changed_blocks",
		},
		{
			name:    "406 microversion",
			err:     cinderHTTPError(http.StatusNotAcceptable, `{"computeFault": {"code": 406, "message": "Version 3.72 is not supported by the API. Minimum is 3.0 and maximum is 3.71."}}`),
			want:    codes.FailedPrecondition,
			wantMsg: "maximum is 3.71",
		},
		{
			name:    "409 conflict",
			err:     cinderHTTPError(http.StatusConflict, `{"conflictingRequest": {"code": 409, "message": "Backend does not support changed block tracking."}}`),
			want:    codes.FailedPrecondition,
			wantMsg: "changed block tracking",
		},
		{
			name:    "500 server error",
			err:     cinderHTTPError(http.StatusInternalServerError, `{"computeFault": {"code": 500, "message": "The server has either erred or is incapable of performing the requested operation."}}`),
			want:    codes.Internal,
			wantMsg: "HTTP 500",
		},
		{
			name:    "502 bad gateway",
			err:     cinderHTTPError(http.StatusBadGateway, "<html><body>Bad Gateway</body></html>"),
			want:    codes.Unavailable,
			wantMsg: "Bad Gateway",
		},
		{
			name:    "503 service unavailable",
			err:     cinderHTTPError(http.StatusServiceUnavailable, `{"serviceUnavailable": {"code": 503, "message": "The volume service is unavailable."}}`),
			want:    codes.Unavailable,
			wantMsg: "volume service is unavailable",
		},
		{
			name:    "504 gateway timeout without body",
			err:     cinderHTTPError(http.StatusGatewayTimeout, ""),
			want:    codes.Unavailable,
			wantMsg: http.StatusText(http.StatusGatewayTimeout),
		},
		{
			name:    "transport error",
			err:     errors.New("dial tcp 10.0.0.1:8776: connect: connection refused"),
			want:    codes.Unavailable,
			wantMsg: "connection refused",
		},
		{
			name: "cancelled request",
			err:  fmt.Errorf("Post %q: %w", "http://cinder/v3/volumes/x/action", context.Canceled),
			want: codes.Canceled,
		},
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			want: codes.DeadlineExceeded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, pageErr(tc.err)))

			resps, err := runDelta(context.Background(), cloud, deltaRequest())
			assertCode(t, err, tc.want)
			if msg := status.Convert(err).Message(); !strings.Contains(msg, tc.wantMsg) {
				t.Errorf("status message %q does not contain %q", msg, tc.wantMsg)
			}
			if len(resps) != 0 {
				t.Errorf("%d messages were sent before the error", len(resps))
			}
		})
	}
}

func TestCinderErrorMessage(t *testing.T) {
	long := strings.Repeat("x", 2*cinderErrorBodyLimit)
	multibyte := "a" + strings.Repeat("€", cinderErrorBodyLimit)

	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "cinder envelope", body: `{"badRequest": {"code": 400, "message": "Invalid snapshot."}}`, want: "Invalid snapshot."},
		{name: "first message wins", body: `{"b": {"message": "second"}, "a": {"message": "first"}}`, want: "first"},
		{name: "envelope without message", body: `{"badRequest": {"code": 400}}`, want: `{"badRequest": {"code": 400}}`},
		{name: "plain text", body: "  upstream timed out  ", want: "upstream timed out"},
		{name: "empty", body: "", want: ""},
		{name: "bounded", body: long, want: long[:cinderErrorBodyLimit] + "..."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cinderErrorMessage([]byte(tc.body)); got != tc.want {
				t.Errorf("cinderErrorMessage(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}

	got := cinderErrorMessage([]byte(multibyte))
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "...") || len(got) > cinderErrorBodyLimit+3 {
		t.Errorf("multibyte body was cut to %q, want a bounded valid UTF-8 prefix ending in ...", got)
	}
}

func TestSnapshotMetadataRetriesSmallerPages(t *testing.T) {
	tooLarge := cinderHTTPError(http.StatusRequestEntityTooLarge, `{"requestEntityTooLarge": {"code": 413, "message": "Too many changed blocks."}}`)
	cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
		pageOf(cbtBlock, blk(0, cbtBlock)),
		pageErr(tooLarge),
		pageErr(tooLarge),
		lastPage(blk(2*cbtBlock, cbtBlock)),
	))

	resps, err := runDelta(context.Background(), cloud, deltaRequest())
	assertCode(t, err, codes.OK)

	var limits, markers []int64
	for _, c := range cloud.calls {
		limits = append(limits, int64(c.opts.Limit))
		markers = append(markers, c.opts.Marker)
	}
	if want := []int64{0, 0, 500, 250}; !slices.Equal(limits, want) {
		t.Errorf("limits sent %v, want %v", limits, want)
	}
	if want := []int64{0, cbtBlock, cbtBlock, cbtBlock}; !slices.Equal(markers, want) {
		t.Errorf("markers sent %v, want the retries to keep %v", markers, want)
	}

	assertExtents(t, checkStream(t, deltaMessages(resps), cbtGiB), [][2]int64{{0, cbtBlock}, {2 * cbtBlock, cbtBlock}})
}

func TestSnapshotMetadataGivesUpOnPersistent413(t *testing.T) {
	tooLarge := cinderHTTPError(http.StatusRequestEntityTooLarge, "")
	cloud := newCBTCloud(cbtSnapshot(1, "available"), func(context.Context, string, openstack.ListChangedBlocksOpts) (*openstack.ChangedBlocksPage, error) {
		return nil, tooLarge
	})

	resps, err := runDelta(context.Background(), cloud, deltaRequest())
	assertCode(t, err, codes.Internal)

	var limits []int
	for _, c := range cloud.calls {
		limits = append(limits, c.opts.Limit)
	}
	if want := []int{0, 500, 250, 125, 62, 31, 15, 7, 3, 1}; !slices.Equal(limits, want) {
		t.Errorf("limits sent %v, want %v", limits, want)
	}
	if len(resps) != 0 {
		t.Errorf("%d messages were sent before giving up", len(resps))
	}
}

func TestSnapshotMetadataCancelled(t *testing.T) {
	t.Run("before the first request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t))
		resps, err := runDelta(ctx, cloud, deltaRequest())
		assertCode(t, err, codes.Canceled)
		if len(resps) != 0 {
			t.Errorf("%d messages were sent on a cancelled stream", len(resps))
		}
	})

	t.Run("between pages", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cloud := newCBTCloud(cbtSnapshot(1, "available"), func(context.Context, string, openstack.ListChangedBlocksOpts) (*openstack.ChangedBlocksPage, error) {
			cancel()
			return pageOf(cbtBlock, blk(0, cbtBlock)).page, nil
		})

		_, err := runAllocated(ctx, cloud, allocatedRequest())
		assertCode(t, err, codes.Canceled)
		if len(cloud.calls) != 1 {
			t.Errorf("%d changed blocks requests after cancellation, want 1", len(cloud.calls))
		}
	})
}

func TestSnapshotMetadataMarkerMustAdvance(t *testing.T) {
	t.Run("repeated marker", func(t *testing.T) {
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t,
			pageOf(cbtBlock, blk(0, cbtBlock)),
			pageOf(cbtBlock, blk(cbtBlock, cbtBlock)),
		))

		_, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.Internal)
		if len(cloud.calls) != 2 {
			t.Errorf("%d changed blocks requests, want 2", len(cloud.calls))
		}
	})

	t.Run("zero marker", func(t *testing.T) {
		cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, pageOf(0, blk(0, cbtBlock))))

		_, err := runDelta(context.Background(), cloud, deltaRequest())
		assertCode(t, err, codes.Internal)
	})
}

func TestSnapshotMetadataSendFailure(t *testing.T) {
	cloud := newCBTCloud(cbtSnapshot(1, "available"), scripted(t, lastPage(blk(0, cbtBlock))))

	stream := &fakeDeltaStream{ctx: context.Background(), sendErr: errors.New("broken pipe")}
	err := newCBTServer(cloud).GetMetadataDelta(deltaRequest(), stream)
	assertCode(t, err, codes.Internal)
}
