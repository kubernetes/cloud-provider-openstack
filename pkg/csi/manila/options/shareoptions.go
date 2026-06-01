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
	Protocol            string `name:"protocol" matches:"^(?i)CEPHFS|NFS$"`
	Type                string `name:"type" value:"default:default"`
	ShareNetworkID      string `name:"shareNetworkID" value:"optional"`
	AutoTopology        string `name:"autoTopology" value:"default:false" matches:"(?i)^true|false$"`
	AvailabilityZone    string `name:"availability" value:"optional"`
	AppendShareMetadata string `name:"appendShareMetadata" value:"optional"`
	Affinity            string `name:"affinity" value:"optional"`
	AntiAffinity        string `name:"antiAffinity" value:"optional"`
	GroupID             string `name:"groupID" value:"optional"`

	// Adapter options

	CephfsMounter            string `name:"cephfs-mounter" value:"default:fuse" matches:"^kernel|fuse$"`
	CephfsClientID           string `name:"cephfs-clientID" value:"optional"`
	CephfsKernelMountOptions string `name:"cephfs-kernelMountOptions" value:"optional"`
	CephfsFuseMountOptions   string `name:"cephfs-fuseMountOptions" value:"optional"`
	NFSShareClient           string `name:"nfs-shareClient" value:"default:0.0.0.0/0"`
}

type NodeVolumeContext struct {
	ShareID        string `name:"shareID" value:"optionalIf:shareName=." precludes:"shareName"`
	ShareName      string `name:"shareName" value:"optionalIf:shareID=." precludes:"shareID"`
	ShareAccessID  string `name:"shareAccessID" value:"optionalIf:shareAccessIDs=." precludes:"shareAccessIDs"` // Keep this for backwards compatibility
	ShareAccessIDs string `name:"shareAccessIDs" value:"optionalIf:shareAccessID=." precludes:"shareAccessID"`

	// Adapter options

	CephfsMounter            string `name:"cephfs-mounter" value:"default:fuse" matches:"^kernel|fuse$"`
	CephfsKernelMountOptions string `name:"cephfs-kernelMountOptions" value:"optional"`
	CephfsFuseMountOptions   string `name:"cephfs-fuseMountOptions" value:"optional"`
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

func NodeVolumeContextFields() []string {
	return nodeVolumeCtxValidator.Fields
}
