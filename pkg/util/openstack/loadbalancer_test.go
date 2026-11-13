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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	cbmetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/testutil"

	"k8s.io/cloud-provider-openstack/pkg/metrics"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
)

// route identifies an HTTP handler by method and exact request path.
type route struct {
	method string
	path   string
}

// testServer is an httptest server plus the gophercloud ServiceClient pointed at
// it, so the openstack util functions issue real HTTP requests against handlers
// we control.
type testServer struct {
	t        *testing.T
	server   *httptest.Server
	client   *gophercloud.ServiceClient
	handlers map[route]http.HandlerFunc
}

// newTestServer builds a test server. Waits are kept fast by having status GETs
// return a terminal state (ACTIVE / 404) on the first poll, so the backoff in
// WaitActiveAndGetLoadBalancer / waitLoadBalancerDeleted exits on step one. The
// OCCM_WAIT_LB_ACTIVE_STEPS guard keeps a regression from blowing up into a long
// retry loop if an expectation is wrong.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	t.Setenv("OCCM_WAIT_LB_ACTIVE_STEPS", "1")

	ts := &testServer{t: t, handlers: make(map[route]http.HandlerFunc)}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, ok := ts.handlers[route{r.Method, r.URL.Path}]
		if !ok {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		h(w, r)
	}))
	t.Cleanup(ts.server.Close)

	endpoint := ts.server.URL + "/"
	ts.client = &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{},
		Endpoint:       endpoint,
	}
	return ts
}

// handle registers a handler for an exact method+path.
func (ts *testServer) handle(method, path string, h http.HandlerFunc) {
	ts.handlers[route{method, path}] = h
}

// json registers a handler that returns status code with a JSON body.
func (ts *testServer) json(method, path string, status int, body string) {
	ts.handle(method, path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// lbBody renders a single-loadbalancer response body with the given id/status.
func lbBody(id, status string) string {
	return fmt.Sprintf(`{"loadbalancer":{"id":%q,"name":"lb","provisioning_status":%q}}`, id, status)
}

func ctx() context.Context { return context.Background() }

// counterValue reads the current value of a single labelled counter from a
// CounterVec, so tests can assert on the delta across a call.
func counterValue(t *testing.T, vec *cbmetrics.CounterVec, label string) float64 {
	t.Helper()
	v, err := testutil.GetCounterMetricValue(vec.WithLabelValues(label))
	if err != nil {
		t.Fatalf("failed to read counter %q: %v", label, err)
	}
	return v
}

func TestLoadbalancerUpdateReturnsActive(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"

	// PUT responds with PENDING_UPDATE, as the real API does.
	ts.json(http.MethodPut, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, "PENDING_UPDATE"))
	// The subsequent status poll returns ACTIVE.
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	name := "renamed"
	lb, err := UpdateLoadBalancer(ctx(), ts.client, lbID, loadbalancers.UpdateOpts{Name: &name})
	if err != nil {
		t.Fatalf("UpdateLoadBalancer returned error: %v", err)
	}
	// The whole point of the fix: callers get the ACTIVE LB, not PENDING_UPDATE.
	if lb.ProvisioningStatus != activeStatus {
		t.Fatalf("expected ACTIVE load balancer, got %q", lb.ProvisioningStatus)
	}
}

func TestLoadbalancerUpdateError(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodPut, "/lbaas/loadbalancers/"+lbID, http.StatusInternalServerError, `{}`)

	name := "renamed"
	if _, err := UpdateLoadBalancer(ctx(), ts.client, lbID, loadbalancers.UpdateOpts{Name: &name}); err == nil {
		t.Fatal("expected error from failed update, got nil")
	}
}

func TestUpdateLoadBalancerTags(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodPut, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, "PENDING_UPDATE"))
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	if err := UpdateLoadBalancerTags(ctx(), ts.client, lbID, []string{"a", "b"}); err != nil {
		t.Fatalf("UpdateLoadBalancerTags returned error: %v", err)
	}
}

func TestGetLoadbalancerByID(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	lb, err := GetLoadbalancerByID(ctx(), ts.client, lbID)
	if err != nil {
		t.Fatalf("GetLoadbalancerByID returned error: %v", err)
	}
	if lb.ID != lbID {
		t.Fatalf("expected lb id %q, got %q", lbID, lb.ID)
	}
}

func TestGetLoadbalancerByIDNotFound(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "missing"
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusNotFound, `{}`)

	if _, err := GetLoadbalancerByID(ctx(), ts.client, lbID); err == nil {
		t.Fatal("expected error for missing load balancer, got nil")
	}
}

