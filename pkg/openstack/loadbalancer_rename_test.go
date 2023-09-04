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
	"k8s.io/cloud-provider-openstack/pkg/util"
	"strings"
	"testing"

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
