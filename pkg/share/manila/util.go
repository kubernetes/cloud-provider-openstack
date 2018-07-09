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
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func getPVAccessMode(PVCAccessMode []v1.PersistentVolumeAccessMode) []v1.PersistentVolumeAccessMode {
	if len(PVCAccessMode) > 0 {
		return PVCAccessMode
	}

	return []v1.PersistentVolumeAccessMode{
		v1.ReadWriteOnce,
		v1.ReadOnlyMany,
		v1.ReadWriteMany,
	}
}

func getStorageSizeInGiga(pvc *v1.PersistentVolumeClaim) (int, error) {
	errStorageSizeNotConfigured := fmt.Errorf("requested storage capacity must be set")

	if pvc.Spec.Resources.Requests == nil {
		return 0, errStorageSizeNotConfigured
	}

	storageSize, ok := pvc.Spec.Resources.Requests[v1.ResourceStorage]
	if !ok {
		return 0, errStorageSizeNotConfigured
	}

	if storageSize.IsZero() {
		return 0, fmt.Errorf("requested storage size must not have zero value")
	}

	if storageSize.Sign() == -1 {
		return 0, fmt.Errorf("requested storage size must be greater than zero")
	}

	var buf []byte
	canonicalValue, _ := storageSize.AsScale(resource.Giga)
	storageSizeAsByteSlice, _ := canonicalValue.AsCanonicalBytes(buf)

	return strconv.Atoi(string(storageSizeAsByteSlice))
}

// Chooses one ExportLocation according to the below rules:
// 1. Path is not empty
// 2. IsAdminOnly == false
// 3. Preferred == true are preferred over Preferred == false
// 4. Locations with lower slice index are preferred over locations with higher slice index
func chooseExportLocation(locs []shares.ExportLocation) (shares.ExportLocation, error) {
	if len(locs) == 0 {
		return shares.ExportLocation{}, fmt.Errorf("export locations list is empty")
	}

	var (
		foundMatchingNotPreferred = false
		matchingNotPreferred      shares.ExportLocation
	)

	for _, loc := range locs {
		if loc.IsAdminOnly || strings.TrimSpace(loc.Path) == "" {
			continue
		}

		if loc.Preferred {
			return loc, nil
		}

		if !foundMatchingNotPreferred {
			matchingNotPreferred = loc
			foundMatchingNotPreferred = true
		}
	}

	if foundMatchingNotPreferred {
		return matchingNotPreferred, nil
	}

	return shares.ExportLocation{}, fmt.Errorf("cannot find any non-admin export location")
}

func getAnnotationFromPV(volume *v1.PersistentVolume, annotationKey string) (string, error) {
	if provisionType, ok := volume.ObjectMeta.Annotations[annotationKey]; ok {
		return provisionType, nil
	}

	return "", fmt.Errorf("PV object for volume %s is missing key %s in its annotations", volume.GetName(), annotationKey)
}

func getProvisionTypeFromPV(volume *v1.PersistentVolume) (string, error) {
	return getAnnotationFromPV(volume, manilaAnnotationProvisionType)
}

func getShareIDfromPV(volume *v1.PersistentVolume) (string, error) {
	return getAnnotationFromPV(volume, manilaAnnotationID)
}

func getSecretRefFromPV(nameKey, namespaceKey string, pv *v1.PersistentVolume) (*v1.SecretReference, error) {
	var (
		ref = &v1.SecretReference{}
		err error
	)

	ref.Name, err = getAnnotationFromPV(pv, nameKey)
	if err != nil {
		return nil, err
	}

	ref.Namespace, err = getAnnotationFromPV(pv, namespaceKey)
	if err != nil {
		return nil, err
	}

	return ref, nil
}

func getOSSecretRefFromPV(pv *v1.PersistentVolume) (*v1.SecretReference, error) {
	return getSecretRefFromPV(manilaAnnotationOSSecretName, manilaAnnotationOSSecretNamespace, pv)
}

func getShareSecretRefFromPV(pv *v1.PersistentVolume) (*v1.SecretReference, error) {
	return getSecretRefFromPV(manilaAnnotationShareSecretName, manilaAnnotationShareSecretNamespace, pv)
}

func waitForShareStatus(shareID string, client *gophercloud.ServiceClient, desiredStatus string) error {
	return gophercloud.WaitFor(shareAvailabilityTimeout, func() (bool, error) {
		share, err := shares.Get(client, shareID).Extract()
		if err != nil {
			return false, err
		}

		return share.Status == desiredStatus, nil
	})
}
