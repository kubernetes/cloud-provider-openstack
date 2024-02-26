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

	d := cinder.NewDriver(&cinder.DriverOpts{Endpoint: endpoint, ClusterID: cluster})

	fakecloudprovider := getfakecloud()
	openstack.OsInstances = map[string]openstack.IOpenStack{
		"": fakecloudprovider,
	}

	fakemnt := GetFakeMountProvider()
	fakemet := &fakemetadata{}

	d.SetupControllerService(openstack.OsInstances)
	d.SetupNodeService(fakecloudprovider, fakemnt, fakemet, map[string]string{})

	// TODO: Stop call

	go d.Run()

	config := sanity.NewTestConfig()
	config.Address = endpoint
	sanity.Test(t, config)
}
