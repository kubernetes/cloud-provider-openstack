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
	"os/signal"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"k8s.io/cloud-provider-openstack/pkg/kms/server"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
)

var (
	socketpath  string
	cloudconfig string
)

func init() {
	flag.Set("logtostderr", "true")
}

func main() {
	// Glog requires this otherwise it complains.
	flag.CommandLine.Parse(nil)
	// This is a temporary hack to enable proper logging until upstream dependencies
	// are migrated to fully utilize klog instead of glog.
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	logs.InitLogs()
	defer logs.FlushLogs()

	cmd := &cobra.Command{
		Use:   "barbican-kms-plugin",
		Short: "Barbican KMS plugin for kubernetes",
		RunE: func(cmd *cobra.Command, args []string) error {
			sigchan := make(chan os.Signal, 1)
			signal.Notify(sigchan, unix.SIGTERM, unix.SIGINT)
			err := server.Run(cloudconfig, socketpath, sigchan)
			return err
		},
	}

	cmd.Flags().AddGoFlagSet(flag.CommandLine)

	cmd.PersistentFlags().StringVar(&socketpath, "socketpath", "", "Barbican KMS Plugin unix socket endpoint")
	cmd.MarkPersistentFlagRequired("socketpath")

	cmd.PersistentFlags().StringVar(&cloudconfig, "cloud-config", "", "Barbican KMS Plugin cloud config")
	cmd.MarkPersistentFlagRequired("cloud-config")

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s", err.Error())
		os.Exit(1)
	}

	os.Exit(0)
}
