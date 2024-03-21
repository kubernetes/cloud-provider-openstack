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

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-openstack/pkg/identity/keystone"
	"k8s.io/cloud-provider-openstack/pkg/version"
)

var config = keystone.NewConfig()

func main() {
	cmd := &cobra.Command{
		Use:   "k8s-keystone-auth",
		Short: "Keystone authentication webhook plugin for Kubernetes",
		Run: func(cmd *cobra.Command, args []string) {
			if err := config.ValidateFlags(); err != nil {
				klog.Errorf("%v", err)
				os.Exit(1)
			}

			keystoneAuth, err := keystone.NewKeystoneAuth(config)
			if err != nil {
				klog.Errorf("%v", err)
				os.Exit(1)
			}
			keystoneAuth.Run()

		},
		Version: version.Version,
	}

	keystone.AddExtraFlags(pflag.CommandLine)

	config.AddFlags(pflag.CommandLine)

	code := cli.Run(cmd)
	os.Exit(code)

}
