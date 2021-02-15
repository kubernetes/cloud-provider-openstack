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
	"math/rand"
	"net/http"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/healthz"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-openstack/pkg/openstack"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/cloud-provider/app"
	"k8s.io/cloud-provider/options"
	"k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	_ "k8s.io/component-base/metrics/prometheus/restclient" // for client metric registration
	_ "k8s.io/component-base/metrics/prometheus/version"    // for version metric registration
	"k8s.io/klog/v2"
	_ "k8s.io/kubernetes/pkg/features" // add the kubernetes feature gates

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() {
	mux := http.NewServeMux()
	healthz.InstallHandler(mux)
}

var (
	versionFlag bool
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	goflag.CommandLine.Parse([]string{})
	controllerList := []string{"cloud-node", "cloud-node-lifecycle", "service", "route"}

	s, err := options.NewCloudControllerManagerOptions()
	if err != nil {
		klog.Fatalf("unable to initialize command options: %v", err)
	}
	s.KubeCloudShared.CloudProvider.Name = openstack.ProviderName

	command := &cobra.Command{
		Use: "openstack-cloud-controller-manager",
		Long: `The Cloud controller manager is a daemon that embeds
the cloud specific control loops shipped with Kubernetes.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Glog requires this otherwise it complains.
			goflag.CommandLine.Parse(nil)

			// This is a temporary hack to enable proper logging until upstream dependencies
			// are migrated to fully utilize klog instead of glog.
			klogFlags := goflag.NewFlagSet("klog", goflag.ExitOnError)
			klog.InitFlags(klogFlags)

			// Sync the glog and klog flags.
			cmd.Flags().VisitAll(func(f1 *pflag.Flag) {
				f2 := klogFlags.Lookup(f1.Name)
				if f2 != nil {
					value := f1.Value.String()
					f2.Value.Set(value)
				}
			})
		},
		Run: func(cmd *cobra.Command, args []string) {
			if versionFlag {
				version.PrintVersionAndExit()
			}

			flag.PrintFlags(cmd.Flags())

			c, err := s.Config(controllerList, app.ControllersDisabledByDefault.List())
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}

			cloudconfig := c.Complete().ComponentConfig.KubeCloudShared.CloudProvider
			cloud, err := cloudprovider.InitCloudProvider(cloudconfig.Name, cloudconfig.CloudConfigFile)
			if err != nil {
				klog.Fatalf("Cloud provider could not be initialized: %v", err)
			}
			if cloud == nil {
				klog.Fatalf("cloud provider is nil")
			}

			if err := app.Run(c.Complete(), app.DefaultControllerInitializers(c.Complete(), cloud), wait.NeverStop); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		},
	}

	fs := command.Flags()
	namedFlagSets := s.Flags(controllerList, app.ControllersDisabledByDefault.List())
	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	fs.BoolVar(&versionFlag, "version", false, "Print version and exit")

	openstack.AddExtraFlags(pflag.CommandLine)

	// TODO: once we switch everything over to Cobra commands, we can go back to calling
	// utilflag.InitFlags() (by removing its pflag.Parse() call). For now, we have to set the
	// normalize func and add the go flag set by hand.
	pflag.CommandLine.SetNormalizeFunc(flag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	// utilflag.InitFlags()
	logs.InitLogs()
	defer logs.FlushLogs()

	klog.V(1).Infof("openstack-cloud-controller-manager version: %s", version.Version)

	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
