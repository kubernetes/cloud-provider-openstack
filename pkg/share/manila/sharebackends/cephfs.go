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
	"strings"

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/api/core/v1"
)

// CephFS struct, implements ShareBackend interface for k8s CephFS
type CephFS struct {
	ShareBackend
}

// Name of the backend
func (CephFS) Name() string { return "cephfs" }

// BuildSource builds PersistentVolumeSource for k8s CephFS
func (CephFS) BuildSource(args *BuildSourceArgs) (*v1.PersistentVolumeSource, error) {
	monitorsStr, path, err := splitExportLocation(args.Location)
	if err != nil {
		return nil, err
	}

	monitors := strings.Split(monitorsStr, ",")

	return &v1.PersistentVolumeSource{
		CephFS: &v1.CephFSPersistentVolumeSource{
			Monitors:  monitors,
			Path:      path,
			ReadOnly:  false,
			User:      args.AccessRight.AccessTo,
			SecretRef: args.ShareSecretRef,
		},
	}, nil
}

// GrantAccess to Ceph share
func (CephFS) GrantAccess(args *GrantAccessArgs) (*shares.AccessRight, error) {
	accessRight, err := getOrCreateCephxAccess(args)
	if err != nil {
		return nil, err
	}

	err = createSecret(args.ShareSecretRef, args.Clientset, map[string][]byte{
		"key": []byte(accessRight.AccessKey),
	})

	return accessRight, err
}

// RevokeAccess to k8s secret created by GrantAccess()
func (CephFS) RevokeAccess(args *RevokeAccessArgs) error {
	return deleteSecret(args.ShareSecretRef, args.Clientset)
}