// TestGetLoadbalancerByName exercises the single-result list helper via the LB
// name lookup: zero, two, and exactly one match.
func TestGetLoadbalancerByName(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr error
		wantID  string
	}{
		{
			name:    "no results",
			body:    `{"loadbalancers":[]}`,
			wantErr: cpoerrors.ErrNotFound,
		},
		{
			name:    "multiple results",
			body:    `{"loadbalancers":[{"id":"a"},{"id":"b"}]}`,
			wantErr: cpoerrors.ErrMultipleResults,
		},
		{
			name:   "single result",
			body:   `{"loadbalancers":[{"id":"only"}]}`,
			wantID: "only",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.json(http.MethodGet, "/lbaas/loadbalancers", http.StatusOK, tt.body)

			lb, err := GetLoadbalancerByName(ctx(), ts.client, "lb")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lb.ID != tt.wantID {
				t.Fatalf("expected id %q, got %q", tt.wantID, lb.ID)
			}
		})
	}
}

// TestGetLoadbalancerByNameDuplicateAcrossPages guards the fix for kayrus's
// review comment: a duplicate resource may appear on a *later* page. Each page
// here carries a single load balancer, so neither page alone looks ambiguous,
// but the total is two. The helper must page through to the second result and
// report ErrMultipleResults rather than silently returning the first match.
func TestGetLoadbalancerByNameDuplicateAcrossPages(t *testing.T) {
	ts := newTestServer(t)

	// Gophercloud reads the next-page URL from the "loadbalancers_links" rel=next
	// entry. Both pages share the same path, so route by the marker query param:
	// page 1 carries one result plus a link to page 2, page 2 carries the second.
	page2URL := ts.server.URL + "/lbaas/loadbalancers?marker=page2"
	ts.handle(http.MethodGet, "/lbaas/loadbalancers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Query().Get("marker") == "page2" {
			_, _ = w.Write([]byte(`{"loadbalancers":[{"id":"dup"}]}`))
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"loadbalancers":[{"id":"dup"}],"loadbalancers_links":[{"rel":"next","href":%q}]}`, page2URL)))
	})

	if _, err := GetLoadbalancerByName(ctx(), ts.client, "lb"); !errors.Is(err, cpoerrors.ErrMultipleResults) {
		t.Fatalf("expected ErrMultipleResults across pages, got %v", err)
	}
}

// TestGetLoadbalancerByNameEmptyListNotAnAPIError guards the fix for the review
// comment that an empty list returned with HTTP 200 must NOT be recorded as an
// OpenStack API error. ErrNotFound is our own result-verification sentinel, not
// a transport error, so the error counter for "loadbalancer_list" must stay flat
// while the total request counter still increments.
func TestGetLoadbalancerByNameEmptyListNotAnAPIError(t *testing.T) {
	metrics.RegisterMetrics("occm")
	const label = "loadbalancer_list"

	errsBefore := counterValue(t, metrics.APIRequestMetrics.Errors, label)
	totalBefore := counterValue(t, metrics.APIRequestMetrics.Total, label)

	ts := newTestServer(t)
	ts.json(http.MethodGet, "/lbaas/loadbalancers", http.StatusOK, `{"loadbalancers":[]}`)

	if _, err := GetLoadbalancerByName(ctx(), ts.client, "lb"); !errors.Is(err, cpoerrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if got := counterValue(t, metrics.APIRequestMetrics.Errors, label) - errsBefore; got != 0 {
		t.Fatalf("empty-list 200 recorded as API error: error counter grew by %v, want 0", got)
	}
	if got := counterValue(t, metrics.APIRequestMetrics.Total, label) - totalBefore; got != 1 {
		t.Fatalf("expected total request counter to grow by 1, grew by %v", got)
	}
}

// TestGetLoadbalancerByNameMultipleNotAnAPIError is the ErrMultipleResults
// counterpart: a 200 with several entries is also a successful request.
func TestGetLoadbalancerByNameMultipleNotAnAPIError(t *testing.T) {
	metrics.RegisterMetrics("occm")
	const label = "loadbalancer_list"

	errsBefore := counterValue(t, metrics.APIRequestMetrics.Errors, label)

	ts := newTestServer(t)
	ts.json(http.MethodGet, "/lbaas/loadbalancers", http.StatusOK, `{"loadbalancers":[{"id":"a"},{"id":"b"}]}`)

	if _, err := GetLoadbalancerByName(ctx(), ts.client, "lb"); !errors.Is(err, cpoerrors.ErrMultipleResults) {
		t.Fatalf("expected ErrMultipleResults, got %v", err)
	}

	if got := counterValue(t, metrics.APIRequestMetrics.Errors, label) - errsBefore; got != 0 {
		t.Fatalf("multi-result 200 recorded as API error: error counter grew by %v, want 0", got)
	}
}

func TestGetListenerByName(t *testing.T) {
	ts := newTestServer(t)
	ts.json(http.MethodGet, "/lbaas/listeners", http.StatusOK, `{"listeners":[{"id":"l1","name":"listener"}]}`)

	listener, err := GetListenerByName(ctx(), ts.client, "listener", "lb-1")
	if err != nil {
		t.Fatalf("GetListenerByName returned error: %v", err)
	}
	if listener.ID != "l1" {
		t.Fatalf("expected listener id l1, got %q", listener.ID)
	}
}

func TestGetPoolByName(t *testing.T) {
	ts := newTestServer(t)
	ts.json(http.MethodGet, "/lbaas/pools", http.StatusOK, `{"pools":[]}`)

	if _, err := GetPoolByName(ctx(), ts.client, "pool", "lb-1"); !errors.Is(err, cpoerrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestGetPoolByListener covers the four branches of the reworked lookup.
func TestGetPoolByListener(t *testing.T) {
	const lbID, listenerID = "lb-1", "list-1"

	t.Run("default pool", func(t *testing.T) {
		ts := newTestServer(t)
		ts.json(http.MethodGet, "/lbaas/listeners", http.StatusOK,
			fmt.Sprintf(`{"listeners":[{"id":%q,"default_pool_id":"pool-default"}]}`, listenerID))
		ts.json(http.MethodGet, "/lbaas/pools/pool-default", http.StatusOK, `{"pool":{"id":"pool-default"}}`)

		pool, err := GetPoolByListener(ctx(), ts.client, lbID, listenerID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pool.ID != "pool-default" {
			t.Fatalf("expected pool-default, got %q", pool.ID)
		}
	})

	t.Run("fallback to single pools entry", func(t *testing.T) {
		ts := newTestServer(t)
		ts.json(http.MethodGet, "/lbaas/listeners", http.StatusOK,
			fmt.Sprintf(`{"listeners":[{"id":%q,"pools":[{"id":"pool-fallback"}]}]}`, listenerID))
		ts.json(http.MethodGet, "/lbaas/pools/pool-fallback", http.StatusOK, `{"pool":{"id":"pool-fallback"}}`)

		pool, err := GetPoolByListener(ctx(), ts.client, lbID, listenerID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pool.ID != "pool-fallback" {
			t.Fatalf("expected pool-fallback, got %q", pool.ID)
		}
	})

	t.Run("no pools", func(t *testing.T) {
		ts := newTestServer(t)
		ts.json(http.MethodGet, "/lbaas/listeners", http.StatusOK,
			fmt.Sprintf(`{"listeners":[{"id":%q}]}`, listenerID))

		if _, err := GetPoolByListener(ctx(), ts.client, lbID, listenerID); !errors.Is(err, cpoerrors.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("multiple pools", func(t *testing.T) {
		ts := newTestServer(t)
		ts.json(http.MethodGet, "/lbaas/listeners", http.StatusOK,
			fmt.Sprintf(`{"listeners":[{"id":%q,"pools":[{"id":"p1"},{"id":"p2"}]}]}`, listenerID))

		if _, err := GetPoolByListener(ctx(), ts.client, lbID, listenerID); !errors.Is(err, cpoerrors.ErrMultipleResults) {
			t.Fatalf("expected ErrMultipleResults, got %v", err)
		}
	})
}

func TestCreateListener(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodPost, "/lbaas/listeners", http.StatusCreated, `{"listener":{"id":"l-new","name":"listener"}}`)
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	listener, err := CreateListener(ctx(), ts.client, lbID, listeners.CreateOpts{
		Name:           "listener",
		Protocol:       listeners.ProtocolHTTP,
		ProtocolPort:   80,
		LoadbalancerID: lbID,
	})
	if err != nil {
		t.Fatalf("CreateListener returned error: %v", err)
	}
	if listener.ID != "l-new" {
		t.Fatalf("expected created listener id l-new, got %q", listener.ID)
	}
}

func TestCreatePool(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodPost, "/lbaas/pools", http.StatusCreated, `{"pool":{"id":"p-new"}}`)
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	pool, err := CreatePool(ctx(), ts.client, pools.CreateOpts{
		LoadbalancerID: lbID,
		Protocol:       pools.ProtocolHTTP,
		LBMethod:       pools.LBMethodRoundRobin,
	}, lbID)
	if err != nil {
		t.Fatalf("CreatePool returned error: %v", err)
	}
	if pool.ID != "p-new" {
		t.Fatalf("expected created pool id p-new, got %q", pool.ID)
	}
}

func TestUpdateListenerWaitsActive(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodPut, "/lbaas/listeners/l1", http.StatusOK, `{"listener":{"id":"l1"}}`)
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	name := "newname"
	if err := UpdateListener(ctx(), ts.client, lbID, "l1", listeners.UpdateOpts{Name: &name}); err != nil {
		t.Fatalf("UpdateListener returned error: %v", err)
	}
}

func TestDeleteListenerWaitsActive(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodDelete, "/lbaas/listeners/l1", http.StatusNoContent, ``)
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	if err := DeleteListener(ctx(), ts.client, "l1", lbID); err != nil {
		t.Fatalf("DeleteListener returned error: %v", err)
	}
}

func TestUpdatePoolWaitsActive(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodPut, "/lbaas/pools/p1", http.StatusOK, `{"pool":{"id":"p1"}}`)
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusOK, lbBody(lbID, activeStatus))

	name := "newpool"
	if err := UpdatePool(ctx(), ts.client, lbID, "p1", pools.UpdateOpts{Name: &name}); err != nil {
		t.Fatalf("UpdatePool returned error: %v", err)
	}
}

func TestDeleteLoadbalancer(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	ts.json(http.MethodDelete, "/lbaas/loadbalancers/"+lbID, http.StatusNoContent, ``)
	// waitLoadBalancerDeleted polls GET until NotFound.
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusNotFound, `{}`)

	if err := DeleteLoadbalancer(ctx(), ts.client, lbID, true); err != nil {
		t.Fatalf("DeleteLoadbalancer returned error: %v", err)
	}
}

func TestDeleteLoadbalancerAlreadyGone(t *testing.T) {
	ts := newTestServer(t)
	const lbID = "lb-1"
	// Delete itself 404s; this must be treated as success.
	ts.json(http.MethodDelete, "/lbaas/loadbalancers/"+lbID, http.StatusNotFound, `{}`)
	ts.json(http.MethodGet, "/lbaas/loadbalancers/"+lbID, http.StatusNotFound, `{}`)

	if err := DeleteLoadbalancer(ctx(), ts.client, lbID, false); err != nil {
		t.Fatalf("DeleteLoadbalancer on already-deleted lb returned error: %v", err)
	}
}

func TestGetLoadBalancersList(t *testing.T) {
	ts := newTestServer(t)
	ts.json(http.MethodGet, "/lbaas/loadbalancers", http.StatusOK,
		`{"loadbalancers":[{"id":"a"},{"id":"b"}]}`)

	lbs, err := GetLoadBalancers(ctx(), ts.client, loadbalancers.ListOpts{})
	if err != nil {
		t.Fatalf("GetLoadBalancers returned error: %v", err)
	}
	if len(lbs) != 2 {
		t.Fatalf("expected 2 load balancers, got %d", len(lbs))
	}
}

func TestGetPoolsEmpty(t *testing.T) {
	ts := newTestServer(t)
	ts.json(http.MethodGet, "/lbaas/pools", http.StatusOK, `{"pools":[]}`)

	pls, err := GetPools(ctx(), ts.client, "lb-1")
	if err != nil {
		t.Fatalf("GetPools returned error: %v", err)
	}
	if len(pls) != 0 {
		t.Fatalf("expected empty pool slice, got %d", len(pls))
	}
}

func TestGetListenersByLoadBalancerID(t *testing.T) {
	ts := newTestServer(t)
	ts.json(http.MethodGet, "/lbaas/listeners", http.StatusOK,
		`{"listeners":[{"id":"l1"},{"id":"l2"},{"id":"l3"}]}`)

	ls, err := GetListenersByLoadBalancerID(ctx(), ts.client, "lb-1")
	if err != nil {
		t.Fatalf("GetListenersByLoadBalancerID returned error: %v", err)
	}
	if len(ls) != 3 {
		t.Fatalf("expected 3 listeners, got %d", len(ls))
	}
}

// Guard against accidentally leaking the server URL scheme assumption.
func TestEndpointIsHTTP(t *testing.T) {
	ts := newTestServer(t)
	if !strings.HasPrefix(ts.client.Endpoint, "http://") {
		t.Fatalf("expected http endpoint, got %q", ts.client.Endpoint)
	}
}
