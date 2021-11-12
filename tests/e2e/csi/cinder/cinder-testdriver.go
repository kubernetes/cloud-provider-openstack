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

type cinderDriver struct {
	driverInfo       storageframework.DriverInfo
	manifests        []string
	volumeAttributes []map[string]string
}

var Cinderdriver = InitCinderDriver

type cinderVolume struct {
	ID               string
	Name             string
	Status           string
	AvailabilityZone string
	f                *framework.Framework
}

// initCinderDriver returns cinderDriver that implements TestDriver interface
func initCinderDriver(name string, manifests ...string) storageframework.TestDriver {
	return &cinderDriver{
		driverInfo: storageframework.DriverInfo{
			Name:        name,
			MaxFileSize: storageframework.FileSizeLarge,
			SupportedFsType: sets.NewString(
				"", // Default fsType
				"ext2",
				"ext3",
				"ext4",
				"xfs",
			),
			SupportedSizeRange: e2evolume.SizeRange{
				Min: "1Gi",
			},
			TopologyKeys: []string{
				"topology.cinder.csi.openstack.org/zone",
			},
			Capabilities: map[storageframework.Capability]bool{
				storageframework.CapPersistence:         true,
				storageframework.CapFsGroup:             true,
				storageframework.CapExec:                true,
				storageframework.CapMultiPODs:           true,
				storageframework.CapBlock:               true,
				storageframework.CapPVCDataSource:       true,
				storageframework.CapTopology:            true,
				storageframework.CapControllerExpansion: true,
				storageframework.CapNodeExpansion:       true,
				//storageframework.CapSnapshotDataSource: true,

			},
		},
		manifests: manifests,
		volumeAttributes: []map[string]string{
			{"capacity": "1Gi"},
		},
	}
}

func InitCinderDriver() storageframework.TestDriver {

	return initCinderDriver("cinder.csi.openstack.org",
		"cinder-csi-controllerplugin.yaml",
		"cinder-csi-controllerplugin-rbac.yaml",
		"cinder-csi-nodeplugin.yaml",
		"cinder-csi-nodeplugin-rbac.yaml",
		"csi-secret-cinderplugin.yaml")
}

var (
	_ storageframework.TestDriver              = &cinderDriver{}
	_ storageframework.DynamicPVTestDriver     = &cinderDriver{}
	_ storageframework.SnapshottableTestDriver = &cinderDriver{}
	_ storageframework.EphemeralTestDriver     = &cinderDriver{}
)

func (d *cinderDriver) GetDriverInfo() *storageframework.DriverInfo {
	return &d.driverInfo
}

func (d *cinderDriver) SkipUnsupportedTest(pattern storageframework.TestPattern) {
}

func (d *cinderDriver) GetDynamicProvisionStorageClass(config *storageframework.PerTestConfig, fsType string) *storagev1.StorageClass {
	provisioner := "cinder.csi.openstack.org"
	parameters := map[string]string{}
	if fsType != "" {
		parameters["fsType"] = fsType
	}
	ns := config.Framework.Namespace.Name
	return storageframework.GetStorageClass(provisioner, parameters, nil, ns)
}

func (d *cinderDriver) GetSnapshotClass(config *storageframework.PerTestConfig, parameters map[string]string) *unstructured.Unstructured {
	snapshotter := d.driverInfo.Name
	ns := config.Framework.Namespace.Name
	return utils.GenerateSnapshotClassSpec(snapshotter, parameters, ns)
}

func (d *cinderDriver) GetClaimSize() string {
	return "2Gi"
}

func (d *cinderDriver) GetVolume(config *storageframework.PerTestConfig, volumeNumber int) (map[string]string, bool, bool) {
	return d.volumeAttributes[volumeNumber%len(d.volumeAttributes)], false /* not shared */, false /* read-write */
}

func (d *cinderDriver) GetCSIDriverName(config *storageframework.PerTestConfig) string {
	return d.driverInfo.Name
}

func (d *cinderDriver) PrepareTest(f *framework.Framework) (*storageframework.PerTestConfig, func()) {
	config := &storageframework.PerTestConfig{
		Driver:    d,
		Prefix:    "cinder",
		Framework: f,
	}

	return config, func() {}
}
