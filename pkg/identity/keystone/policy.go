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
	"bufio"
	"encoding/json"
	"os"
)

type policy struct {
	ResourceSpec *resourcePolicySpec `json:"resource,omitempty"`

	NonResourceSpec *nonResourcePolicySpec `json:"nonresource,omitempty"`

	Match []policyMatch `json:"match"`

	ResourcePermissionsSpec map[string][]string `json:"resource_permissions,omitempty"`

	NonResourcePermissionsSpec map[string][]string `json:"nonresource_permissions,omitempty"`

	Users map[string][]string `json:"users"`
}

// Supported types for policy match.
const (
	TypeUser    string = "user"
	TypeGroup   string = "group"
	TypeProject string = "project"
	TypeRole    string = "role"
)

type policyMatch struct {
	Type string `json:"type"`

	Values []string `json:"values"`
}

type resourcePolicySpec struct {
	// Kubernetes resource API verb like: get, list, watch, create, update, delete, proxy.
	// ["*"] matches all verbs.
	Verbs []string `json:"verbs"`

	// Resources is the list of resource names.
	// ["*"] matches all resources
	Resources []string `json:"resources"`

	// APIGroup is the name of an API group.
	// "*" matches all API groups
	APIGroup *string `json:"version"`

	// Namespace is the name of a namespace.
	// "*" matches all namespaces (including unnamespaced requests)
	Namespace *string `json:"namespace"`
}

type nonResourcePolicySpec struct {
	// Kubernetes resource API verb like: get, list, watch, create, update, delete, proxy.
	// "*" matches all verbs.
	Verbs []string `json:"verbs"`

	// NonResourcePath matches non-resource request paths.
	// "*" matches all paths
	// "/foo/*" matches all subpaths of foo
	NonResourcePath *string `json:"path"`
}

type policyList []*policy

// newFromFile loads a list of policies from a file
func newFromFile(path string) (policyList, error) {
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
