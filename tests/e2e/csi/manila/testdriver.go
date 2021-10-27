package test

import (
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/test/e2e/framework"
	e2evolume "k8s.io/kubernetes/test/e2e/framework/volume"
	storageframework "k8s.io/kubernetes/test/e2e/storage/framework"
	"k8s.io/kubernetes/test/e2e/storage/utils"
)

type manilaTestDriver struct {
	driverInfo storageframework.DriverInfo
}

var (
	_ storageframework.TestDriver              = &manilaTestDriver{}
	_ storageframework.DynamicPVTestDriver     = &manilaTestDriver{}
	_ storageframework.SnapshottableTestDriver = &manilaTestDriver{}
)

func newManilaTestDriver(name string) storageframework.TestDriver {
	return &manilaTestDriver{
		driverInfo: storageframework.DriverInfo{
			Name:            name,
			MaxFileSize:     storageframework.FileSizeLarge,
			SupportedFsType: sets.NewString(""),
			SupportedSizeRange: e2evolume.SizeRange{
				Min: "1Gi",
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

func (d *manilaTestDriver) PrepareTest(f *framework.Framework) (*storageframework.PerTestConfig, func()) {
	return &storageframework.PerTestConfig{
		Driver:    d,
		Prefix:    "manila",
		Framework: f,
	}, func() {}
}

//
// storageframework.DynamicPVTestDriver interface implementation
//

func (d *manilaTestDriver) GetDynamicProvisionStorageClass(config *storageframework.PerTestConfig, fsType string) *storagev1.StorageClass {
	parameters := map[string]string{
		"type": "default",
		"csi.storage.k8s.io/provisioner-secret-name":            "csi-manila-secrets",
		"csi.storage.k8s.io/provisioner-secret-namespace":       "default",
		"csi.storage.k8s.io/controller-expand-secret-name":      "csi-manila-secrets",
		"csi.storage.k8s.io/controller-expand-secret-namespace": "default",
		"csi.storage.k8s.io/node-stage-secret-name":             "csi-manila-secrets",
		"csi.storage.k8s.io/node-stage-secret-namespace":        "default",
		"csi.storage.k8s.io/node-publish-secret-name":           "csi-manila-secrets",
		"csi.storage.k8s.io/node-publish-secret-namespace":      "default",
	}
	if fsType != "" {
		parameters["fsType"] = fsType
	}

	return storageframework.GetStorageClass(
		d.driverInfo.Name,
		parameters,
		nil,
		config.Framework.Namespace.Name,
	)
}

//
// storageframework.SnapshottableTestDriver interface implementation
//

func (d *manilaTestDriver) GetSnapshotClass(config *storageframework.PerTestConfig, parameters map[string]string) *unstructured.Unstructured {
	if parameters == nil {
		parameters = make(map[string]string)
	}
	parameters["csi.storage.k8s.io/snapshotter-secret-name"] = "csi-manila-secrets"
	parameters["csi.storage.k8s.io/snapshotter-secret-namespace"] = "default"

	return utils.GenerateSnapshotClassSpec(
		d.driverInfo.Name,
		parameters,
		config.Framework.Namespace.Name,
	)
}
