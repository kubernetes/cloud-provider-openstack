/*
Copyright 2024 The Kubernetes Authors.

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

package csi

import (
	"context"
	"math/rand"
	"os"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const (
	// https://github.com/kubernetes-csi/external-snapshotter/pull/375
	VolSnapshotNameKey        = "csi.storage.k8s.io/volumesnapshot/name"
	VolSnapshotNamespaceKey   = "csi.storage.k8s.io/volumesnapshot/namespace"
	VolSnapshotContentNameKey = "csi.storage.k8s.io/volumesnapshotcontent/name"
	// https://github.com/kubernetes-csi/external-provisioner/pull/399
	PvcNameKey      = "csi.storage.k8s.io/pvc/name"
	PvcNamespaceKey = "csi.storage.k8s.io/pvc/namespace"
	PvNameKey       = "csi.storage.k8s.io/pv/name"
	// https://github.com/kubernetes/kubernetes/pull/79983
	VolEphemeralKey = "csi.storage.k8s.io/ephemeral"
)

var (
	// Recognized volume parameters passed by Kubernetes csi-snapshotter sidecar
	// when run with --extra-create-metadata flag. These are added to metadata
	// of newly created snapshots if present.
	RecognizedCSISnapshotterParams = []string{
		VolSnapshotNameKey,
		VolSnapshotNamespaceKey,
		VolSnapshotContentNameKey,
	}
	// Recognized volume parameters passed by Kubernetes csi-provisioner sidecar
	// when run with --extra-create-metadata flag. These are added to metadata
	// of newly created shares if present.
	RecognizedCSIProvisionerParams = []string{
		PvcNameKey,
		PvcNamespaceKey,
		PvNameKey,
	}
)

var (
	// CSI controller options
	pvcAnnotations bool
	// k8s client options
	master          string
	kubeconfig      string
	kubeAPIQPS      float32
	kubeAPIBurst    int
	minResyncPeriod time.Duration
)

func AddPVCFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&master, "master", "", "Master URL to build a client config from. Either this or kubeconfig needs to be set if the provisioner is being run out of cluster.")
	cmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Absolute path to the kubeconfig file. Either this or master needs to be set if the provisioner is being run out of cluster.")
	cmd.PersistentFlags().Float32Var(&kubeAPIQPS, "kube-api-qps", 5, "QPS to use while communicating with the kubernetes apiserver.")
	cmd.PersistentFlags().IntVar(&kubeAPIBurst, "kube-api-burst", 10, "Burst to use while communicating with the kubernetes apiserver.")
	cmd.PersistentFlags().DurationVar(&minResyncPeriod, "min-resync-period", 12*time.Hour, "The resync period in reflectors will be random between MinResyncPeriod and 2*MinResyncPeriod.")

	cmd.PersistentFlags().BoolVar(&pvcAnnotations, "pvc-annotations", false, "Enable support for PVC annotations in the controller's CreateVolume CSI method (enabling this flag requires enabling the --extra-create-metadata flag in csi-provisioner)")
}

func GetAZFromTopology(topologyKey string, requirement *csi.TopologyRequirement) string {
	var zone string
	var exists bool

	defer func() { klog.V(1).Infof("detected AZ from the topology: %s", zone) }()
	klog.V(4).Infof("preferred topology requirement: %+v", requirement.GetPreferred())
	klog.V(4).Infof("requisite topology requirement: %+v", requirement.GetRequisite())

	for _, topology := range requirement.GetPreferred() {
		zone, exists = topology.GetSegments()[topologyKey]
		if exists {
			return zone
		}
	}

	for _, topology := range requirement.GetRequisite() {
		zone, exists = topology.GetSegments()[topologyKey]
		if exists {
			return zone
		}
	}

	return zone
}

func GetPVCLister() v1.PersistentVolumeClaimLister {
	if !pvcAnnotations {
		return nil
	}

	// get the KUBECONFIG from env if specified (useful for local/debug cluster)
	kubeconfigEnv := os.Getenv("KUBECONFIG")

	if kubeconfigEnv != "" {
		klog.Infof("Found KUBECONFIG environment variable set, using that..")
		kubeconfig = kubeconfigEnv
	}

	config, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		klog.Fatalf("Failed to create config: %v", err)
	}

	config.QPS = kubeAPIQPS
	config.Burst = kubeAPIBurst

	config.ContentType = runtime.ContentTypeProtobuf
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create client: %v", err)
	}

	factory := informers.NewSharedInformerFactory(clientset, resyncPeriod(minResyncPeriod))
	ctx := context.TODO()
	pvcInformer := factory.Core().V1().PersistentVolumeClaims().Informer()
	go pvcInformer.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), pvcInformer.HasSynced) {
		klog.Fatal("Error syncing PVC informer cache")
	}

	klog.Info("Successully created PVC Annotations Lister")

	return factory.Core().V1().PersistentVolumeClaims().Lister()
}

// GetPVCAnnotations returns PVC annotations for the given PVC name and
// namespace stored in the params map.
func GetPVCAnnotations(pvcLister v1.PersistentVolumeClaimLister, params map[string]string) map[string]string {
	if pvcLister == nil {
		return nil
	}

	namespace := params[PvcNamespaceKey]
	pvcName := params[PvcNameKey]
	if namespace == "" || pvcName == "" {
		klog.Errorf("Invalid namespace or PVC name (%s/%s), check whether the --extra-create-metadata flag is set in csi-provisioner", namespace, pvcName)
		return nil
	}

	pvc, err := pvcLister.PersistentVolumeClaims(namespace).Get(pvcName)
	if err != nil {
		klog.Errorf("Failed to get PVC %s/%s: %v", namespace, pvcName, err)
		return nil
	}

	return pvc.Annotations
}

// resyncPeriod generates a random duration so that multiple controllers don't
// get into lock-step and all hammer the apiserver with list requests
// simultaneously. Copied from the
// k8s.io/cloud-provider/app/controllermanager.go
func resyncPeriod(s time.Duration) time.Duration {
	factor := rand.Float64() + 1
	return time.Duration(float64(s.Nanoseconds()) * factor)
}
