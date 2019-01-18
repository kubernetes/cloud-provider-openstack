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
	"github.com/pborman/uuid"
	"k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions/validator"
	volumeutil "k8s.io/kubernetes/pkg/volume/util"
)

// ShareOptions contains options for provisioning and attaching a share
type ShareOptions struct {
	// Common options

	Zones    string `name:"zones" value:"default:nova"`
	Type     string `name:"type" value:"default:default"`
	Protocol string `name:"protocol" matches:"^(?i)CEPHFS|NFS$"`
	Backend  string `name:"backend" matches:"^(?i)CEPHFS|CSI-CEPHFS|NFS$"`

	OSSecretName         string `name:"osSecretName"`
	OSSecretNamespace    string `name:"osSecretNamespace" value:"default:default"`
	ShareSecretNamespace string `name:"shareSecretNamespace" value:"default:default"`

	OSShareID       string `name:"osShareID" value:"optional" dependsOn:"osShareAccessID"`
	OSShareName     string `name:"osShareName" value:"optional" dependsOn:"osShareAccessID"`
	OSShareAccessID string `name:"osShareAccessID" value:"optional" dependsOn:"osShareID|osShareName"`

	// Backend options

	CSICEPHFSdriver  string `name:"csi-driver" value:"requiredIf:backend=^(?i)csi-cephfs$"`
	CSICEPHFSmounter string `name:"mounter" value:"default:fuse" matches:"^(?i)kernel|fuse$"`

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

	setOfZones, err := volumeutil.ZonesToSet(opts.Zones)
	if err != nil {
		return nil, err
	}

	opts.Zones = volumeutil.ChooseZoneForVolume(setOfZones, volOptions.PVC.GetName())

	return opts, nil
}

/*
// NewShareOptions creates new share options
func NewShareOptions(volOptions *controller.VolumeOptions, c clientset.Interface) (*ShareOptions, error) {
	params := volOptions.Parameters
	opts := &ShareOptions{}
	nParams := len(params)

	opts.ShareName = "pvc-" + string(volOptions.PVC.GetUID())

	var (
		n   int
		err error
	)

	// Required common options
	n, err = extractParams(&optionConstraints{}, params, &opts.CommonOptions)
	if err != nil {
		return nil, err
	}
	nParams -= n

	constraints := optionConstraints{protocol: opts.Protocol, backend: opts.Backend}

	// Protocol specific options
	n, err = extractParams(&constraints, params, &opts.ProtocolOptions)
	if err != nil {
		return nil, err
	}
	nParams -= n

	// Backend specific options
	n, err = extractParams(&constraints, params, &opts.BackendOptions)
	if err != nil {
		return nil, err
	}
	nParams -= n

	if nParams > 0 {
		return nil, fmt.Errorf("parameters contain invalid field(s): %d", nParams)
	}

	setOfZones, err := volumeutil.ZonesToSet(opts.Zones)
	if err != nil {
		return nil, err
	}

	opts.Zones = volumeutil.ChooseZoneForVolume(setOfZones, volOptions.PVC.GetName())

	// Retrieve and parse secrets

	sec, err := readSecrets(c, &v1.SecretReference{
		Name:      opts.OSSecretName,
		Namespace: opts.OSSecretNamespace,
	})
	if err != nil {
		return nil, err
	}

	if err = buildOpenStackOptionsTo(&opts.OpenStackOptions, sec); err != nil {
		return nil, err
	}

	// Share secret name and namespace
	opts.ShareSecretRef = v1.SecretReference{Name: "manila-" + uuid.NewUUID().String(), Namespace: opts.ShareSecretNamespace}

	return opts, nil
}
*/
