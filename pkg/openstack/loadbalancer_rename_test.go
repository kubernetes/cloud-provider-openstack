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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-openstack/pkg/util"

	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	"github.com/stretchr/testify/assert"
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

func TestLbNameMatchesService(t *testing.T) {
	tests := []struct {
		name        string
		lbName      string
		namespace   string
		serviceName string
		expected    bool
	}{
		{
			// The case renaming was introduced for in #2552: the LB of this
			// exact Service, carrying a stale cluster-name component.
			name:        "LB created for this Service under an old cluster-name",
			lbName:      "kube_service_oldcluster_kns_fakeservice",
			namespace:   "kns",
			serviceName: "fakeservice",
			expected:    true,
		},
		{
			// #2682: a Service references a shared LB created by a Service
			// with a different name in another cluster.
			name:        "shared LB created for a different Service name",
			lbName:      "kube_service_clustera_kns_fakeservice",
			namespace:   "kns",
			serviceName: "otherservice",
			expected:    false,
		},
		{
			// #2682: same Service name, different namespace.
			name:        "shared LB created in a different namespace",
			lbName:      "kube_service_clustera_kns_fakeservice",
			namespace:   "otherns",
			serviceName: "fakeservice",
			expected:    false,
		},
		{
			// #2682 with the same namespace and name in both clusters. From
			// Octavia data alone this is indistinguishable from the legitimate
			// rename above, hence true. Such setups need enable-lb-rename=false.
			name:        "shared LB from another cluster with identical namespace and name",
			lbName:      "kube_service_clustera_kns_fakeservice",
			namespace:   "kns",
			serviceName: "fakeservice",
			expected:    true,
		},
		{
			name:        "LB with a name not decomposable into components",
			lbName:      "kube_service_",
			namespace:   "kns",
			serviceName: "fakeservice",
			expected:    false,
		},
		{
			// Long names are cut to 255 characters on creation, the comparison
			// must reconstruct the cut name identically.
			name:        "255-cut LB name still matches its Service",
			lbName:      util.Sprintf255(lbFormat, servicePrefix, "oldcluster", "namespace"+strings.Repeat("foo", 100), "name"),
			namespace:   "namespace" + strings.Repeat("foo", 100),
			serviceName: "name",
			expected:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lb := &loadbalancers.LoadBalancer{Name: test.lbName}
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: test.namespace,
					Name:      test.serviceName,
				},
			}
			assert.Equal(t, test.expected, lbNameMatchesService(lb, service))
		})
	}
}
