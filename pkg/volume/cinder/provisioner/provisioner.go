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

package provisioner

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/cloud-provider-openstack/pkg/volume/cinder/volumeservice"
	"k8s.io/klog"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/controller"

	volumes_v2 "github.com/gophercloud/gophercloud/openstack/blockstorage/v2/volumes"
)

const (
	// ProvisionerName is the unique name of this provisioner
	ProvisionerName = "openstack.org/standalone-cinder"

	// ProvisionerIDAnn is an annotation to identify a particular instance of this provisioner
	ProvisionerIDAnn = "standaloneCinderProvisionerIdentity"

	// CinderVolumeIDAnn is an annotation to store the ID of the associated cinder volume
	CinderVolumeIDAnn = "cinderVolumeId"

	// CloneRequestAnn is an annotation to request that the PVC be provisioned as a clone of the referenced PVC
	CloneRequestAnn = "k8s.io/CloneRequest"

	// CloneOfAnn is an annotation to indicate that a PVC is a clone of the referenced PVC
	CloneOfAnn = "k8s.io/CloneOf"

	// SmartCloneEnabled is a provisioner parameter to enable smart clone mode for a storage class
	SmartCloneEnabled = "smartclone"
)

type cinderProvisioner struct {
	// Openstack cinder client
	VolumeService *gophercloud.ServiceClient

	// Kubernetes client. Use to create secret
	Client kubernetes.Interface
	// Identity of this cinderProvisioner, generated. Used to identify "this"
	// provisioner's PVs.
	Identity string

	vsb volumeServiceBroker
	mb  mapperBroker
	cb  clusterBroker
}

// NewCinderProvisioner returns a Provisioner that creates volumes using a
// standalone cinder instance and produces PersistentVolumes that use native
// kubernetes PersistentVolumeSources.
func NewCinderProvisioner(client kubernetes.Interface, id, configFilePath string) (controller.Provisioner, error) {
	volumeService, err := volumeservice.GetVolumeService(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume service: %v", err)
	}

	return &cinderProvisioner{
		VolumeService: volumeService,
		Client:        client,
		Identity:      id,
		vsb:           &gophercloudBroker{},
		mb:            &volumeMapperBroker{},
		cb:            &k8sClusterBroker{},
	}, nil
}

func (p *cinderProvisioner) getCreateOptions(options controller.ProvisionOptions) (volumes_v2.CreateOpts, error) {
	name := fmt.Sprintf("cinder-dynamic-pvc-%s", uuid.NewUUID())
	capacity := options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]
	sizeBytes := capacity.Value()
	// Cinder works with gigabytes, convert to GiB with rounding up
	sizeGB := int((sizeBytes + 1024*1024*1024 - 1) / (1024 * 1024 * 1024))
	volType := ""
	availability := "nova"
	cloneEnabled := false
	// Apply ProvisionerParameters (case-insensitive). We leave validation of
	// the values to the cloud provider.
	for k, v := range options.StorageClass.Parameters {
		switch strings.ToLower(k) {
		case "type":
			volType = v
		case "availability":
			availability = v
		case SmartCloneEnabled:
			cloneEnabled = true
		default:
			return volumes_v2.CreateOpts{}, fmt.Errorf("invalid option %q", k)
		}
	}

	sourceVolID := ""
	if cloneEnabled {
		if sourcePVCRef, ok := options.PVC.Annotations[CloneRequestAnn]; ok {
			var ns string
			parts := strings.SplitN(sourcePVCRef, "/", 2)
			if len(parts) < 2 {
				ns = options.PVC.Namespace
			} else {
				ns = parts[0]
			}
			sourcePVCName := parts[len(parts)-1]
			sourcePVC, err := p.cb.getPVC(p, ns, sourcePVCName)
			if err != nil {
				return volumes_v2.CreateOpts{}, fmt.Errorf("Unable to get PVC %s/%s", ns, sourcePVCName)
			}
			if sourceVolID, ok = sourcePVC.Annotations[CinderVolumeIDAnn]; ok {
				klog.Infof("Requesting clone of cinder volumeID %s", sourceVolID)
			} else {
				return volumes_v2.CreateOpts{}, fmt.Errorf("PVC %s/%s missing %s annotation",
					ns, sourcePVCName, CinderVolumeIDAnn)
			}
		}
	}

	return volumes_v2.CreateOpts{
		Name:             name,
		Size:             sizeGB,
		VolumeType:       volType,
		AvailabilityZone: availability,
		SourceVolID:      sourceVolID,
	}, nil
}

func (p *cinderProvisioner) annotatePVC(cinderVolID string, pvc *v1.PersistentVolumeClaim, createOptions volumes_v2.CreateOpts) error {
	annotations := make(map[string]string, 2)
	annotations[CinderVolumeIDAnn] = cinderVolID

	// Add clone annotation if this is a cloned volume
	if sourcePVCName, ok := pvc.Annotations[CloneRequestAnn]; ok {
		if createOptions.SourceVolID != "" {
			klog.Infof("Annotating PVC %s/%s as a clone of PVC %s/%s",
				pvc.Namespace, pvc.Name, pvc.Namespace, sourcePVCName)
			annotations[CloneOfAnn] = sourcePVCName
		}
	}
	err := p.cb.annotatePVC(p, pvc.Namespace, pvc.Name, annotations)
	return err
}

