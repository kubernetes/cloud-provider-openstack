package test

import (
	// revive:disable:dot-imports
	. "github.com/onsi/ginkgo"
	// revive:enable:dot-imports
	// revive:disable:blank-imports
	_ "github.com/onsi/gomega"
	// revive:enable:blank-imports

	"k8s.io/kubernetes/test/e2e/framework/testfiles"
	storageframework "k8s.io/kubernetes/test/e2e/storage/framework"
	"k8s.io/kubernetes/test/e2e/storage/testsuites"
	"k8s.io/kubernetes/test/e2e/storage/utils"
	e2etestingmanifests "k8s.io/kubernetes/test/e2e/testing-manifests"
)

var CSITestSuites = []func() storageframework.TestSuite{
	testsuites.InitVolumesTestSuite,
	testsuites.InitSnapshottableTestSuite,
	testsuites.InitProvisioningTestSuite,
	testsuites.InitSubPathTestSuite,
	testsuites.InitVolumeModeTestSuite,
	testsuites.InitVolumeExpandTestSuite,
	testsuites.InitVolumeIOTestSuite,
}

var _ = utils.SIGDescribe("[manila-csi-e2e] CSI Volumes", func() {
	testfiles.AddFileSource(e2etestingmanifests.GetE2ETestingManifestsFS())

	testDriver := newManilaTestDriver()

	Context(storageframework.GetDriverNameWithFeatureTags(testDriver), func() {
		storageframework.DefineTestSuites(testDriver, CSITestSuites)
	})
})
