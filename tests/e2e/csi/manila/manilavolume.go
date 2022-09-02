package test

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"k8s.io/kubernetes/test/e2e/framework"
	storageframework "k8s.io/kubernetes/test/e2e/storage/framework"
)

func runCmd(name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	framework.Logf("Running %s %v", cmd.Path, cmd.Args)
	err := cmd.Run()

	framework.Logf("Stdout output %s %v: `%s`", cmd.Path, cmd.Args, stdout.String())
	framework.Logf("Stderr output %s %v: `%s`", cmd.Path, cmd.Args, stderr.String())

	return stdout.Bytes(), err
}

// It is assummed that the `openstack` and other related programs
// are accessible from $PATH on the node.

type manilaVolume struct {
	shareID  string
	accessID string
}

func manilaCreateVolume(
	shareProto,
	accessType,
	accessTo string,
	sizeInGiB int,
	config *storageframework.PerTestConfig,
) storageframework.TestVolume {
	ginkgo.By("Creating a test Manila volume externally")

	// Create share.

	out, err := runCmd(
		"openstack",
		"share",
		"create",
		shareProto,
		strconv.Itoa(sizeInGiB),
		"--property=provisioned-by=manila.csi.openstack.org/e2e.test",
		"--format=value",
		"--column=id",
		"--wait",
	)

	shareID := strings.TrimSpace(string(out))

	framework.ExpectNoError(err)
	framework.ExpectNotEqual(shareID, "")

	framework.Logf("Created test Manila volume %s", shareID)

	// Grant access to the share.

	out, err = runCmd(
		"openstack",
		"share",
		"access",
		"create",
		shareID,
		accessType,
		accessTo,
		"--format=value",
		"--column=id",
	)

	// XXX: In case of cephx access rights, the access_key field might
	//      not be generated in time for when the volume is mounted.
	//      Tests may fail. A workaround would be to wait until it is ready.

	accessID := strings.TrimSpace(string(out))

	framework.ExpectNoError(err)
	framework.ExpectNotEqual(accessID, "")

	framework.Logf("Created access right %s for Manila volume %s", accessID, shareID)

	return &manilaVolume{
		shareID:  shareID,
		accessID: accessID,
	}
}

func (v *manilaVolume) DeleteVolume() {
	ginkgo.By("Deleting test Manila volume externally")

	_, err := runCmd(
		"openstack",
		"share",
		"delete",
		v.shareID,
	)

	if err != nil {
		framework.Failf("Failed to remove Manila volume %s: %v", v.shareID, err)
	}
}
