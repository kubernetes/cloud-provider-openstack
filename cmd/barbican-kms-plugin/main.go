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
	"os"
	"os/signal"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"k8s.io/cloud-provider-openstack/pkg/kms/server"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
)

var (
	socketPath  string
	cloudConfig string
)

func main() {
	flag.Parse()

	// This is a temporary hack to enable proper logging until upstream dependencies
	// are migrated to fully utilize klog instead of glog.
	klog.InitFlags(nil)

	cmd := &cobra.Command{
		Use:   "barbican-kms-plugin",
		Short: "Barbican KMS plugin for Kubernetes",
		RunE: func(cmd *cobra.Command, args []string) error {
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, unix.SIGTERM, unix.SIGINT)
			err := server.Run(cloudConfig, socketPath, sigChan)
			return err
		},
	}

	cmd.PersistentFlags().StringVar(&socketPath, "socketpath", "", "Barbican KMS Plugin unix socket endpoint")
	if err := cmd.MarkPersistentFlagRequired("socketpath"); err != nil {
		klog.Fatalf("Unable to mark flag socketpath as required: %v", err)
	}

	cmd.PersistentFlags().StringVar(&cloudConfig, "cloud-config", "", "Barbican KMS Plugin cloud config")
	if err := cmd.MarkPersistentFlagRequired("cloud-config"); err != nil {
		klog.Fatalf("Unable to mark flag cloud-config as required: %v", err)
	}

	code := cli.Run(cmd)
	os.Exit(code)
}
