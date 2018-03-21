/*
Copyright 2016 The Kubernetes Authors.

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

// The external controller manager is responsible for running controller loops that
// are cloud provider dependent. It uses the API to listen to new events on resources.

package main

import (
	goflag "flag"
	"fmt"
	"os"

	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/kubernetes/cmd/cloud-controller-manager/app"
	"k8s.io/kubernetes/cmd/cloud-controller-manager/app/options"
	_ "k8s.io/kubernetes/pkg/client/metrics/prometheus" // for client metric registration
	_ "k8s.io/kubernetes/pkg/version/prometheus"        // for version metric registration
	"k8s.io/kubernetes/pkg/version/verflag"

	"git.openstack.org/openstack/openstack-cloud-controller-manager/pkg/cloudprovider/providers/openstack"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

var version string

func init() {
	healthz.DefaultHealthz()
}

func main() {
	s := options.NewCloudControllerManagerServer()
	s.AddFlags(pflag.CommandLine)

	goflag.CommandLine.Parse([]string{})
	flag.InitFlags()
	logs.InitLogs()
	defer logs.FlushLogs()

	glog.V(1).Infof("openstack-cloud-controller-manager version: %s", version)

	verflag.PrintAndExitIfRequested()

	s.CloudProvider = openstack.ProviderName
	if err := app.Run(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
