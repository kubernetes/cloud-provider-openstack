package sanity

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"
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
	nodeID := "45678"

	d := cinder.NewDriver(nodeID, endpoint, cluster)
	c := getfakecloud()
	fakemnt := &fakemount{}
	fakemet := &fakemetadata{}

	d.SetupDriver(c, fakemnt, fakemet)

	// TODO: Stop call

	go d.Run()

	config := &sanity.Config{
		TargetPath:  path.Join(basePath, "mnt"),
		StagingPath: path.Join(basePath, "mnt-stage"),
		Address:     endpoint,
	}

	sanity.Test(t, config)
}
