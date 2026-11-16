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

package cinder

import (
	"context"
	"fmt"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/cloud-provider-openstack/pkg/util/brick"
	"k8s.io/cloud-provider-openstack/pkg/util/metadata"
	"k8s.io/cloud-provider-openstack/pkg/util/mount"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/klog/v2"
)

const (
	driverName  = "cinder.csi.openstack.org"
	topologyKey = "topology." + driverName + "/zone"

	// maxVolumesPerNode is the maximum number of volumes that can be attached to a node
	maxVolumesPerNode = 256

	// ResizeRequired parameter, if set to true, will trigger a resize on mount operation
	ResizeRequired = driverName + "/resizeRequired"

	// AttachModeNova uses Nova attach/detach for volume operations (default).
	AttachModeNova = "nova"
	// AttachModeDirect uses Cinder Attachment API with os-brick sidecar
	// for direct attachment (typically bare-metal nodes).
	AttachModeDirect = "direct"

	// snapshotMetadataProbeTimeout bounds the Block Storage API version
	// discovery made for each cloud when the snapshot metadata service is
	// set up.
	snapshotMetadataProbeTimeout = 30 * time.Second
)

var (
	// CSI spec version
	specVersion = "1.8.0"

	// Driver version
	// Version history:
	// * 1.3.0: Up to version 1.3.0 driver version was the same as CSI spec version
	// * 1.3.1: Bump for 1.21 release
	// * 1.3.2: Allow --cloud-config to be given multiple times
	// * 1.3.3: Bump for 1.22 release
	// * 2.0.0: Bump for 1.23 release
	Version = "2.0.0"
)

// Deprecated: use Driver instead
//
//revive:disable:exported
type CinderDriver = Driver

//revive:enable:exported

type Driver struct {
	name         string
	fqVersion    string //Fully qualified version in format {Version}@{CPO version}
	endpoint     string
	clusterID    string
	withTopology bool
	attachMode   string

	// withSnapshotMetadata records the operator's request for the
	// SnapshotMetadata service. It is cleared when no configured cloud can
	// serve it, so that the capability is never advertised without a backend.
	withSnapshotMetadata bool

	ids *identityServer
	cs  *controllerServer
	ns  *nodeServer
	sms *snapshotMetadataServer

	vcap  []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
	nscap []*csi.NodeServiceCapability

	pvcLister v1.PersistentVolumeClaimLister
}

type DriverOpts struct {
	ClusterID    string
	Endpoint     string
	WithTopology bool
	AttachMode   string

	// WithSnapshotMetadata provides the CSI SnapshotMetadata service on top
	// of the Cinder os-list_changed_blocks volume action.
	WithSnapshotMetadata bool

	PVCLister v1.PersistentVolumeClaimLister
}

func NewDriver(o *DriverOpts) *Driver {
	attachMode := o.AttachMode
	if attachMode == "" {
		attachMode = AttachModeNova
	}

	d := &Driver{
		name:                 driverName,
		fqVersion:            fmt.Sprintf("%s@%s", Version, version.Version),
		endpoint:             o.Endpoint,
		clusterID:            o.ClusterID,
		withTopology:         o.WithTopology,
		attachMode:           attachMode,
		withSnapshotMetadata: o.WithSnapshotMetadata,
		pvcLister:            o.PVCLister,
	}

	klog.Info("Driver: ", d.name)
	klog.Info("Driver version: ", d.fqVersion)
	klog.Info("CSI Spec version: ", specVersion)
	klog.Infof("Topology awareness: %t", d.withTopology)
	klog.Infof("Snapshot metadata service: %t", d.withSnapshotMetadata)

	d.AddControllerServiceCapabilities(
		[]csi.ControllerServiceCapability_RPC_Type{
			csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
			csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
			csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
			csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
			csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
			csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
			csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
			csi.ControllerServiceCapability_RPC_LIST_VOLUMES_PUBLISHED_NODES,
			csi.ControllerServiceCapability_RPC_GET_VOLUME,
		})
	d.AddVolumeCapabilityAccessModes(
		[]csi.VolumeCapability_AccessMode_Mode{
			csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		})

	// ignoring error, because AddNodeServiceCapabilities is public
	// and so potentially used somewhere else.
	_ = d.AddNodeServiceCapabilities(
		[]csi.NodeServiceCapability_RPC_Type{
			csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
			csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
			csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		})

	d.ids = NewIdentityServer(d)

	return d
}

func (d *Driver) AddControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) {
	csc := make([]*csi.ControllerServiceCapability, 0, len(cl))

	for _, c := range cl {
		klog.Infof("Enabling controller service capability: %v", c.String())
		csc = append(csc, NewControllerServiceCapability(c))
	}

	d.cscap = csc
}

