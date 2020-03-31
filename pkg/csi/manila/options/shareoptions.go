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

package options

import (
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/validator"
)

type ControllerVolumeContext struct {
	Protocol         string `name:"protocol" matches:"^(?i)CEPHFS|NFS$"`
	Type             string `name:"type" value:"default:default"`
	ShareNetworkID   string `name:"shareNetworkID" value:"optional"`
	AvailabilityZone string `name:"availability" value:"optional"`

	// Adapter options

	CephfsMounter  string `name:"cephfs-mounter" value:"default:fuse" matches:"^kernel|fuse$"`
	NFSShareClient string `name:"nfs-shareClient" value:"default:0.0.0.0/0"`
}

type NodeVolumeContext struct {
	ShareID       string `name:"shareID" value:"optionalIf:shareName=." precludes:"shareName"`
	ShareName     string `name:"shareName" value:"optionalIf:shareID=." precludes:"shareID"`
	ShareAccessID string `name:"shareAccessID"`

	// Adapter options

	CephfsMounter string `name:"cephfs-mounter" value:"default:fuse" matches:"^kernel|fuse$"`
}

var (
	controllerVolumeCtxValidator = validator.New(&ControllerVolumeContext{})
	nodeVolumeCtxValidator       = validator.New(&NodeVolumeContext{})
)

func NewControllerVolumeContext(data map[string]string) (*ControllerVolumeContext, error) {
	opts := &ControllerVolumeContext{}
	if err := controllerVolumeCtxValidator.Populate(data, opts); err != nil {
		return nil, err
	}

	return opts, nil
}

func NewNodeVolumeContext(data map[string]string) (*NodeVolumeContext, error) {
	opts := &NodeVolumeContext{}
	if err := nodeVolumeCtxValidator.Populate(data, opts); err != nil {
		return nil, err
	}

	return opts, nil
}
