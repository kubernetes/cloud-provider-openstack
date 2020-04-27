package sanity

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

// start sanity test for driver
func TestDriver(t *testing.T) {
	basePath, err := ioutil.TempDir("", "cinder.csi.openstack.org")
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(basePath)

	socket := path.Join(basePath, "csi.sock")
	endpoint := "unix://" + socket
	cluster := "kubernetes"
	nodeID := "fake-node"

	d := cinder.NewDriver(nodeID, endpoint, cluster)
	fakecloudprovider := getfakecloud()
	openstack.OsInstance = fakecloudprovider
	fakemnt := &fakemount{}
	fakemet := &fakemetadata{}

	d.SetupDriver(fakecloudprovider, fakemnt, fakemet)

	// TODO: Stop call

	go d.Run()

	config := &sanity.Config{
		TargetPath:  path.Join(basePath, "mnt"),
		StagingPath: path.Join(basePath, "mnt-stage"),
		Address:     endpoint,
	}

	sanity.Test(t, config)
}
