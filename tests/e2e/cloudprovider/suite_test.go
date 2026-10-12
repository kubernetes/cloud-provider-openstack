package cloudprovider

import (
	"flag"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// suiteCtx holds OpenStack and Kubernetes clients shared across all tests.
// Initialized once in BeforeSuite to avoid re-authenticating per test.
var suiteCtx *testContext

func TestCloudProvider(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	// Parse flags before running specs
	flag.Parse()

	ginkgo.BeforeSuite(func(ctx ginkgo.SpecContext) {
		var err error
		suiteCtx, err = setupTestContext(ctx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = createNamespace(ctx, suiteCtx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = createDeployment(ctx, suiteCtx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.AfterSuite(func(ctx ginkgo.SpecContext) {
		if suiteCtx == nil {
			return
		}
		if autoCleanup {
			deleteDeployment(ctx, suiteCtx)
		}
		if suiteCtx.logFile != nil {
			suiteCtx.logFile.Close()
		}
	})

	ginkgo.RunSpecs(t, "CloudProvider E2E Suite")
}
