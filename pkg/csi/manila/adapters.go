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
	"strings"

	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
	"k8s.io/klog/v2"
)

func getShareAdapter(proto string) shareadapters.ShareAdapter {
	switch strings.ToUpper(proto) {
	case "CEPHFS":
		return &shareadapters.Cephfs{}
	case "NFS":
		return &shareadapters.NFS{}
	default:
		klog.Fatalf("unknown share adapter %s", proto)
	}

	return nil
}
