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

package shareoptions

import (
	"fmt"
	"github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/controller"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions/validator"
	"k8s.io/cloud-provider/volume/helpers"
)

// ShareOptions contains options for provisioning and attaching a share
type ShareOptions struct {
	// Common options

	Zones    string `name:"zones" value:"default:nova"`
	Type     string `name:"type" value:"default:default"`
	Protocol string `name:"protocol" matches:"^(?i)CEPHFS|NFS$"`
	Backend  string `name:"backend" matches:"^cephfs|csi-cephfs|nfs$"`

	OSSecretName         string `name:"osSecretName"`
	OSSecretNamespace    string `name:"osSecretNamespace" value:"default:default"`
	ShareSecretNamespace string `name:"shareSecretNamespace" value:"default:default"`

	OSShareNetworkID string `name:"osShareNetworkID" value:"optional"`

	OSShareID       string `name:"osShareID" value:"optional" dependsOn:"osShareAccessID"`
	OSShareName     string `name:"osShareName" value:"optional" dependsOn:"osShareAccessID"`
	OSShareAccessID string `name:"osShareAccessID" value:"optional" dependsOn:"osShareID|osShareName"`

	// Backend options

	CSICEPHFSdriver  string `name:"csi-driver" value:"requiredIf:backend=^csi-cephfs$"`
	CSICEPHFSmounter string `name:"mounter" value:"default:fuse" matches:"^kernel|fuse$"`

	NFSShareClient string `name:"nfs-share-client" value:"default:0.0.0.0"`
}

var (
	shareOptionsValidator = validator.New(&ShareOptions{})
)

// NewShareOptions creates a new instance of ShareOptions
func NewShareOptions(volOptions *controller.VolumeOptions) (*ShareOptions, error) {
	opts := &ShareOptions{}

	if err := shareOptionsValidator.Populate(volOptions.Parameters, opts); err != nil {
		return nil, err
	}

	setOfZones, err := helpers.ZonesToSet(opts.Zones)
	if err != nil {
		return nil, err
	}

	zones := helpers.ChooseZonesForVolume(setOfZones, volOptions.PVC.GetName(), 1)
	if zones.Len() == 0 {
		return nil, fmt.Errorf("could not find a zone")
	} else {
		opts.Zones = zones.List()[0]
	}

	return opts, nil
}
