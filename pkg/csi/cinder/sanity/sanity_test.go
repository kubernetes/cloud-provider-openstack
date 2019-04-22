package sanity

import (
	"io/ioutil"
	"os"
	"testing"

	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"
)

//start sanity test for driver
func TestDriver(t *testing.T) {

	socket := "/tmp/csi.sock"
	endpoint := "unix://" + socket
	cluster := "kubernetes"
	nodeID := "45678"

	d := cinder.NewDriver(nodeID, endpoint, cluster)
	c := &cloud{}
	fakemnt := &fakemount{}
	fakemet := &fakemetadata{}

	d.SetupDriver(c, fakemnt, fakemet)

	//TODO: Stop call

	go d.Run()

	mntDir, err := ioutil.TempDir("", "mnt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mntDir)

	mntStageDir, err := ioutil.TempDir("", "mnt-stage")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mntStageDir)

	config := &sanity.Config{
		TargetPath:  mntDir,
		StagingPath: mntStageDir,
		Address:     endpoint,
	}

	sanity.Test(t, config)
}
