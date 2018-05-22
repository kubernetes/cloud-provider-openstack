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
			Monitors: monitors,
			Path:     path,
			ReadOnly: false,
			User:     args.AccessRight.AccessTo,
			SecretRef: &v1.SecretReference{
				Name:      getSecretName(args.Share.ID),
				Namespace: args.Options.OSSecretNamespace,
			},
		},
	}, nil
}

// GrantAccess to Ceph share
func (CephFS) GrantAccess(args *GrantAccessArgs) (*shares.AccessRight, error) {
	accessRight, err := grantAccessCephx(args)
	if err != nil {
		return nil, err
	}

	err = createSecret(getSecretName(args.Share.ID), args.Options.OSSecretNamespace, args.Clientset, map[string][]byte{
		"key": []byte(accessRight.AccessKey),
	})

	return accessRight, err
}

// RevokeAccess to k8s secret created by GrantAccess()
func (CephFS) RevokeAccess(args *RevokeAccessArgs) error {
	return deleteSecret(getSecretName(args.ShareID), args.SecretNamespace, args.Clientset)
}
