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
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	deleteTestClusterName = "kubernetes"
	deleteTestLBID        = "a1b2c3d4-0000-4000-8000-000000000001"
	deleteTestVIPPortID   = "a1b2c3d4-0000-4000-8000-000000000002"
	deleteTestSGID        = "a1b2c3d4-0000-4000-8000-000000000003"
	deleteTestNodePortID  = "a1b2c3d4-0000-4000-8000-000000000004"
	deleteTestServiceUID  = "a1b2c3d4-0000-4000-8000-000000000005"
)

// fakeDeleteCloud is a minimal stateful Octavia and Neutron API covering the
// calls made by EnsureLoadBalancerDeleted. It records every request.
type fakeDeleteCloud struct {
	mu sync.Mutex

	lbExists bool
	// lbGetErrorsAfterDelete makes the next N GETs of the load balancer after
	// it was deleted return 503, as a transient error while OCCM waits for the
	// delete to finish.
	lbGetErrorsAfterDelete int
	lbDeleted              bool

	sgExists   bool
	sgName     string
	nodePortSG []string

	requests []string
}

func writeFakeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeDeleteCloud) octavia(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())

	// The API version list is not served on purpose: the Octavia version is
	// cached in a package variable in pkg/util/openstack, and caching it here
	// would change the results of other tests. Without it no tag support is
	// assumed, which the delete path does not need.
	lbPath := "/v2.0/lbaas/loadbalancers/" + deleteTestLBID
	switch {
	case r.Method == http.MethodGet && r.URL.Path == lbPath:
		if f.lbDeleted && f.lbGetErrorsAfterDelete > 0 {
			f.lbGetErrorsAfterDelete--
			writeFakeJSON(w, http.StatusServiceUnavailable, map[string]any{"faultstring": "Service Unavailable"})
			return
		}
		if !f.lbExists {
			writeFakeJSON(w, http.StatusNotFound, map[string]any{"faultstring": "Not Found"})
			return
		}
		writeFakeJSON(w, http.StatusOK, map[string]any{"loadbalancer": map[string]any{
			"id":                  deleteTestLBID,
			"name":                fmt.Sprintf("%s%s_default_web", servicePrefix, deleteTestClusterName),
			"provisioning_status": activeStatus,
			"operating_status":    "ONLINE",
			"vip_port_id":         deleteTestVIPPortID,
		}})
	case r.Method == http.MethodDelete && r.URL.Path == lbPath && f.lbExists:
		f.lbExists = false
		f.lbDeleted = true
		w.WriteHeader(http.StatusNoContent)
	default:
		writeFakeJSON(w, http.StatusNotFound, map[string]any{"faultstring": "Not Found"})
	}
}

func (f *fakeDeleteCloud) neutron(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())

	nodePort := map[string]any{"id": deleteTestNodePortID, "security_groups": f.nodePortSG}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v2.0/floatingips":
		writeFakeJSON(w, http.StatusOK, map[string]any{"floatingips": []any{}})
	case r.Method == http.MethodGet && r.URL.Path == "/v2.0/security-groups":
		groups := []any{}
		if f.sgExists && r.URL.Query().Get("name") == f.sgName {
			groups = append(groups, map[string]any{"id": deleteTestSGID, "name": f.sgName})
		}
		writeFakeJSON(w, http.StatusOK, map[string]any{"security_groups": groups})
	case r.Method == http.MethodDelete && r.URL.Path == "/v2.0/security-groups/"+deleteTestSGID && f.sgExists:
		f.sgExists = false
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/v2.0/ports":
		ports := []any{}
		if slices.Contains(f.nodePortSG, r.URL.Query().Get("security_groups")) {
			ports = append(ports, nodePort)
		}
		writeFakeJSON(w, http.StatusOK, map[string]any{"ports": ports})
	case r.Method == http.MethodPut && r.URL.Path == "/v2.0/ports/"+deleteTestNodePortID:
		var body struct {
			Port struct {
				SecurityGroups []string `json:"security_groups"`
			} `json:"port"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.nodePortSG = body.Port.SecurityGroups
		nodePort["security_groups"] = f.nodePortSG
		writeFakeJSON(w, http.StatusOK, map[string]any{"port": nodePort})
	default:
		writeFakeJSON(w, http.StatusNotFound, map[string]any{"NeutronError": map[string]any{"type": "NotFound"}})
	}
}

func TestEnsureLoadBalancerDeletedRemovesSecurityGroup(t *testing.T) {
	testCases := []struct {
		name                   string
		lbExists               bool
		lbGetErrorsAfterDelete int
		// attempts is how many times the service controller calls
		// EnsureLoadBalancerDeleted; all but the last are expected to fail.
		attempts int
	}{
		{
			name:     "load balancer exists",
			lbExists: true,
			attempts: 1,
		},
		{
			name:     "load balancer already deleted",
			lbExists: false,
			attempts: 1,
		},
		{
			name:                   "retry after the load balancer was deleted but waiting for it failed",
			lbExists:               true,
			lbGetErrorsAfterDelete: 1,
			attempts:               2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "web",
					Namespace:   "default",
					UID:         types.UID(deleteTestServiceUID),
					Annotations: map[string]string{ServiceAnnotationLoadBalancerID: deleteTestLBID},
				},
				Spec: corev1.ServiceSpec{
					Type:  corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{{Port: 80, NodePort: 30080, Protocol: corev1.ProtocolTCP}},
				},
			}
			f := &fakeDeleteCloud{
				lbExists:               tc.lbExists,
				lbGetErrorsAfterDelete: tc.lbGetErrorsAfterDelete,
				sgExists:               true,
				sgName:                 getSecurityGroupName(service),
				nodePortSG:             []string{"default", deleteTestSGID},
			}
			lbServer := httptest.NewServer(http.HandlerFunc(f.octavia))
			defer lbServer.Close()
			networkServer := httptest.NewServer(http.HandlerFunc(f.neutron))
			defer networkServer.Close()

			provider := &gophercloud.ProviderClient{HTTPClient: *http.DefaultClient}
			lbaas := &LbaasV2{LoadBalancer: LoadBalancer{
				lb:      &gophercloud.ServiceClient{ProviderClient: provider, Endpoint: lbServer.URL + "/", ResourceBase: lbServer.URL + "/v2.0/"},
				network: &gophercloud.ServiceClient{ProviderClient: provider, Endpoint: networkServer.URL + "/", ResourceBase: networkServer.URL + "/v2.0/"},
				opts: LoadBalancerOpts{
					LBProvider:           "ovn",
					CascadeDelete:        true,
					ManageSecurityGroups: true,
				},
			}}

			for i := 1; i < tc.attempts; i++ {
				err := lbaas.EnsureLoadBalancerDeleted(context.TODO(), deleteTestClusterName, service)
				require.Error(t, err, "attempt %d", i)
			}
			err := lbaas.EnsureLoadBalancerDeleted(context.TODO(), deleteTestClusterName, service)
			require.NoError(t, err)

			f.mu.Lock()
			defer f.mu.Unlock()
			assert.False(t, f.lbExists, "load balancer still exists")
			assert.False(t, f.sgExists, "security group %s was not deleted; requests: %v", f.sgName, f.requests)
			assert.NotContains(t, f.nodePortSG, deleteTestSGID, "security group still attached to the node port")
		})
	}
}
