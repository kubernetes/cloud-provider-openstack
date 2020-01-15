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

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"github.com/pborman/uuid"
	v1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	manilautil "k8s.io/cloud-provider-openstack/pkg/csi/manila/util"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/sharebackends"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions"
	"k8s.io/klog"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/controller"
)

// Provisioner struct, implements controller.Provisioner interface
type Provisioner struct {
	clientset          clientset.Interface
	userAgent          string
	extraUserAgentData []string
}

// NewProvisioner creates a new instance of Manila provisioner
func NewProvisioner(c clientset.Interface, userAgent string, extraUserAgentData []string) *Provisioner {
	return &Provisioner{
		clientset:          c,
		userAgent:          userAgent,
		extraUserAgentData: extraUserAgentData,
	}
}

// Provision a share in Manila service
func (p *Provisioner) Provision(volOptions controller.ProvisionOptions) (*v1.PersistentVolume, error) {
	if volOptions.PVC.Spec.Selector != nil {
		return nil, fmt.Errorf("claim Selector is not supported")
	}

	// Initialization

	shareOptions, err := shareoptions.NewShareOptions(&volOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create share options: %v", err)
	}

	volumeHandle := "pvc-" + string(volOptions.PVC.GetUID())
	osSecretRef := v1.SecretReference{Name: shareOptions.OSSecretName, Namespace: shareOptions.OSSecretNamespace}
	shareSecretRef := v1.SecretReference{Name: "manila-" + uuid.NewUUID().String(), Namespace: shareOptions.ShareSecretNamespace}

	osOptions, err := shareoptions.NewOpenStackOptionsFromSecret(p.clientset, &osSecretRef)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve secrets %s:%s: %v",
			shareOptions.OSSecretNamespace, shareOptions.OSSecretName, err)
	}

	shareBackend, err := getShareBackend(shareOptions.Backend)
	if err != nil {
		return nil, fmt.Errorf("failed to get share backend: %v", err)
	}

	client, err := manilaclient.New(osOptions, p.userAgent, p.extraUserAgentData)
	if err != nil {
		return nil, fmt.Errorf("failed to create Manila v2 client: %v", err)
	}

	// Share provision

	var share *shares.Share

	if shareOptions.OSShareAccessID == "" {
		// Dynamic provision - we're creating a new share

		share, err = createShare(volumeHandle, &volOptions, shareOptions, client)
		if err != nil {
			return nil, fmt.Errorf("failed to create a share: %v", err)
		}

		defer func() {
			// Delete the share if any of its setup operations fail
			if err != nil {
				if delErr := deleteShare(share.ID, manilaProvisionTypeDynamic, &osSecretRef, client, p.clientset); delErr != nil {
					klog.Errorf("failed to delete share %s in a rollback procedure: %v", share.ID, delErr)
				}
			}
		}()

		if err = waitForShareStatus(share.ID, client, "available"); err != nil {
			return nil, fmt.Errorf("waiting for share %s to become created failed: %v", share.ID, err)
		}
	} else {
		// Static provision - we're using an existing share

		share, err = getShare(shareOptions, client)
		if err != nil {
			return nil, fmt.Errorf("failed to get share %s: %v", shareOptions.OSShareID, err)
		}
	}

	// Get the export location

	chosenExportLocation, err := manilautil.GetChosenExportLocation(share.ID, client)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve export location for volume %s: %v", share.ID, err)
	}

	accessRight, err := shareBackend.GrantAccess(&sharebackends.GrantAccessArgs{
		Share:          share,
		Options:        shareOptions,
		ShareSecretRef: &shareSecretRef,
		Clientset:      p.clientset,
		Client:         client,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to grant access for share %s: %v", share.ID, err)
	}

	// For deleteShare()
	registerBackendForShare(shareOptions.Backend, share.ID)

	volSource, err := shareBackend.BuildSource(&sharebackends.BuildSourceArgs{
		VolumeHandle:   volumeHandle,
		Share:          share,
		Options:        shareOptions,
		ShareSecretRef: &shareSecretRef,
		Location:       chosenExportLocation,
		Clientset:      p.clientset,
		AccessRight:    accessRight,
	})
	if err != nil {
		return nil, fmt.Errorf("backend %s failed to create volume source for share %s: %v", shareBackend.Name(), share.ID, err)
	}

	klog.Infof("successfully provisioned share %s (%s/%s)", share.ID, shareOptions.Protocol, shareOptions.Backend)

	return buildPersistentVolume(share, accessRight, volSource, &volOptions, &shareSecretRef, shareOptions), nil
}

// Delete a share from Manila service
func (p *Provisioner) Delete(pv *v1.PersistentVolume) error {
	// Initialization

	shareID, err := getShareIDfromPV(pv)
	if err != nil {
		return fmt.Errorf("failed to get share ID for volume %s: %v", pv.GetName(), err)
	}

	osSecretRef, err := getOSSecretRefFromPV(pv)
	if err != nil {
		return fmt.Errorf("failed to get OpenStack secret reference from PV for share %s: %v", shareID, err)
	}

	shareSecretRef, err := getShareSecretRefFromPV(pv)
	if err != nil {
		return fmt.Errorf("failed to get share secret reference from PV for share %s: %v", shareID, err)
	}

	provisionType, err := getProvisionTypeFromPV(pv)
	if err != nil {
		return fmt.Errorf("failed to get provision type for volume %s: %v", pv.GetName(), err)
	}

	osOptions, err := shareoptions.NewOpenStackOptionsFromSecret(p.clientset, osSecretRef)
	if err != nil {
		return fmt.Errorf("failed to create OpenStack options for share %s: %v", shareID, err)
	}

	client, err := manilaclient.New(osOptions, p.userAgent, p.extraUserAgentData)
	if err != nil {
		return fmt.Errorf("failed to create Manila v2 client for share %s: %v", shareID, err)
	}

	// Share deletion

	if err = deleteShare(shareID, provisionType, shareSecretRef, client, p.clientset); err != nil {
		return fmt.Errorf("failed to delete share %s: %v", shareID, err)
	}

	klog.Infof("successfully deleted share %s", shareID)

	return nil
}
