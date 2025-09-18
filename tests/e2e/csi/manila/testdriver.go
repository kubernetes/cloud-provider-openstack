package test

import (
	"context"
	"fmt"

	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/test/e2e/framework"
	e2evolume "k8s.io/kubernetes/test/e2e/framework/volume"
	storageframework "k8s.io/kubernetes/test/e2e/storage/framework"
	"k8s.io/kubernetes/test/e2e/storage/utils"
)

const (
	driverName = "nfs.manila.csi.openstack.org"

	// Common secret for all operations that access Manila API.
	manilaSecretName      = "csi-manila-secrets"
	manilaSecretNamespace = "default"

	// Parameters used for volume provisioning.
	manilaShareProto      = "NFS"
	manilaShareType       = "default"
	manilaShareAccessType = "ip"
	manilaShareAccessTo   = "0.0.0.0/0"
	manilaShareSizeGiB    = 1
)

type manilaTestDriver struct {
	driverInfo       storageframework.DriverInfo
	volumeAttributes []map[string]string
}

var (
	_ storageframework.TestDriver                 = &manilaTestDriver{}
	_ storageframework.DynamicPVTestDriver        = &manilaTestDriver{}
	_ storageframework.SnapshottableTestDriver    = &manilaTestDriver{}
	_ storageframework.EphemeralTestDriver        = &manilaTestDriver{}
	_ storageframework.PreprovisionedPVTestDriver = &manilaTestDriver{}
)

func newManilaTestDriver() storageframework.TestDriver {
	return &manilaTestDriver{
		driverInfo: storageframework.DriverInfo{
			Name: driverName,
			// Either MaxFileSize needs to be set to FileSizeMedium at most, or larger volumes
			// need to be created -- otherwise VolumeIOTestSuite fails.
			MaxFileSize:     storageframework.FileSizeMedium,
			SupportedFsType: sets.NewString(""),
			SupportedSizeRange: e2evolume.SizeRange{
				Min: fmt.Sprintf("%dGi", manilaShareSizeGiB),
			},
			Capabilities: map[storageframework.Capability]bool{
				storageframework.CapPersistence:         true,
				storageframework.CapFsGroup:             true,
				storageframework.CapExec:                true,
				storageframework.CapMultiPODs:           true,
				storageframework.CapRWX:                 true,
				storageframework.CapSnapshotDataSource:  true,
				storageframework.CapControllerExpansion: true,
				storageframework.CapOnlineExpansion:     true,
			},
		},
		volumeAttributes: []map[string]string{
			{
				"type": manilaShareType,
				"csi.storage.k8s.io/provisioner-secret-name":            manilaSecretName,
				"csi.storage.k8s.io/provisioner-secret-namespace":       manilaSecretNamespace,
				"csi.storage.k8s.io/controller-expand-secret-name":      manilaSecretName,
				"csi.storage.k8s.io/controller-expand-secret-namespace": manilaSecretNamespace,
				"csi.storage.k8s.io/node-stage-secret-name":             manilaSecretName,
				"csi.storage.k8s.io/node-stage-secret-namespace":        manilaSecretNamespace,
				"csi.storage.k8s.io/node-publish-secret-name":           manilaSecretName,
				"csi.storage.k8s.io/node-publish-secret-namespace":      manilaSecretNamespace,
			},
		},
	}
}

func validateVolumeType(volumeType storageframework.TestVolType) {
	unsupported := []storageframework.TestVolType{
		storageframework.CSIInlineVolume,
	}

	for _, t := range unsupported {
		if t == volumeType {
			framework.Failf("Unsupported test volume type %s", t)
		}
	}
}

//
// storageframework.TestDriver interface implementation
//

func (d *manilaTestDriver) GetDriverInfo() *storageframework.DriverInfo {
	return &d.driverInfo
}

func (d *manilaTestDriver) SkipUnsupportedTest(storageframework.TestPattern) {
}

func (d *manilaTestDriver) PrepareTest(ctx context.Context, f *framework.Framework) *storageframework.PerTestConfig {
	return &storageframework.PerTestConfig{
		Driver:    d,
		Prefix:    "manila",
		Framework: f,
	}
}

