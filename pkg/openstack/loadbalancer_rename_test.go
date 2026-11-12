/*
Copyright 2024 The Kubernetes Authors.

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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/cloud-provider-openstack/pkg/util"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceClusterName(t *testing.T) {
	tests := []struct {
		name           string
		oldClusterName string
		clusterName    string
		objectName     string
		expected       string
	}{
		{
			name:           "Simple kubernetes replace",
			oldClusterName: "kubernetes",
			clusterName:    "cluster123",
			objectName:     "kube_service_kubernetes_namespace_name",
			expected:       "kube_service_cluster123_namespace_name",
		},
		{
			name:           "Simple kube replace",
			oldClusterName: "kube",
			clusterName:    "cluster123",
			objectName:     "kube_service_kube_namespace_name",
			expected:       "kube_service_cluster123_namespace_name",
		},
		{
			name:           "Replace, no prefix",
			oldClusterName: "kubernetes",
			clusterName:    "cluster123",
			objectName:     "foobar_kubernetes_namespace_name",
			expected:       "foobar_cluster123_namespace_name",
		},
		{
			name:           "Replace, not found",
			oldClusterName: "foobar",
			clusterName:    "cluster123",
			objectName:     "kube_service_kubernetes_namespace_name",
			expected:       "kube_service_kubernetes_namespace_name",
		},
		{
			name:           "Replace, cut 255",
			oldClusterName: "kubernetes",
			clusterName:    "cluster123",
			objectName:     "kube_service_kubernetes_namespace_name" + strings.Repeat("foo", 100),
			expected: "kube_service_cluster123_namespace_namefoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoo" +
				"foofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoo" +
				"foofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoofoof",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := replaceClusterName(test.oldClusterName, test.clusterName, test.objectName)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestDecomposeLBName(t *testing.T) {
	tests := []struct {
		name           string
		resourcePrefix string
		objectName     string
		expected       string
	}{
		{
			name:           "Simple kubernetes",
			resourcePrefix: "",
			objectName:     "kube_service_kubernetes_namespace_name",
			expected:       "kubernetes",
		},
		{
			name:           "Kubernetes with prefix",
			resourcePrefix: "listener_",
			objectName:     "listener_kube_service_kubernetes_namespace_name",
			expected:       "kubernetes",
		},
		{
			name:           "Example with _ in clusterName",
			resourcePrefix: "listener_",
			objectName:     "listener_kube_service_kubernetes_123_namespace_name",
			expected:       "kubernetes_123",
		},
		{
			name:           "No match",
			resourcePrefix: "listener_",
			objectName:     "FOOBAR",
			expected:       "",
		},
		{
			name:           "Looong namespace, so string is cut, but no _ in clusterName",
			resourcePrefix: "listener_",
			objectName:     util.CutString255("listener_kube_service_kubernetes_namespace" + strings.Repeat("foo", 100) + "_name"),
			expected:       "kubernetes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := getClusterName(test.resourcePrefix, test.objectName)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestRenameLoadBalancerListenerWithRenamedPool(t *testing.T) {
	const (
		lbID       = "lb-id"
		listenerID = "listener-id"
		poolID     = "pool-id"
	)

	// The pool already has the new cluster-name, e.g. because a previous rename
	// attempt failed after renaming the pool but before renaming the listener.
	var listenerName string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2.0/lbaas/listeners", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"listeners": [{"id": "listener-id", "name": "listener_0_kube_service_old_namespace_name"}]}`))
	})
	mux.HandleFunc("GET /v2.0/lbaas/pools", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"pools": [{"id": "pool-id", "name": "pool_0_kube_service_new_namespace_name", "listeners": [{"id": "listener-id"}]}]}`))
	})
	mux.HandleFunc("PUT /v2.0/lbaas/pools/"+poolID, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("pool with the new cluster-name must not be renamed")
		_, _ = w.Write([]byte(`{"pool": {"id": "pool-id"}}`))
	})
	mux.HandleFunc("PUT /v2.0/lbaas/listeners/"+listenerID, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Listener struct {
				Name string `json:"name"`
			} `json:"listener"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		listenerName = body.Listener.Name
		_, _ = w.Write([]byte(`{"listener": {"id": "listener-id"}}`))
	})
	mux.HandleFunc("/v2.0/lbaas/loadbalancers/"+lbID, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"loadbalancer": {"id": "lb-id", "name": "kube_service_new_namespace_name", "provisioning_status": "ACTIVE"}}`))
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()

	client := &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{HTTPClient: *srv.Client()},
		Endpoint:       srv.URL + "/",
		ResourceBase:   srv.URL + "/v2.0/",
	}
	lb := &loadbalancers.LoadBalancer{ID: lbID, Name: "kube_service_old_namespace_name"}

	_, err := renameLoadBalancer(context.TODO(), client, lb, "kube_service_new_namespace_name", "new")
	require.NoError(t, err)
	assert.Equal(t, "listener_0_kube_service_new_namespace_name", listenerName)
}
