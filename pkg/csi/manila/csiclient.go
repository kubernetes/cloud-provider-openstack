/*
Copyright 2019 The Kubernetes Authors.

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

package manila

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/csiclient"
)

type csiNodeCapabilitySet map[csi.NodeServiceCapability_RPC_Type]bool

func csiNodeGetCapabilities(ctx context.Context, nodeClient csiclient.Node) (csiNodeCapabilitySet, error) {
	rsp, err := nodeClient.GetCapabilities(ctx)
	if err != nil {
		return nil, err
	}

	caps := csiNodeCapabilitySet{}
	for _, cap := range rsp.GetCapabilities() {
		if cap == nil {
			continue
		}
		rpc := cap.GetRpc()
		if rpc == nil {
			continue
		}
		t := rpc.GetType()
		caps[t] = true
	}

	return caps, nil
}
