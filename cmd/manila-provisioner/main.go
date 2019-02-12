/*
Copyright 2018 The Kubernetes Authors.

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

	"github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/controller"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/cloud-provider-openstack/pkg/share/manila"
	"k8s.io/klog"
)

var (
	kubeconfig      = flag.String("kubeconfig", "", "Path to a kube config. Only required if out-of-cluster.")
	provisionerName = flag.String("provisioner", "externalstorage.k8s.io/manila", "Name of the provisioner. The provisioner will only provision volumes for claims that request a StorageClass with a provisioner field set equal to this name.")
)

func main() {
	flag.Set("logtostderr", "true")

	// Glog requires this otherwise it complains.
	flag.Parse()
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

	// Create an InClusterConfig and use it to create a client for the controller
	// to use to communicate with Kubernetes
	config, err := buildConfig(*kubeconfig)
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

	// Start the provision controller which will dynamically provision Manila PVs
	provisioner := controller.NewProvisionController(
		clientset,
		*provisionerName,
		manila.NewProvisioner(clientset),
		serverVersion.GitVersion,
	)

	provisioner.Run(wait.NeverStop)
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
