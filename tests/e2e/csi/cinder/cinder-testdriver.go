package test

import (
	"fmt"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/test/e2e/framework"
	e2evolume "k8s.io/kubernetes/test/e2e/framework/volume"
	"k8s.io/kubernetes/test/e2e/storage/testpatterns"
	"k8s.io/kubernetes/test/e2e/storage/testsuites"
)

type cinderDriver struct {
	driverInfo testsuites.DriverInfo
	manifests  []string
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
func initCinderDriver(name string, manifests ...string) testsuites.TestDriver {
	return &cinderDriver{
		driverInfo: testsuites.DriverInfo{
			Name:        name,
			MaxFileSize: testpatterns.FileSizeLarge,
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
			Capabilities: map[testsuites.Capability]bool{
				testsuites.CapPersistence:        true,
				testsuites.CapFsGroup:            true,
				testsuites.CapExec:               true,
				testsuites.CapMultiPODs:          true,
				testsuites.CapBlock:              true,
				testsuites.CapSnapshotDataSource: true,
				testsuites.CapPVCDataSource:      true,
				testsuites.CapTopology:           true,
			},
			TopologyKeys: []string{
				"topology.cinder.csi.openstack.org/zone",
			},
		},
		manifests: manifests,
	}
}

func InitCinderDriver() testsuites.TestDriver {
	return initCinderDriver("cinder.csi.openstack.org",
		"cinder-csi-controllerplugin.yaml",
		"cinder-csi-controllerplugin-rbac.yaml",
		"cinder-csi-nodeplugin.yaml",
		"cinder-csi-nodeplugin-rbac.yaml",
		"csi-secret-cinderplugin.yaml")
}

// var _ testsuites.PreprovisionedVolumeTestDriver = &cinderDriver{}
// var _ testsuites.PreprovisionedPVTestDriver = &cinderDriver{}
var (
	_ testsuites.DynamicPVTestDriver     = &cinderDriver{}
	_ testsuites.SnapshottableTestDriver = &cinderDriver{}
	_ testsuites.TestDriver              = &cinderDriver{}
)

func (d *cinderDriver) GetDriverInfo() *testsuites.DriverInfo {
	return &d.driverInfo
}

func (d *cinderDriver) SkipUnsupportedTest(pattern testpatterns.TestPattern) {
}

func (d *cinderDriver) GetDynamicProvisionStorageClass(config *testsuites.PerTestConfig, fsType string) *storagev1.StorageClass {
	provisioner := "cinder.csi.openstack.org"
	parameters := map[string]string{}
	if fsType != "" {
		parameters["fsType"] = fsType
	}
	ns := config.Framework.Namespace.Name
	suffix := fmt.Sprintf("%s-sc", d.driverInfo.Name)

	return testsuites.GetStorageClass(provisioner, parameters, nil, ns, suffix)
}

func (d *cinderDriver) GetSnapshotClass(config *testsuites.PerTestConfig) *unstructured.Unstructured {
	parameters := map[string]string{}
	snapshotter := d.driverInfo.Name
	suffix := fmt.Sprintf("%s-vsc", snapshotter)
	ns := config.Framework.Namespace.Name
	return testsuites.GetSnapshotClass(snapshotter, parameters, ns, suffix)
}

func (d *cinderDriver) GetClaimSize() string {
	return "2Gi"
}

func (d *cinderDriver) PrepareTest(f *framework.Framework) (*testsuites.PerTestConfig, func()) {
	config := &testsuites.PerTestConfig{
		Driver:    d,
		Prefix:    "cinder",
		Framework: f,
	}

	return config, func() {}
}
