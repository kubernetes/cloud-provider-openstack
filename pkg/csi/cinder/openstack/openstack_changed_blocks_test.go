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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
)

const (
	testChangedBlocksVolume = "6d3a0c7b-5d3f-4f8e-9f4e-0a1b2c3d4e5f"
	testChangedBlocksBase   = "0f7c1a2b-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	testChangedBlocksTarget = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
)

// recordedAction holds the os-list_changed_blocks object the fake Cinder
// received, as raw JSON per key so a test can tell an omitted key from a
// zero value.
type recordedAction struct {
	fields map[string]json.RawMessage
}

// newChangedBlocksServer serves one canned response to the volume action and
// checks the parts of the request that the Cinder contract fixes.
func newChangedBlocksServer(t *testing.T, status int, body string) (*httptest.Server, *recordedAction) {
	t.Helper()
	rec := &recordedAction{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		wantPath := "/volumes/" + testChangedBlocksVolume + "/action"
		if !strings.HasSuffix(r.URL.Path, wantPath) {
			t.Errorf("path = %s, want suffix %s", r.URL.Path, wantPath)
		}
		wantVersion := "volume " + ChangedBlocksMicroversion
		if got := r.Header.Get("OpenStack-API-Version"); got != wantVersion {
			t.Errorf("OpenStack-API-Version = %q, want %q", got, wantVersion)
		}

		var envelope map[string]map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		if len(envelope) != 1 {
			t.Errorf("request body has %d top-level keys, want 1", len(envelope))
		}
		action, ok := envelope["os-list_changed_blocks"]
		if !ok {
			t.Errorf("request body lacks the os-list_changed_blocks key")
		}
		rec.fields = action

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func newTestOpenStack(srv *httptest.Server, bsOpts BlockStorageOpts) *OpenStack {
	return newTestOpenStackAt(srv, "/", bsOpts)
}

// newTestOpenStackAt builds a client whose catalog endpoint is the given path
// below the test server, as the Keystone catalog would hand it out.
func newTestOpenStackAt(srv *httptest.Server, path string, bsOpts BlockStorageOpts) *OpenStack {
	return &OpenStack{
		blockstorage: &gophercloud.ServiceClient{
			ProviderClient: &gophercloud.ProviderClient{HTTPClient: *srv.Client()},
			Endpoint:       srv.URL + path,
			ResourceBase:   srv.URL + path,
			// NewBlockStorageV3 sets this type and gophercloud derives the
			// microversion header from it.
			Type: "block-storage",
		},
		bsOpts: bsOpts,
	}
}

func TestListChangedBlocksAllocationMap(t *testing.T) {
	srv, rec := newChangedBlocksServer(t, http.StatusOK, `{"changed_blocks":[{"offset":0,"length":4096,"exists":true},{"offset":8192,"length":4096,"exists":true}],"changed_blocks_next_marker":12288}`)
	cloud := newTestOpenStack(srv, BlockStorageOpts{})

	page, err := cloud.ListChangedBlocks(context.Background(), testChangedBlocksVolume, ListChangedBlocksOpts{
		TargetSnapshotID: testChangedBlocksTarget,
	})
	if err != nil {
		t.Fatalf("ListChangedBlocks: %v", err)
	}

	if got := string(rec.fields["target_snapshot_id"]); got != `"`+testChangedBlocksTarget+`"` {
		t.Errorf("target_snapshot_id = %s, want %q", got, testChangedBlocksTarget)
	}
	for _, key := range []string{"base_snapshot_id", "offset", "length", "marker", "limit"} {
		if _, ok := rec.fields[key]; ok {
			t.Errorf("request carries %s, want it omitted", key)
		}
	}
	if len(rec.fields) != 1 {
		t.Errorf("request has %d fields, want only target_snapshot_id: %v", len(rec.fields), rec.fields)
	}

	want := []ChangedBlock{
		{Offset: 0, Length: 4096, Exists: true},
		{Offset: 8192, Length: 4096, Exists: true},
	}
	if !reflect.DeepEqual(page.Blocks, want) {
		t.Errorf("blocks = %+v, want %+v", page.Blocks, want)
	}
	if page.NextMarker == nil {
		t.Fatal("NextMarker = nil, want 12288")
	}
	if *page.NextMarker != 12288 {
		t.Errorf("NextMarker = %d, want 12288", *page.NextMarker)
	}
}

func TestListChangedBlocksDelta(t *testing.T) {
	srv, rec := newChangedBlocksServer(t, http.StatusOK, `{"changed_blocks":[],"changed_blocks_next_marker":null}`)
	cloud := newTestOpenStack(srv, BlockStorageOpts{})

	page, err := cloud.ListChangedBlocks(context.Background(), testChangedBlocksVolume, ListChangedBlocksOpts{
		BaseSnapshotID:   testChangedBlocksBase,
		TargetSnapshotID: testChangedBlocksTarget,
		Offset:           1 << 30,
		Length:           64 << 30,
		Marker:           4096,
		Limit:            500,
	})
	if err != nil {
		t.Fatalf("ListChangedBlocks: %v", err)
	}

	want := map[string]string{
		"base_snapshot_id":   `"` + testChangedBlocksBase + `"`,
		"target_snapshot_id": `"` + testChangedBlocksTarget + `"`,
		"offset":             "1073741824",
		"length":             "68719476736",
		"marker":             "4096",
		"limit":              "500",
	}
	for key, val := range want {
		if got := string(rec.fields[key]); got != val {
			t.Errorf("%s = %s, want %s", key, got, val)
		}
	}
	if len(rec.fields) != len(want) {
		t.Errorf("request has %d fields, want %d: %v", len(rec.fields), len(want), rec.fields)
	}

	if len(page.Blocks) != 0 {
		t.Errorf("blocks = %+v, want none", page.Blocks)
	}
	if page.NextMarker != nil {
		t.Errorf("NextMarker = %d, want nil", *page.NextMarker)
	}
}

func TestListChangedBlocksHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "409 conflict",
			status: http.StatusConflict,
			body:   `{"conflictingRequest":{"code":409,"message":"backend does not support changed block tracking"}}`,
		},
		{
			name:   "503 unavailable",
			status: http.StatusServiceUnavailable,
			body:   `{"serviceUnavailable":{"code":503,"message":"volume service is unavailable"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newChangedBlocksServer(t, tc.status, tc.body)
			cloud := newTestOpenStack(srv, BlockStorageOpts{})

			page, err := cloud.ListChangedBlocks(context.Background(), testChangedBlocksVolume, ListChangedBlocksOpts{
				BaseSnapshotID:   testChangedBlocksBase,
				TargetSnapshotID: testChangedBlocksTarget,
			})
			if err == nil {
				t.Fatal("expected an error")
			}
			if page != nil {
				t.Errorf("page = %+v, want nil", page)
			}
			if !gophercloud.ResponseCodeIs(err, tc.status) {
				t.Errorf("error %v does not carry status %d", err, tc.status)
			}
			var codeErr gophercloud.ErrUnexpectedResponseCode
			if !errors.As(err, &codeErr) {
				t.Fatalf("error %T is not a gophercloud.ErrUnexpectedResponseCode", err)
			}
			if string(codeErr.Body) != tc.body {
				t.Errorf("body = %s, want %s", codeErr.Body, tc.body)
			}
		})
	}
}

func TestListChangedBlocksIgnoreMicroversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Errorf("request reached Cinder although the microversion is ignored")
	}))
	t.Cleanup(srv.Close)
	cloud := newTestOpenStack(srv, BlockStorageOpts{IgnoreVolumeMicroversion: true})

	page, err := cloud.ListChangedBlocks(context.Background(), testChangedBlocksVolume, ListChangedBlocksOpts{
		TargetSnapshotID: testChangedBlocksTarget,
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if page != nil {
		t.Errorf("page = %+v, want nil", page)
	}
	if !strings.Contains(err.Error(), "ignore-volume-microversion") {
		t.Errorf("error %q does not name ignore-volume-microversion", err)
	}
	if !errors.Is(err, ErrChangedBlocksUnsupported) {
		t.Errorf("error %v is not ErrChangedBlocksUnsupported", err)
	}
}

// versionDocument is the discovery document a versioned Cinder endpoint
// returns, with the given maximum microversion.
func versionDocument(maxVersion string) string {
	return `{"versions":[{"id":"v3.0","status":"CURRENT","version":"` + maxVersion + `","min_version":"3.0"}]}`
}

func TestChangedBlocksSupported(t *testing.T) {
	const projectID = "8f3c1a2b4d5e6f708192a3b4c5d6e7f8"

	cases := []struct {
		name string
		// endpoint is the catalog URL path; wantPath is where Cinder serves
		// the version document for it.
		endpoint  string
		wantPath  string
		status    int
		body      string
		want      bool
		wantError bool
	}{
		{name: "maximum above", endpoint: "/", wantPath: "/", status: http.StatusOK, body: versionDocument("3.75"), want: true},
		{name: "maximum equal", endpoint: "/", wantPath: "/", status: http.StatusOK, body: versionDocument(ChangedBlocksMicroversion), want: true},
		{name: "maximum below", endpoint: "/", wantPath: "/", status: http.StatusOK, body: versionDocument("3.70"), want: false},
		{name: "versioned endpoint", endpoint: "/v3/", wantPath: "/v3/", status: http.StatusOK, body: versionDocument(ChangedBlocksMicroversion), want: true},
		{name: "project scoped endpoint", endpoint: "/v3/" + projectID + "/", wantPath: "/v3/", status: http.StatusOK, body: versionDocument(ChangedBlocksMicroversion), want: true},
		{name: "project scoped endpoint below", endpoint: "/volume/v3/" + projectID + "/", wantPath: "/volume/v3/", status: http.StatusOK, body: versionDocument("3.71"), want: false},
		{name: "discovery fails", endpoint: "/", wantPath: "/", status: http.StatusInternalServerError, body: `{"computeFault":{"code":500,"message":"boom"}}`, wantError: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != tc.wantPath {
					// Cinder has no route below the versioned root other
					// than the project-scoped resources.
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"itemNotFound":{"code":404,"message":"The resource could not be found."}}`))
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			cloud := newTestOpenStackAt(srv, tc.endpoint, BlockStorageOpts{})

			got, err := cloud.ChangedBlocksSupported(context.Background())
			if tc.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ChangedBlocksSupported: %v", err)
			}
			if got != tc.want {
				t.Errorf("supported = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestChangedBlocksSupportedIgnoreMicroversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Errorf("request reached Cinder although the microversion is ignored")
	}))
	t.Cleanup(srv.Close)
	cloud := newTestOpenStack(srv, BlockStorageOpts{IgnoreVolumeMicroversion: true})

	got, err := cloud.ChangedBlocksSupported(context.Background())
	if err != nil {
		t.Fatalf("ChangedBlocksSupported: %v", err)
	}
	if got {
		t.Error("supported = true, want false")
	}
}
