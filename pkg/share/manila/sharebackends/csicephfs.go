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
)

// CSICephFS struct, implements ShareBackend interface for CSI CephFS
type CSICephFS struct {
	ShareBackend
}

// Name of the backend
func (CSICephFS) Name() string { return "csi-cephfs" }

// BuildSource builds PersistentVolumeSource for CSI CephFS driver
func (CSICephFS) BuildSource(args *BuildSourceArgs) (*v1.PersistentVolumeSource, error) {
	monitors, rootPath, err := splitExportLocation(args.Location)
	if err != nil {
		return nil, err
	}

	return &v1.PersistentVolumeSource{
		CSI: &v1.CSIPersistentVolumeSource{
			Driver:       args.Options.CSICEPHFSdriver,
			ReadOnly:     false,
			VolumeHandle: args.Options.ShareName,
			VolumeAttributes: map[string]string{
				"monitors":        monitors,
				"rootPath":        rootPath,
				"mounter":         "fuse",
				"provisionVolume": "false",
			},
			NodeStageSecretRef: &args.Options.ShareSecretRef,
		},
	}, nil
}

// GrantAccess to Ceph share and creates a k8s Secret
func (CSICephFS) GrantAccess(args *GrantAccessArgs) (*shares.AccessRight, error) {
	accessRight, err := getOrCreateCephxAccess(args)
	if err != nil {
		return nil, err
	}

	err = createSecret(&args.Options.ShareSecretRef, args.Clientset, map[string][]byte{
		"userID":  []byte(accessRight.AccessTo),
		"userKey": []byte(accessRight.AccessKey),
	})

	return accessRight, err
}

// RevokeAccess to k8s secret created by GrantAccess()
func (CSICephFS) RevokeAccess(args *RevokeAccessArgs) error {
	return deleteSecret(args.ShareSecretRef, args.Clientset)
}
