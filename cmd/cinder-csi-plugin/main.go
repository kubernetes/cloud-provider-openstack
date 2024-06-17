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
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/cloud-provider-openstack/pkg/util/mount"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
)

var (
	endpoint                 string
	nodeID                   string
	cloudConfig              []string
	cloudNames               []string
	additionalTopologies     map[string]string
	cluster                  string
	httpEndpoint             string
	provideControllerService bool
	provideNodeService       bool
)

func main() {
	cmd := &cobra.Command{
		Use:   "cinder-csi-plugin",
		Short: "CSI based Cinder driver",
		Run: func(cmd *cobra.Command, args []string) {
			handle()
		},
		Version: version.Version,
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

	cmd.PersistentFlags().StringSliceVar(&cloudNames, "cloud-name", []string{""}, "Cloud name to instruct CSI driver to read additional OpenStack cloud credentials from the configuration subsections. This option can be specified multiple times to manage multiple OpenStack clouds.")
	cmd.PersistentFlags().StringToStringVar(&additionalTopologies, "additional-topology", map[string]string{}, "Additional CSI driver topology keys, for example topology.kubernetes.io/region=REGION1. This option can be specified multiple times to add multiple additional topology keys.")

	cmd.PersistentFlags().StringVar(&cluster, "cluster", "", "The identifier of the cluster that the plugin is running in.")
	cmd.PersistentFlags().StringVar(&httpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for providing metrics for diagnostics, will listen (example: `:8080`). The default is empty string, which means the server is disabled.")

	cmd.PersistentFlags().BoolVar(&provideControllerService, "provide-controller-service", true, "If set to true then the CSI driver does provide the controller service (default: true)")
	cmd.PersistentFlags().BoolVar(&provideNodeService, "provide-node-service", true, "If set to true then the CSI driver does provide the node service (default: true)")

	openstack.AddExtraFlags(pflag.CommandLine)

	code := cli.Run(cmd)
	os.Exit(code)
}

func handle() {
	// Initialize cloud
	d := cinder.NewDriver(&cinder.DriverOpts{Endpoint: endpoint, ClusterID: cluster})

	openstack.InitOpenStackProvider(cloudConfig, httpEndpoint)
	var err error
	clouds := make(map[string]openstack.IOpenStack)
	for _, cloudName := range cloudNames {
		clouds[cloudName], err = openstack.GetOpenStackProvider(cloudName)
		if err != nil {
			klog.Warningf("Failed to GetOpenStackProvider %s: %v", cloudName, err)
			return
		}
	}

	if provideControllerService {
		d.SetupControllerService(clouds)
	}

	if provideNodeService {
		//Initialize mount
		mount := mount.GetMountProvider()

		//Initialize Metadata
		metadata := metadata.GetMetadataProvider(clouds[cloudNames[0]].GetMetadataOpts().SearchOrder)

		d.SetupNodeService(clouds[cloudNames[0]], mount, metadata, additionalTopologies)
	}

	d.Run()
}
