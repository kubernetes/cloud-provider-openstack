/*
Copyright 2014 The Kubernetes Authors.

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

package helper

import (
	"k8s.io/api/core/v1"
)

// AddToNodeAddresses appends the NodeAddresses to the passed-by-pointer slice,
// only if they do not already exist
func AddToNodeAddresses(addresses *[]v1.NodeAddress, addAddresses ...v1.NodeAddress) {
	for _, add := range addAddresses {
		exists := false
		for _, existing := range *addresses {
			if existing.Address == add.Address && existing.Type == add.Type {
				exists = true
				break
			}
		}
		if !exists {
			*addresses = append(*addresses, add)
		}
	}
}

// RemoveFromNodeAddresses removes the NodeAddresses from the passed-by-pointer
// slice if they already exist.
func RemoveFromNodeAddressees(addresses *[]v1.NodeAddress, removeAddresses ...v1.NodeAddress) {
	var indexesToRemove []int
	for _, remove := range removeAddresses {
		for i := len(*addresses) - 1; i >= 0; i-- {
			existing := (*addresses)[i]
			if existing.Address == remove.Address && (existing.Type == remove.Type || remove.Type == "") {
				indexesToRemove = append(indexesToRemove, i)
			}
		}
	}
	for _, i := range indexesToRemove {
		if i < len(*addresses) {
			*addresses = append((*addresses)[:i], (*addresses)[i+1:]...)
		}
	}
}
