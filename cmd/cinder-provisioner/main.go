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
	"github.com/spf13/pflag"
	"k8s.io/klog"

	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/cloud-provider-openstack/pkg/volume/cinder/provisioner"
	"k8s.io/cloud-provider-openstack/pkg/volume/cinder/volumeservice"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/controller"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	kflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
)

var (
	master      string
	kubeconfig  string
	id          string
	cloudconfig string
)

func main() {
	pflag.StringVar(&master, "master", "", "Master URL")
	pflag.StringVar(&kubeconfig, "kubeconfig", "", "Absolute path to the kubeconfig")
	pflag.StringVar(&id, "id", "", "Unique provisioner identity")
	pflag.StringVar(&cloudconfig, "cloud-config", "", "Path to OpenStack config file")

	volumeservice.AddExtraFlags(pflag.CommandLine)

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

	kflag.InitFlags()
	logs.InitLogs()
	defer logs.FlushLogs()

	klog.V(1).Infof("cinder-provisioner version: %s", version.Version)

	var config *rest.Config
	var err error
	if master != "" || kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags(master, kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	prID := provisioner.ProvisionerName
	if id != "" {
		prID = id
	}
	if err != nil {
		klog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		klog.Fatalf("Error getting server version: %v", err)
	}

	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	cinderProvisioner, err := provisioner.NewCinderProvisioner(clientset, prID, cloudconfig)
	if err != nil {
		klog.Fatalf("Error creating Cinder provisioner: %v", err)
	}

	// Start the provision controller which will dynamically provision cinder
	// PVs
	pc := controller.NewProvisionController(
		clientset,
		provisioner.ProvisionerName,
		cinderProvisioner,
		serverVersion.GitVersion,
	)

	pc.Run(wait.NeverStop)
}
