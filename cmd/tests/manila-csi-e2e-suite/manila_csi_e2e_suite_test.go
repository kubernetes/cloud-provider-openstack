package main

import (
	"flag"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	_ "k8s.io/cloud-provider-openstack/tests/e2e/csi/manila"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e/framework"
	frameworkconfig "k8s.io/kubernetes/test/e2e/framework/config"
)

func init() {
	frameworkconfig.CopyFlags(frameworkconfig.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
	framework.AfterReadingAllFlags(&framework.TestContext)
}

func Test(t *testing.T) {
	gomega.RegisterFailHandler(framework.Fail)
	if framework.TestContext.ReportDir != "" {
		if err := os.MkdirAll(framework.TestContext.ReportDir, 0755); err != nil {
			klog.Fatalf("Failed creating report directory: %v", err)
		}
	}
	klog.Infof("Starting e2e run %q on Ginkgo node %d", framework.RunID, ginkgo.GinkgoParallelProcess())

	suiteConfig, reporterConfig := framework.CreateGinkgoConfig()
	reporterConfig.FullTrace = true

	klog.Infof("Starting e2e run %q on Ginkgo node %d", framework.RunID, suiteConfig.ParallelProcess)
	ginkgo.RunSpecs(t, "Manila CSI e2e Suite", suiteConfig, reporterConfig)

}

func main() {
	flag.Parse()
	Test(&testing.T{})
}
