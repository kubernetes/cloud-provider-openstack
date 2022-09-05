package test

import (
	// revive:disable:dot-imports

	// revive:enable:dot-imports
	// revive:disable:blank-imports
	"github.com/onsi/ginkgo/v2"
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

	ginkgo.Context(storageframework.GetDriverNameWithFeatureTags(testDriver), func() {
		storageframework.DefineTestSuites(testDriver, CSITestSuites)
	})
})
