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
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/csiclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/runtimeconfig"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
)

var (
	// Driver configuration
	driverName            string
	withTopology          bool
	protoSelector         string
	fwdEndpoint           string
	compatibilitySettings string

	// Node information
	nodeID    string
	nodeAZ    string
	clusterID string

	// Runtime options
	endpoint          string
	runtimeConfigFile string
	userAgentData     []string
)

func validateShareProtocolSelector(v string) error {
	supportedShareProtocols := []string{"NFS", "CEPHFS"}

	v = strings.ToUpper(v)
	for _, proto := range supportedShareProtocols {
		if v == proto {
			return nil
		}
	}

	return fmt.Errorf("share protocol %q not supported; supported protocols are %v", v, supportedShareProtocols)
}

func main() {
	if err := flag.CommandLine.Parse([]string{}); err != nil {
		klog.Fatalf("Unable to parse flags: %v", err)
	}

	cmd := &cobra.Command{
		Use:   os.Args[0],
		Short: "CSI Manila driver",
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
			if err := validateShareProtocolSelector(protoSelector); err != nil {
				klog.Fatalf(err.Error())
			}

			manilaClientBuilder := &manilaclient.ClientBuilder{UserAgent: "manila-csi-plugin", ExtraUserAgentData: userAgentData}
			csiClientBuilder := &csiclient.ClientBuilder{}

			d, err := manila.NewDriver(
				&manila.DriverOpts{
					DriverName:          driverName,
					NodeID:              nodeID,
					NodeAZ:              nodeAZ,
					WithTopology:        withTopology,
					ShareProto:          protoSelector,
					ServerCSIEndpoint:   endpoint,
					FwdCSIEndpoint:      fwdEndpoint,
					ManilaClientBuilder: manilaClientBuilder,
					CSIClientBuilder:    csiClientBuilder,
					ClusterID:           clusterID,
				},
			)

			if err != nil {
				klog.Fatalf("driver initialization failed: %v", err)
			}

			runtimeconfig.RuntimeConfigFilename = runtimeConfigFile

			d.Run()
		},
	}

	cmd.Flags().AddGoFlagSet(flag.CommandLine)

	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "unix://tmp/csi.sock", "CSI endpoint")

	cmd.PersistentFlags().StringVar(&driverName, "drivername", "manila.csi.openstack.org", "name of the driver")

	cmd.PersistentFlags().StringVar(&nodeID, "nodeid", "", "this node's ID")
	if err := cmd.MarkPersistentFlagRequired("nodeid"); err != nil {
		klog.Fatalf("Unable to mark flag nodeid to be required: %v", err)
	}

	cmd.PersistentFlags().StringVar(&nodeAZ, "nodeaz", "", "this node's availability zone")

	cmd.PersistentFlags().StringVar(&runtimeConfigFile, "runtime-config-file", "", "path to the runtime configuration file")

	cmd.PersistentFlags().BoolVar(&withTopology, "with-topology", false, "cluster is topology-aware")

	cmd.PersistentFlags().StringVar(&protoSelector, "share-protocol-selector", "", "specifies which Manila share protocol to use. Valid values are NFS and CEPHFS")
	if err := cmd.MarkPersistentFlagRequired("share-protocol-selector"); err != nil {
		klog.Fatalf("Unable to mark flag share-protocol-selector to be required: %v", err)
	}

	cmd.PersistentFlags().StringVar(&fwdEndpoint, "fwdendpoint", "", "CSI Node Plugin endpoint to which all Node Service RPCs are forwarded. Must be able to handle the file-system specified in share-protocol-selector")
	if err := cmd.MarkPersistentFlagRequired("fwdendpoint"); err != nil {
		klog.Fatalf("Unable to mark flag fwdendpoint to be required: %v", err)
	}

	cmd.PersistentFlags().StringVar(&compatibilitySettings, "compatibility-settings", "", "settings for the compatibility layer")

	cmd.PersistentFlags().StringArrayVar(&userAgentData, "user-agent", nil, "extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")

	cmd.PersistentFlags().StringVar(&clusterID, "cluster-id", "", "The identifier of the cluster that the plugin is running in.")

	code := cli.Run(cmd)
	os.Exit(code)
}
