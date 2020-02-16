/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sanity

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
)

func TestDriver(t *testing.T) {
	basePath, err := ioutil.TempDir("", "manila.csi.openstack.org")
	if err != nil {
		t.Fatalf("failed create base path in %s: %v", basePath, err)
	}

	defer os.RemoveAll(basePath)

	endpoint := path.Join(basePath, "csi.sock")
	fwdEndpoint := "unix:///fake-fwd-endpoint"

	d, err := manila.NewDriver(
		&manila.DriverOpts{
			DriverName:          "fake.manila.csi.openstack.org",
			NodeID:              "node",
			WithTopology:        true,
			NodeAZ:              "fake-az",
			ShareProto:          "NFS",
			ServerCSIEndpoint:   endpoint,
			FwdCSIEndpoint:      fwdEndpoint,
			ManilaClientBuilder: &fakeManilaClientBuilder{},
			CSIClientBuilder:    &fakeCSIClientBuilder{},
			CompatOpts:          &options.CompatibilityOptions{},
		})

	if err != nil {
		t.Fatalf("failed to initialize CSI Manila driver: %v", err)
	}

	go d.Run()

	sanity.Test(t, &sanity.Config{
		Address:     endpoint,
		SecretsFile: "fake-secrets.yaml",
		TargetPath:  path.Join(basePath, "target"),
		StagingPath: path.Join(basePath, "staging"),
	})
}
