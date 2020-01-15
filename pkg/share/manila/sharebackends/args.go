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

package sharebackends

import (
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions"
)

// BuildSourceArgs contains arguments for ShareBackend.BuildSource()
type BuildSourceArgs struct {
	VolumeHandle   string
	Share          *shares.Share
	Options        *shareoptions.ShareOptions
	ShareSecretRef *v1.SecretReference
	Location       *shares.ExportLocation
	Clientset      clientset.Interface
	AccessRight    *shares.AccessRight
}

// GrantAccessArgs contains arguments for ShareBackend.GrantAccess()
type GrantAccessArgs struct {
	Share          *shares.Share
	Options        *shareoptions.ShareOptions
	ShareSecretRef *v1.SecretReference
	Clientset      clientset.Interface
	Client         manilaclient.Interface
}

// RevokeAccessArgs contains arguments for ShareBackend.RevokeAccess()
type RevokeAccessArgs struct {
	ShareID        string
	ShareSecretRef *v1.SecretReference
	Clientset      clientset.Interface
	Client         manilaclient.Interface
}
