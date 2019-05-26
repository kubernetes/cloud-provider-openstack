/*
Copyright 2018 The Kubernetes Authors.

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

package utils

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Hash gets the data hash.
func Hash(data string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
}

// GetResourceName get Ingress related resource name.
func GetResourceName(namespace, name, clusterName string) string {
	// Keep consistent with the name of load balancer created for Service.
	return fmt.Sprintf("kube_ingress_%s_%s_%s", clusterName, namespace, name)
}

// NodeNames get all the node names.
func NodeNames(nodes []*apiv1.Node) []string {
	ret := make([]string, len(nodes))
	for i, node := range nodes {
		ret[i] = node.Name
	}
	return ret
}

// NodeSlicesEqual check if two nodes equals to each other.
func NodeSlicesEqual(x, y []*apiv1.Node) bool {
	if len(x) != len(y) {
		return false
	}
	return stringSlicesEqual(NodeNames(x), NodeNames(y))
}

func stringSlicesEqual(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	if !sort.StringsAreSorted(x) {
		sort.Strings(x)
	}
	if !sort.StringsAreSorted(y) {
		sort.Strings(y)
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// GetNodeID get instance ID from the Node spec.
func GetNodeID(node *apiv1.Node) (string, error) {
	var providerIDRegexp = regexp.MustCompile(`^openstack:///([^/]+)$`)

	matches := providerIDRegexp.FindStringSubmatch(node.Spec.ProviderID)
	if len(matches) != 2 {
		return "", fmt.Errorf("failed to find instance ID from node provider ID %s", node.Spec.ProviderID)
	}

	return matches[1], nil
}

// Convert2Set converts a string list to string set.
func Convert2Set(list []string) sets.String {
	set := sets.NewString()
	for _, s := range list {
		set.Insert(s)
	}

	return set
}
