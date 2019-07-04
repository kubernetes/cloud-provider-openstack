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

package main

import (
	goflag "flag"

	flag "github.com/spf13/pflag"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila"
	"k8s.io/klog"
)

var (
	endpoint      = flag.String("endpoint", "unix://tmp/csi.sock", "CSI endpoint")
	driverName    = flag.String("drivername", "manila.csi.openstack.org", "name of the driver")
	nodeID        = flag.String("nodeid", "", "this node's ID")
	protoSelector = flag.String("share-protocol-selector", "", "specifies which Manila share protocol to use")
	fwdEndpoint   = flag.String("fwdendpoint", "", "CSI Node Plugin endpoint to which all Node Service RPCs are forwarded. Must be able to handle the file-system specified in share-protocol-selector")
)

func main() {
	klog.InitFlags(nil)
	if err := goflag.Set("logtostderr", "true"); err != nil {
		klog.Exitf("failed to set logtostderr flag: %v", err)
	}
	manila.AddExtraFlags(flag.CommandLine)
	flag.Parse()

	d, err := manila.NewDriver(*nodeID, *driverName, *endpoint, *fwdEndpoint, *protoSelector)
	if err != nil {
		klog.Fatalf("driver initialization failed: %v", err)
	}

	d.Run()
}
