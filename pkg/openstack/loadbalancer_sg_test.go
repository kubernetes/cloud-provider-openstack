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
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnsureAndUpdateOctaviaSecurityGroupRuleAlreadyDeleted(t *testing.T) {
	var (
		mu             sync.Mutex
		deletedRules   []string
		updatedPorts   []string
		createdRuleCnt int
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2.0/security-groups", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"security_groups": [{"id": "sg-id", "name": "lb-sg-uid-default-web"}]}`))
	})
	mux.HandleFunc("GET /v2.0/subnets/subnet-id", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"subnet": {"id": "subnet-id", "cidr": "10.0.0.0/24"}}`))
	})
	// Two stale rules for node ports the Service no longer uses.
	mux.HandleFunc("GET /v2.0/security-group-rules", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"security_group_rules": [
			{"id": "rule-gone", "security_group_id": "sg-id", "direction": "ingress", "ethertype": "IPv4", "protocol": "tcp", "port_range_min": 30001, "port_range_max": 30001, "remote_ip_prefix": "10.0.0.0/24"},
			{"id": "rule-stale", "security_group_id": "sg-id", "direction": "ingress", "ethertype": "IPv4", "protocol": "tcp", "port_range_min": 30002, "port_range_max": 30002, "remote_ip_prefix": "10.0.0.0/24"}
		]}`))
	})
	mux.HandleFunc("POST /v2.0/security-group-rules", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		createdRuleCnt++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"security_group_rule": {"id": "rule-new", "security_group_id": "sg-id"}}`))
	})
	mux.HandleFunc("DELETE /v2.0/security-group-rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "rule-gone" {
			// Deleted concurrently by someone else.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"NeutronError": {"type": "SecurityGroupRuleNotFound"}}`))
			return
		}
		mu.Lock()
		deletedRules = append(deletedRules, id)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v2.0/ports", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ports": [{"id": "port-id", "device_id": "server-id", "port_security_enabled": true, "security_groups": ["default-sg"],
			"fixed_ips": [{"ip_address": "10.0.0.5", "subnet_id": "subnet-id"}]}]}`))
	})
	mux.HandleFunc("PUT /v2.0/ports/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		updatedPorts = append(updatedPorts, r.PathValue("id"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"port": {"id": "port-id"}}`))
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()

	network := &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{HTTPClient: *srv.Client()},
		Endpoint:       srv.URL + "/",
		ResourceBase:   srv.URL + "/v2.0/",
	}
	lbaas := &LbaasV2{LoadBalancer{network: network}}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default", UID: "uid"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80, NodePort: 30000, Protocol: corev1.ProtocolTCP}},
		},
	}
	nodes := []*corev1.Node{{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{ProviderID: "openstack:///server-id"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.5"}},
		},
	}}
	svcConf := &serviceConfig{lbMemberSubnetID: "subnet-id"}

	err := lbaas.ensureAndUpdateOctaviaSecurityGroup(context.TODO(), "kubernetes", service, nodes, svcConf)
	require.NoError(t, err)

	assert.Equal(t, 1, createdRuleCnt, "rule for the current node port must be created")
	assert.Equal(t, []string{"rule-stale"}, deletedRules, "remaining stale rules must still be deleted")
	assert.Equal(t, []string{"port-id"}, updatedPorts, "security group must still be added to the node ports")
}
