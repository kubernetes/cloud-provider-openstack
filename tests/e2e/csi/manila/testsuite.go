package test

import (
	. "github.com/onsi/ginkgo"
	_ "github.com/onsi/gomega"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/testfiles"
	storageframework "k8s.io/kubernetes/test/e2e/storage/framework"
	"k8s.io/kubernetes/test/e2e/storage/testsuites"
	"k8s.io/kubernetes/test/e2e/storage/utils"
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
	testfiles.AddFileSource(testfiles.RootFileSource{Root: framework.TestContext.RepoRoot})

	testDriver := newManilaTestDriver()

	Context(storageframework.GetDriverNameWithFeatureTags(testDriver), func() {
		storageframework.DefineTestSuites(testDriver, CSITestSuites)
	})
})
