package cloudprovider

import (
	"flag"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestCloudProvider(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	// Parse flags before running specs
	flag.Parse()

	ginkgo.RunSpecs(t, "CloudProvider E2E Suite")
}
