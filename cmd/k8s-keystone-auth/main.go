/*
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
	"os"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	kflag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/cloud-provider-openstack/pkg/identity/keystone"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()

	config := keystone.NewConfig()
	config.AddFlags(pflag.CommandLine)
	kflag.InitFlags()

	if err := config.ValidateFlags(); err != nil {
		glog.Errorf("%v", err)
		os.Exit(1)
	}

	keystoneAuth, err := keystone.NewKeystoneAuth(config)
	if err != nil {
		glog.Errorf("%v", err)
		os.Exit(1)
	}
	keystoneAuth.Run()
}