//
// storageframework.DynamicPVTestDriver interface implementation
//

func (d *manilaTestDriver) GetDynamicProvisionStorageClass(ctx context.Context, config *storageframework.PerTestConfig, fsType string) *storagev1.StorageClass {
	parameters := map[string]string{
		"type": manilaShareType,
		"csi.storage.k8s.io/provisioner-secret-name":            manilaSecretName,
		"csi.storage.k8s.io/provisioner-secret-namespace":       manilaSecretNamespace,
		"csi.storage.k8s.io/controller-expand-secret-name":      manilaSecretName,
		"csi.storage.k8s.io/controller-expand-secret-namespace": manilaSecretNamespace,
		"csi.storage.k8s.io/node-stage-secret-name":             manilaSecretName,
		"csi.storage.k8s.io/node-stage-secret-namespace":        manilaSecretNamespace,
		"csi.storage.k8s.io/node-publish-secret-name":           manilaSecretName,
		"csi.storage.k8s.io/node-publish-secret-namespace":      manilaSecretNamespace,
	}

	sc := storageframework.GetStorageClass(
		d.driverInfo.Name,
		parameters,
		nil,
		config.Framework.Namespace.Name,
	)

	allowVolumeExpansion := true
	sc.AllowVolumeExpansion = &allowVolumeExpansion

	return sc
}

//
// storageframework.SnapshottableTestDriver interface implementation
//

func (d *manilaTestDriver) GetSnapshotClass(ctx context.Context, config *storageframework.PerTestConfig, parameters map[string]string) *unstructured.Unstructured {
	if parameters == nil {
		parameters = make(map[string]string)
	}
	parameters["csi.storage.k8s.io/snapshotter-secret-name"] = manilaSecretName
	parameters["csi.storage.k8s.io/snapshotter-secret-namespace"] = manilaSecretNamespace

	return utils.GenerateSnapshotClassSpec(
		d.driverInfo.Name,
		parameters,
		config.Framework.Namespace.Name,
	)
}

//
// storageframework.EphemeralTestDriver interface implementation
//

func (d *manilaTestDriver) GetVolume(config *storageframework.PerTestConfig, volumeNumber int) (attributes map[string]string, shared bool, readOnly bool) {
	return d.volumeAttributes[volumeNumber%len(d.volumeAttributes)], true /* shared */, false /* readOnly */
}

func (d *manilaTestDriver) GetCSIDriverName(config *storageframework.PerTestConfig) string {
	return d.driverInfo.Name
}

//
// storageframework.PreprovisionedVolumeTestDriver interface implementation
//

func (d *manilaTestDriver) CreateVolume(ctx context.Context, config *storageframework.PerTestConfig, volumeType storageframework.TestVolType) storageframework.TestVolume {
	validateVolumeType(volumeType)

	return manilaCreateVolume(manilaShareProto, manilaShareAccessType, manilaShareAccessTo, manilaShareSizeGiB, config)
}

//
// storageframework.PreprovisionedPVTestDriver interface implementation
//

func (d *manilaTestDriver) GetPersistentVolumeSource(readOnly bool, fsType string, testVolume storageframework.TestVolume) (*v1.PersistentVolumeSource, *v1.VolumeNodeAffinity) {
	v, ok := testVolume.(*manilaVolume)
	gomega.Expect(ok).To(gomega.BeTrue(), "Failed to cast test volume to Manila test volume")

	return &v1.PersistentVolumeSource{
		CSI: &v1.CSIPersistentVolumeSource{
			Driver:       d.driverInfo.Name,
			VolumeHandle: v.shareID,
			ReadOnly:     readOnly,
			FSType:       fsType,
			VolumeAttributes: map[string]string{
				"shareID":        v.shareID,
				"shareAccessIDs": v.accessID,
			},
			NodeStageSecretRef: &v1.SecretReference{
				Name:      manilaSecretName,
				Namespace: manilaSecretNamespace,
			},
			NodePublishSecretRef: &v1.SecretReference{
				Name:      manilaSecretName,
				Namespace: manilaSecretNamespace,
			},
		},
	}, nil
}
