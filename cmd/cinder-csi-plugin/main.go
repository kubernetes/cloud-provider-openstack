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
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cloud-provider-openstack/pkg/csi"
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
	noClient                 bool
	withTopology             bool
)

func main() {
	cmd := &cobra.Command{
		Use:   "cinder-csi-plugin",
		Short: "CSI based Cinder driver",
		Run: func(cmd *cobra.Command, args []string) {
			handle()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f := cmd.Flags()

			if !provideControllerService {
				return nil
			}

			configs, err := f.GetStringSlice("cloud-config")
			if err != nil {
				return err
			}

			if len(configs) == 0 {
				return fmt.Errorf("unable to mark flag cloud-config to be required")
			}

			return nil
		},
		Version: version.Version,
	}

	csi.AddPVCFlags(cmd)

	cmd.PersistentFlags().StringVar(&nodeID, "nodeid", "", "node id")
	if err := cmd.PersistentFlags().MarkDeprecated("nodeid", "This option is now ignored by the driver. It will be removed in a future release."); err != nil {
		klog.Fatalf("Unable to mark flag nodeid to be deprecated: %v", err)
	}

	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "CSI endpoint")
	if err := cmd.MarkPersistentFlagRequired("endpoint"); err != nil {
		klog.Fatalf("Unable to mark flag endpoint to be required: %v", err)
	}

	cmd.Flags().StringSliceVar(&cloudConfig, "cloud-config", nil, "CSI driver cloud config. This option can be given multiple times")

	cmd.PersistentFlags().BoolVar(&withTopology, "with-topology", true, "cluster is topology-aware")

	cmd.PersistentFlags().StringSliceVar(&cloudNames, "cloud-name", []string{""}, "Cloud name to instruct CSI driver to read additional OpenStack cloud credentials from the configuration subsections. This option can be specified multiple times to manage multiple OpenStack clouds.")
	cmd.PersistentFlags().StringToStringVar(&additionalTopologies, "additional-topology", map[string]string{}, "Additional CSI driver topology keys, for example topology.kubernetes.io/region=REGION1. This option can be specified multiple times to add multiple additional topology keys.")

	cmd.PersistentFlags().StringVar(&cluster, "cluster", "", "The identifier of the cluster that the plugin is running in.")
	cmd.PersistentFlags().StringVar(&httpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for providing metrics for diagnostics, will listen (example: `:8080`). The default is empty string, which means the server is disabled.")

	cmd.PersistentFlags().BoolVar(&provideControllerService, "provide-controller-service", true, "If set to true then the CSI driver does provide the controller service (default: true)")
	cmd.PersistentFlags().BoolVar(&provideNodeService, "provide-node-service", true, "If set to true then the CSI driver does provide the node service (default: true)")
	cmd.PersistentFlags().BoolVar(&noClient, "node-service-no-os-client", false, "If set to true then the CSI driver node service will not use the OpenStack client (default: false)")
	cmd.PersistentFlags().MarkDeprecated("node-service-no-os-client", "This flag is deprecated and will be removed in the future. Node service do not use OpenStack credentials anymore.") //nolint:errcheck

	openstack.AddExtraFlags(pflag.CommandLine)

	code := cli.Run(cmd)
	os.Exit(code)
}

func handle() {
	// Initialize cloud
	d := cinder.NewDriver(&cinder.DriverOpts{
		Endpoint:     endpoint,
		ClusterID:    cluster,
		PVCLister:    csi.GetPVCLister(),
		WithTopology: withTopology,
	})

	openstack.InitOpenStackProvider(cloudConfig, httpEndpoint)

	if provideControllerService {
		var err error
		clouds := make(map[string]openstack.IOpenStack)
		for _, cloudName := range cloudNames {
			clouds[cloudName], err = openstack.GetOpenStackProvider(cloudName)
			if err != nil {
				klog.Warningf("Failed to GetOpenStackProvider %s: %v", cloudName, err)
				return
			}
		}

		d.SetupControllerService(clouds)
	}

	if provideNodeService {
		// Initialize mount
		mount := mount.GetMountProvider()

		cfg, err := openstack.GetConfigFromFiles(cloudConfig)
		if err != nil && !os.IsNotExist(err) {
			klog.Warningf("Failed to GetConfigFromFiles: %v", err)
			return
		}

		// Initialize Metadata
		metadata := metadata.GetMetadataProvider(cfg.Metadata.SearchOrder)

		d.SetupNodeService(mount, metadata, cfg.BlockStorage, additionalTopologies)
	}

	d.Run()
}
