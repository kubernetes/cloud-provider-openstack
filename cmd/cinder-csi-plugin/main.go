/*
Copyright 2017 The Kubernetes Authors.

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
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/cloud-provider-openstack/pkg/util/mount"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
)

var (
	endpoint     string
	nodeID       string
	cloudConfig  []string
	cluster      string
	httpEndpoint string
)

func main() {
	if err := flag.CommandLine.Parse([]string{}); err != nil {
		klog.Fatalf("Unable to parse flags: %v", err)
	}

	cmd := &cobra.Command{
		Use:   "Cinder",
		Short: "CSI based Cinder driver",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Glog requires this otherwise it complains.
			if err := flag.CommandLine.Parse(nil); err != nil {
				return fmt.Errorf("unable to parse flags: %w", err)
			}

			// This is a temporary hack to enable proper logging until upstream dependencies
			// are migrated to fully utilize klog instead of glog.
			klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
			klog.InitFlags(klogFlags)

			// Sync the glog and klog flags.
			cmd.Flags().VisitAll(func(f1 *pflag.Flag) {
				f2 := klogFlags.Lookup(f1.Name)
				if f2 != nil {
					value := f1.Value.String()
					_ = f2.Value.Set(value)
				}
			})
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			handle()
		},
	}

	cmd.PersistentFlags().StringVar(&nodeID, "nodeid", "", "node id")
	if err := cmd.PersistentFlags().MarkDeprecated("nodeid", "This flag would be removed in future. Currently, the value is ignored by the driver"); err != nil {
		klog.Fatalf("Unable to mark flag nodeid to be deprecated: %v", err)
	}

	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "CSI endpoint")
	if err := cmd.MarkPersistentFlagRequired("endpoint"); err != nil {
		klog.Fatalf("Unable to mark flag endpoint to be required: %v", err)
	}

	cmd.PersistentFlags().StringSliceVar(&cloudConfig, "cloud-config", nil, "CSI driver cloud config. This option can be given multiple times")
	if err := cmd.MarkPersistentFlagRequired("cloud-config"); err != nil {
		klog.Fatalf("Unable to mark flag cloud-config to be required: %v", err)
	}

	cmd.PersistentFlags().StringVar(&cluster, "cluster", "", "The identifier of the cluster that the plugin is running in.")
	cmd.PersistentFlags().StringVar(&httpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for diagnostics, including metrics and leader election health check, will listen (example: `:8080`). The default is empty string, which means the server is disabled.")
	openstack.AddExtraFlags(pflag.CommandLine)

	code := cli.Run(cmd)
	os.Exit(code)
}

func handle() {

	// Initialize cloud
	d := cinder.NewDriver(endpoint, cluster)
	openstack.InitOpenStackProvider(cloudConfig, httpEndpoint)
	cloud, err := openstack.GetOpenStackProvider()
	if err != nil {
		klog.Warningf("Failed to GetOpenStackProvider: %v", err)
		return
	}
	//Initialize mount
	mount := mount.GetMountProvider()

	//Initialize Metadata
	metadata := metadata.GetMetadataProvider(cloud.GetMetadataOpts().SearchOrder)

	d.SetupDriver(cloud, mount, metadata)
	d.Run()
}
