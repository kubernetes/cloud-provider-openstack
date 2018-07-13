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

	"github.com/kubernetes-incubator/external-storage/lib/controller"
	"github.com/pborman/uuid"
	"k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	volumeutil "k8s.io/kubernetes/pkg/volume/util"
)

// ShareOptions contains options for provisioning and attaching a share
type ShareOptions struct {
	ShareName string // Used in CreateOpts when creating a new share

	// If applicable, to be used when creating a k8s Secret in GrantAccess.
	// The same SecretReference will be retrieved for RevokeAccess.
	ShareSecretRef v1.SecretReference

	CommonOptions    // Required common options
	ProtocolOptions  // Protocol specific options
	BackendOptions   // Backend specific options
	OpenStackOptions // OpenStack credentials
}

func init() {
	processStruct(&CommonOptions{})
	processStruct(&ProtocolOptions{})
	processStruct(&BackendOptions{})
	processStruct(&OpenStackOptions{})
}

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
