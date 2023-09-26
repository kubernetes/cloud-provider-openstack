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
	"context"
	"fmt"
	"testing"
	"time"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	snapfake "github.com/kubernetes-csi/external-snapshotter/client/v6/clientset/versioned/fake"
	uuid "github.com/pborman/uuid"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientfake "k8s.io/client-go/kubernetes/fake"
)

func TestControllerWatcherRun(t *testing.T) {
	// Init assert
	assert := assert.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Init fake client and watch interface
	snapClient := snapfake.NewSimpleClientset()
	clientset := clientfake.NewSimpleClientset()
	pvcClient := clientset.CoreV1().PersistentVolumeClaims(corev1.NamespaceDefault)
	watchInterface, err := pvcClient.Watch(ctx, metav1.ListOptions{})
	assert.Nil(err)
	// Start watcher
	ctrlWatcher := controllerWatcher{snapClient: snapClient, watcher: watchInterface}
	go ctrlWatcher.run()
	// Prepare necessary resources
	originalPVCName := "original-pvc"
	testVSName := "test-vs"
	vs := snapapi.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{Name: testVSName},
		Spec: snapapi.VolumeSnapshotSpec{
			Source: snapapi.VolumeSnapshotSource{
				PersistentVolumeClaimName: &originalPVCName,
			},
		},
	}
	_, err = snapClient.SnapshotV1().VolumeSnapshots(corev1.NamespaceDefault).Create(ctx, &vs, metav1.CreateOptions{})
	assert.Nil(err)
	storageAPIGroup := "snapshot.storage.k8s.io"
	// Test PVC creation with VolumeSnapshot
	testPVCUID := uuid.New()
	testPVCName := "test-pvc"
	_, err = pvcClient.Create(context.TODO(), &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: testPVCName,
			UID:  types.UID(testPVCUID),
			Annotations: map[string]string{
				annStorageProvisioner: driverName,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			DataSource: &corev1.TypedLocalObjectReference{
				Name:     testVSName,
				Kind:     "VolumeSnapshot",
				APIGroup: &storageAPIGroup,
			},
		},
	}, metav1.CreateOptions{})
	assert.Nil(err)
	expectedFinalizer := fmt.Sprintf("%s/pvc-%s", driverName, testPVCUID)
	err = wait.PollUntilContextTimeout(ctx, time.Second*1, time.Second*5, true, func(context.Context) (done bool, err error) {
		vs, err := snapClient.SnapshotV1().VolumeSnapshots(corev1.NamespaceDefault).Get(ctx, testVSName, metav1.GetOptions{})
		assert.Nil(err)
		f := vs.GetFinalizers()
		for _, e := range f {
			// If VolumeSnashot contain expectedFinalizer, test successful
			if e == expectedFinalizer {
				return true, nil
			}
		}
		return false, nil
	})
	assert.Nil(err)
	// Test PVC deletion
	err = pvcClient.Delete(context.TODO(), testPVCName, metav1.DeleteOptions{})
	assert.Nil(err)
	err = wait.PollUntilContextTimeout(ctx, time.Second*1, time.Second*5, true, func(context.Context) (done bool, err error) {
		vs, err := snapClient.SnapshotV1().VolumeSnapshots(corev1.NamespaceDefault).Get(ctx, testVSName, metav1.GetOptions{})
		assert.Nil(err)
		f := vs.GetFinalizers()
		for _, e := range f {
			// If VolumeSnashot still contain expectedFinalizer, wait a scond and recheck until timeout
			if e == expectedFinalizer {
				return false, nil
			}
		}
		return true, nil
	})
	assert.Nil(err)
}
