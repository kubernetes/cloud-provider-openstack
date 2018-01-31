/*
Copyright 2015 The Kubernetes Authors.

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

package keystone

import (
	"os"
	"bufio"
	"encoding/json"
)

type Policy struct {
	ResourceSpec *ResourcePolicySpec `json:"resource,omitempty"`

	NonResourceSpec *NonResourcePolicySpec `json:"nonresource,omitempty"`

	// One of user:foo, project:bar, role:baz, group:qux
	Match Match `json:"match"`
}

type Match struct {
	Type string `json:"type"`

	Value string `json:"value"`
}

type ResourcePolicySpec struct {
	// Kubernetes resource API verb like: get, list, watch, create, update, delete, proxy.
	// "*" matches all verbs.
	Verb string `json:"verb"`

	// Resource is the name of a resource.
	// "*" matches all resources
	Resource *string `json:"resource"`

	// APIGroup is the name of an API group.
	// "*" matches all API groups
	APIGroup *string `json:"version"`

	// Namespace is the name of a namespace.
	// "*" matches all namespaces (including unnamespaced requests)
	Namespace *string `json:"namespace"`
}

type NonResourcePolicySpec struct {
	// Kubernetes resource API verb like: get, list, watch, create, update, delete, proxy.
	// "*" matches all verbs.
	Verb string `json:"verb"`

	// NonResourcePath matches non-resource request paths.
	// "*" matches all paths
	// "/foo/*" matches all subpaths of foo
	NonResourcePath *string `json:"path"`
}

type policyList []*Policy

func NewFromFile(path string) (policyList, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var data policyList

	reader := bufio.NewReader(file)
	decoder := json.NewDecoder(reader)
	err = decoder.Decode(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}
