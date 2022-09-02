package sanity

import (
	"os"
	"path"
	"testing"

	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

// start sanity test for driver
func TestDriver(t *testing.T) {
	basePath := os.TempDir()
	defer os.Remove(basePath)

	socket := path.Join(basePath, "csi.sock")
	endpoint := "unix://" + socket
	cluster := "kubernetes"

	d := cinder.NewDriver(endpoint, cluster)
	fakecloudprovider := getfakecloud()
	openstack.OsInstance = fakecloudprovider

	fakemnt := GetFakeMountProvider()
	fakemet := &fakemetadata{}

	d.SetupDriver(fakecloudprovider, fakemnt, fakemet)

	// TODO: Stop call

	go d.Run()

	config := sanity.NewTestConfig()
	config.Address = endpoint
	sanity.Test(t, config)
}
