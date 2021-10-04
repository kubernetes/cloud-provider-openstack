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
	// testsuites.InitSubPathTestSuite,
	// testsuites.InitProvisioningTestSuite,
	// testsuites.InitVolumeModeTestSuite,
	// testsuites.InitSnapshottableTestSuite,
	// testsuites.InitFsGroupChangePolicyTestSuite,
}

var _ = utils.SIGDescribe("[manila-csi-e2e] CSI Volumes", func() {
	testfiles.AddFileSource(testfiles.RootFileSource{Root: framework.TestContext.RepoRoot})

	testDriver := newManilaTestDriver(
		"nfs.manila.csi.openstack.org",
	)

	Context(storageframework.GetDriverNameWithFeatureTags(testDriver), func() {
		storageframework.DefineTestSuites(testDriver, CSITestSuites)
	})
})