func (d *Driver) AddVolumeCapabilityAccessModes(vc []csi.VolumeCapability_AccessMode_Mode) []*csi.VolumeCapability_AccessMode {
	vca := make([]*csi.VolumeCapability_AccessMode, 0, len(vc))

	for _, c := range vc {
		klog.Infof("Enabling volume access mode: %v", c.String())
		vca = append(vca, NewVolumeCapabilityAccessMode(c))
	}

	d.vcap = vca

	return vca
}

func (d *Driver) AddNodeServiceCapabilities(nl []csi.NodeServiceCapability_RPC_Type) error {
	nsc := make([]*csi.NodeServiceCapability, 0, len(nl))

	for _, n := range nl {
		klog.Infof("Enabling node service capability: %v", n.String())
		nsc = append(nsc, NewNodeServiceCapability(n))
	}

	d.nscap = nsc

	return nil
}

func (d *Driver) ValidateControllerServiceRequest(c csi.ControllerServiceCapability_RPC_Type) error {
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}

	for _, cap := range d.cscap {
		if c == cap.GetRpc().GetType() {
			return nil
		}
	}

	return status.Error(codes.InvalidArgument, c.String())
}

func (d *Driver) GetVolumeCapabilityAccessModes() []*csi.VolumeCapability_AccessMode {
	return d.vcap
}

// IsDirectMode returns true when the driver is configured to attach
// volumes directly via Cinder + os-brick instead of through Nova.
func (d *Driver) IsDirectMode() bool {
	return d.attachMode == AttachModeDirect
}

func (d *Driver) SetupControllerService(clouds map[string]openstack.IOpenStack, connProps ConnectorPropertiesGetter) {
	klog.Info("Providing controller service")
	d.cs = NewControllerServer(d, clouds, connProps)
}

func (d *Driver) SetupNodeService(mount mount.IMount, metadata metadata.IMetadata, opts openstack.BlockStorageOpts, topologies map[string]string, connector brick.IConnector, kubeClient kubernetes.Interface, nodeName string, clouds map[string]openstack.IOpenStack) {
	klog.Info("Providing node service")
	d.ns = NewNodeServer(d, mount, metadata, opts, topologies, connector, kubeClient, nodeName, clouds)
}

// SetupSnapshotMetadataService provides the CSI SnapshotMetadata service when
// the driver was started with it. Each cloud is asked whether its Block
// Storage API offers the microversion that carries os-list_changed_blocks. A
// cloud that definitively lacks it is reported, and when every cloud lacks it
// the service is withdrawn so that the SNAPSHOT_METADATA_SERVICE capability
// is not advertised. A failed probe counts as unknown rather than as
// unsupported, because a transient API fault at startup must not remove the
// capability for the lifetime of the process.
func (d *Driver) SetupSnapshotMetadataService(clouds map[string]openstack.IOpenStack) {
	if !d.withSnapshotMetadata {
		return
	}

	unsupported := 0
	for name, cloud := range clouds {
		ctx, cancel := context.WithTimeout(context.Background(), snapshotMetadataProbeTimeout)
		supported, err := cloud.ChangedBlocksSupported(ctx)
		cancel()

		switch {
		case err != nil:
			klog.Warningf("Snapshot metadata: cannot determine whether cloud %q supports Block Storage API microversion %s, assuming it does: %v", name, openstack.ChangedBlocksMicroversion, err)
		case !supported:
			unsupported++
			klog.Warningf("Snapshot metadata: cloud %q does not support Block Storage API microversion %s; snapshot metadata requests for this cloud will fail", name, openstack.ChangedBlocksMicroversion)
		default:
			klog.Infof("Snapshot metadata: cloud %q supports Block Storage API microversion %s", name, openstack.ChangedBlocksMicroversion)
		}
	}

	if len(clouds) > 0 && unsupported == len(clouds) {
		klog.Warningf("Snapshot metadata: no configured cloud supports Block Storage API microversion %s; the service is not provided and the external-snapshot-metadata sidecar will refuse to start", openstack.ChangedBlocksMicroversion)
		d.withSnapshotMetadata = false
		return
	}

	klog.Info("Providing snapshot metadata service")
	d.sms = NewSnapshotMetadataServer(d, clouds)
}

func (d *Driver) Run() {
	if nil == d.cs && nil == d.ns {
		klog.Fatal("No CSI services initialized")
	}

	// A nil *snapshotMetadataServer stored in an interface is not a nil
	// interface, so the service is handed over only when it exists.
	var sms csi.SnapshotMetadataServer
	if d.sms != nil {
		sms = d.sms
	}

	RunServicesInitialized(d.endpoint, d.ids, d.cs, d.ns, sms)
}
