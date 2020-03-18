package test

import (
	"path"

	. "github.com/onsi/ginkgo"
	_ "github.com/onsi/gomega"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/testfiles"
	"k8s.io/kubernetes/test/e2e/storage/testsuites"
	"k8s.io/kubernetes/test/e2e/storage/utils"
)

var CSITestSuites = []func() testsuites.TestSuite{
	testsuites.InitVolumesTestSuite,
	testsuites.InitSubPathTestSuite,
	testsuites.InitProvisioningTestSuite,
	testsuites.InitVolumeModeTestSuite,
	//testsuites.InitVolumeIOTestSuite,
	//testsuites.InitSnapshottableTestSuite,
	//testsuites.InitMultiVolumeTestSuite,
}

// This executes testSuites for csi volumes.
var _ = utils.SIGDescribe("[cinder-csi-e2e] CSI Volumes", func() {
	testfiles.AddFileSource(testfiles.RootFileSource{Root: path.Join(framework.TestContext.RepoRoot, "../../manifests/cinder-csi-plugin/")})

	curDriver := Cinderdriver()
	Context(testsuites.GetDriverNameWithFeatureTags(curDriver), func() {
		testsuites.DefineTestSuite(curDriver, CSITestSuites)
	})

})
