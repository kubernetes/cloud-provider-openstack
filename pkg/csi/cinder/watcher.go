/*
Copyright 2023 The Kubernetes Authors.

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

package cinder

import (
	"fmt"

	snap "github.com/kubernetes-csi/external-snapshotter/client/v6/clientset/versioned"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	annStorageProvisioner = "volume.kubernetes.io/storage-provisioner"
	snapshotKind          = "VolumeSnapshot"
)

type controllerWatcher struct {
	snapClient snap.Interface
	watcher    watch.Interface
}

func (cw *controllerWatcher) run() {
	klog.V(2).Info("Controller watcher started")
	for event := range cw.watcher.ResultChan() {
		obj := event.Object.DeepCopyObject()
		pvc, ok := obj.(*corev1.PersistentVolumeClaim)
		if !ok {
			continue
		}
		if pvc.ObjectMeta.Annotations[annStorageProvisioner] != driverName {
			continue
		}
		if pvc.Spec.DataSource == nil || pvc.Spec.DataSource.Kind != snapshotKind {
			continue
		}
		snapshotObj, err := cw.snapClient.SnapshotV1().VolumeSnapshots(pvc.Namespace).Get(context.Background(), pvc.Spec.DataSource.Name, metav1.GetOptions{})
		if err != nil {
			klog.ErrorS(err, "Error get VolumeSnapshot", "namespace", pvc.Namespace, "name", pvc.Spec.DataSource.Name)
			continue
		}
		update := false
		finalizer := fmt.Sprintf("%s/pvc-%s", driverName, pvc.UID)
		// TODO(JeffYang): Asynchronously add finalizer seems like unsafety, might have some potential issues.
		// Move it into controllerServer CreateVolume after the PR https://github.com/kubernetes-csi/external-provisioner/pull/1070 merged and released.
		// We can synchronously add finalizer there
		if event.Type == watch.Added {
			klog.V(5).InfoS("Receive ADDED event try to add finalizer to VolumeSnapshot", "PersistentVolumeClaim", pvc.Name, "VolumeSnapshot", snapshotObj.Name)
			update = controllerutil.AddFinalizer(snapshotObj, finalizer)
		}
		if event.Type == watch.Deleted {
			klog.V(5).InfoS("Receive DELETED event try to remove finalizer from VolumeSnapshot", "PersistentVolumeClaim", pvc.Name, "VolumeSnapshot", snapshotObj.Name)
			update = controllerutil.RemoveFinalizer(snapshotObj, finalizer)
		}
		if update {
			_, err := cw.snapClient.SnapshotV1().VolumeSnapshots(pvc.Namespace).Update(context.Background(), snapshotObj, metav1.UpdateOptions{})
			if err != nil {
				klog.ErrorS(err, "Error update VolumeSnapshot", "name", snapshotObj.Name, "finalizer", finalizer)
			}
		}
	}
}

func StartContollerWatcher(restConfig *rest.Config) error {
	snapClient, err := snap.NewForConfig(restConfig)
	if err != nil {
		klog.ErrorS(err, "Error building snapshot clientset")
		return err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.ErrorS(err, "Error building kubernetes clientset")
		return err
	}
	pvcClient := clientset.CoreV1().PersistentVolumeClaims(corev1.NamespaceAll)
	watchInterface, err := pvcClient.Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.ErrorS(err, "Error starting watcher")
		return err
	}

	cw := controllerWatcher{snapClient: snapClient, watcher: watchInterface}
	go cw.run()
	return nil
}
