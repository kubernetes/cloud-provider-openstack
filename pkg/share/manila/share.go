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

package manila

import (
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/controller"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/sharebackends"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions"
	"k8s.io/kubernetes/pkg/controller/volume/persistentvolume"
)

const (
	shareAvailabilityTimeout = 120 /* secs */

	manilaAnnotationPrefix               = "manila.cloud-provider-openstack.kubernetes.io/"
	manilaAnnotationID                   = manilaAnnotationPrefix + "ID"
	manilaAnnotationProvisionType        = manilaAnnotationPrefix + "ProvisionType"
	manilaAnnotationOSSecretName         = manilaAnnotationPrefix + "OSSecretName"
	manilaAnnotationOSSecretNamespace    = manilaAnnotationPrefix + "OSSecretNamespace"
	manilaAnnotationShareSecretName      = manilaAnnotationPrefix + "ShareSecretName"
	manilaAnnotationShareSecretNamespace = manilaAnnotationPrefix + "ShareSecretNamespace"

	manilaProvisionTypeDynamic = "dynamic"
	manilaProvisionTypeStatic  = "static"
)

func createShare(
	volumeHandle string,
	volOptions *controller.VolumeOptions,
	shareOptions *shareoptions.ShareOptions,
	client *gophercloud.ServiceClient,
) (*shares.Share, error) {
	req, err := buildCreateRequest(volOptions, shareOptions, volumeHandle)
	if err != nil {
		return nil, err
	}

	return shares.Create(client, *req).Extract()
}

func deleteShare(shareID, provisionType string, shareSecretRef *v1.SecretReference, client *gophercloud.ServiceClient, c clientset.Interface) error {
	if backendName, err := getBackendNameForShare(shareID); err == nil {
		shareBackend, err := getShareBackend(backendName)
		if err != nil {
			return err
		}

		err = shareBackend.RevokeAccess(&sharebackends.RevokeAccessArgs{
			ShareID:        shareID,
			ShareSecretRef: shareSecretRef,
			Clientset:      c,
			Client:         client,
		})

		if err != nil {
			return err
		}
	}

	if provisionType == manilaProvisionTypeDynamic {
		// manila-provisioner is allowed to delete only those shares which it created
		r := shares.Delete(client, shareID)
		if r.Err != nil {
			return r.Err
		}
	}

	return nil
}

func getShare(shareOptions *shareoptions.ShareOptions, client *gophercloud.ServiceClient) (*shares.Share, error) {
	if shareOptions.OSShareID != "" {
		return shares.Get(client, shareOptions.OSShareID).Extract()
	} else if shareOptions.OSShareName != "" {
		shareID, err := shares.IDFromName(client, shareOptions.OSShareName)
		if err != nil {
			return nil, fmt.Errorf("failed to get share ID from name: %v", err)
		}

		return shares.Get(client, shareID).Extract()
	}

	return nil, fmt.Errorf("both OSShareName and OSShareID are empty")
}

func buildCreateRequest(
	volOptions *controller.VolumeOptions,
	shareOptions *shareoptions.ShareOptions,
	volumeHandle string,
) (*shares.CreateOpts, error) {
	storageSize, err := getStorageSizeInGiga(volOptions.PVC)
	if err != nil {
		return nil, fmt.Errorf("couldn't retrieve PVC storage size: %v", err)
	}

	shareName := "pvc-" + string(volOptions.PVC.GetUID())

	return &shares.CreateOpts{
		ShareProto:     shareOptions.Protocol,
		ShareNetworkID: shareOptions.OSShareNetworkID,
		Size:           storageSize,
		Name:           shareName,
		ShareType:      shareOptions.Type,
		Metadata: map[string]string{
			persistentvolume.CloudVolumeCreatedForClaimNamespaceTag: volOptions.PVC.Namespace,
			persistentvolume.CloudVolumeCreatedForClaimNameTag:      volOptions.PVC.Name,
			persistentvolume.CloudVolumeCreatedForVolumeNameTag:     shareName,
		},
	}, nil
}

func buildPersistentVolume(
	share *shares.Share,
	accessRight *shares.AccessRight,
	volSource *v1.PersistentVolumeSource,
	volOptions *controller.VolumeOptions,
	shareSecretRef *v1.SecretReference,
	shareOptions *shareoptions.ShareOptions,
) *v1.PersistentVolume {
	provisionType := manilaProvisionTypeDynamic
	if shareOptions.OSShareAccessID != "" {
		provisionType = manilaProvisionTypeStatic
	}

	return &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: volOptions.PVName,
			Annotations: map[string]string{
				manilaAnnotationID:                   share.ID,
				manilaAnnotationOSSecretName:         shareOptions.OSSecretName,
				manilaAnnotationOSSecretNamespace:    shareOptions.OSSecretNamespace,
				manilaAnnotationShareSecretName:      shareSecretRef.Name,
				manilaAnnotationShareSecretNamespace: shareSecretRef.Namespace,
				manilaAnnotationProvisionType:        provisionType,
			},
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: volOptions.PersistentVolumeReclaimPolicy,
			AccessModes:                   getPVAccessMode(volOptions.PVC.Spec.AccessModes),
			Capacity:                      v1.ResourceList{v1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dG", share.Size))},
			PersistentVolumeSource:        *volSource,
		},
	}
}