// Provision creates a storage asset and returns a PV object representing it.
func (p *cinderProvisioner) Provision(options controller.ProvisionOptions) (*v1.PersistentVolume, error) {
	var (
		volumeID   string
		connection volumeservice.VolumeConnection
		mapper     volumeMapper
		pv         *v1.PersistentVolume
		cleanupErr error
	)

	if options.PVC.Spec.Selector != nil {
		return nil, fmt.Errorf("claim Selector is not supported")
	}

	// TODO: Check access mode
	createOptions, err := p.getCreateOptions(options)
	if err != nil {
		klog.Error(err)
		goto ERROR
	}
	volumeID, err = p.vsb.createCinderVolume(p.VolumeService, createOptions)
	if err != nil {
		klog.Errorf("Failed to create volume")
		goto ERROR
	}

	err = p.vsb.waitForAvailableCinderVolume(p.VolumeService, volumeID)
	if err != nil {
		klog.Errorf("Volume %s did not become available", volumeID)
		goto ERROR_DELETE
	}

	err = p.vsb.reserveCinderVolume(p.VolumeService, volumeID)
	if err != nil {
		klog.Errorf("Failed to reserve volume %s: %v", volumeID, err)
		goto ERROR_DELETE
	}

	connection, err = p.vsb.connectCinderVolume(p.VolumeService, initiatorName, volumeID)
	if err != nil {
		klog.Errorf("Failed to connect volume %s: %v", volumeID, err)
		goto ERROR_UNRESERVE
	}

	err = p.vsb.attachCinderVolume(p.VolumeService, volumeID)
	if err != nil {
		klog.Errorf("Failed to attach volume %s: %v", volumeID, err)
		goto ERROR_DISCONNECT
	}

	mapper, err = p.mb.newVolumeMapperFromConnection(connection)
	if err != nil {
		klog.Errorf("Unable to create volume mapper: %v", err)
		goto ERROR_DETACH
	}

	err = mapper.AuthSetup(p, options, connection)
	if err != nil {
		klog.Errorf("Failed to prepare volume auth: %v", err)
		goto ERROR_DETACH
	}

	pv, err = p.mb.buildPV(mapper, p, options, connection, volumeID)
	if err != nil {
		klog.Errorf("Failed to build PV: %v", err)
		goto ERROR_DETACH
	}

	err = p.annotatePVC(volumeID, options.PVC, createOptions)
	if err != nil {
		klog.Errorf("Failed to annotate cloned PVC: %v", err)
		goto ERROR_DETACH
	}

	return pv, nil

ERROR_DETACH:
	cleanupErr = p.vsb.detachCinderVolume(p.VolumeService, volumeID)
	if cleanupErr != nil {
		klog.Errorf("Failed to detach volume %s: %v", volumeID, cleanupErr)
	}
ERROR_DISCONNECT:
	cleanupErr = p.vsb.disconnectCinderVolume(p.VolumeService, initiatorName, volumeID)
	if cleanupErr != nil {
		klog.Errorf("Failed to disconnect volume %s: %v", volumeID, cleanupErr)
	}
	klog.V(3).Infof("Volume %s disconnected", volumeID)
ERROR_UNRESERVE:
	cleanupErr = p.vsb.unreserveCinderVolume(p.VolumeService, volumeID)
	if cleanupErr != nil {
		klog.Errorf("Failed to unreserve volume %s: %v", volumeID, cleanupErr)
	}
	klog.V(3).Infof("Volume %s unreserved", volumeID)
ERROR_DELETE:
	cleanupErr = p.vsb.deleteCinderVolume(p.VolumeService, volumeID)
	if cleanupErr != nil {
		klog.Errorf("Failed to delete volume %s: %v", volumeID, cleanupErr)
	}
	klog.V(3).Infof("Volume %s deleted", volumeID)
ERROR:
	return nil, err // Return the original error
}

// Delete removes the storage asset that was created by Provision represented
// by the given PV.
func (p *cinderProvisioner) Delete(pv *v1.PersistentVolume) error {
	ann, ok := pv.Annotations[ProvisionerIDAnn]
	if !ok {
		return errors.New("identity annotation not found on PV")
	}
	if ann != p.Identity {
		return &controller.IgnoredError{
			Reason: "identity annotation on PV does not match ours",
		}
	}
	// TODO when beta is removed, have to check kube version and pick v1/beta
	// accordingly: maybe the controller lib should offer a function for that

	volumeID, ok := pv.Annotations[CinderVolumeIDAnn]
	if !ok {
		return errors.New(CinderVolumeIDAnn + " annotation not found on PV")
	}

	mapper, err := p.mb.newVolumeMapperFromPV(pv)
	if err != nil {
		klog.Errorf("Failed to instantiate mapper: %s", err)
		return err
	}

	mapper.AuthTeardown(p, pv)

	err = p.vsb.detachCinderVolume(p.VolumeService, volumeID)
	if err != nil {
		klog.Errorf("Failed to detach volume %s: %v", volumeID, err)
		return err
	}

	err = p.vsb.disconnectCinderVolume(p.VolumeService, initiatorName, volumeID)
	if err != nil {
		klog.Errorf("Failed to disconnect volume %s: %v", volumeID, err)
		return err
	}

	err = p.vsb.unreserveCinderVolume(p.VolumeService, volumeID)
	if err != nil {
		// TODO: Create placeholder PV?
		klog.Errorf("Failed to unreserve volume %s: %v", volumeID, err)
		return err
	}

	err = p.vsb.deleteCinderVolume(p.VolumeService, volumeID)
	if err != nil {
		klog.Errorf("Failed to delete volume %s: %v", volumeID, err)
		return err
	}

	klog.V(2).Infof("Successfully deleted cinder volume %s", volumeID)
	return nil
}
