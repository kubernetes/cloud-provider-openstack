<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Plugin Features](#plugin-features)
  - [Dynamic Provisioning](#dynamic-provisioning)
  - [Topology](#topology)
  - [Block Volume](#block-volume)
  - [Volume Expansion](#volume-expansion)
    - [Rescan on in-use volume resize](#rescan-on-in-use-volume-resize)
  - [Volume Snapshots](#volume-snapshots)
  - [Ephemeral Volumes](#ephemeral-volumes)
    - [[DEPRECATED] CSI Ephemeral Volumes](#deprecated-csi-ephemeral-volumes)
    - [Generic Ephemeral Volumes](#generic-ephemeral-volumes)
  - [Volume Cloning](#volume-cloning)
  - [Multi-Attach Volumes](#multi-attach-volumes)
  - [Liveness probe](#liveness-probe)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Plugin Features

## Dynamic Provisioning

Dynamic Provisioning uses persistence volume claim (PVC) to request the Kubernetes to create the Cinder volume on behalf of user and consumes the volume from inside container.

For usage, refer [sample app](./examples.md#dynamic-volume-provisioning)  

## Topology

This feature enables driver to consider the topology constraints while creating the volume. For more info, refer [Topology Support](https://github.com/kubernetes-csi/external-provisioner/blob/master/README.md#topology-support)

* Enabled by default
* Supported topology keys:
  `topology.cinder.csi.openstack.org/zone` : Availability by Zone
* `allowedTopologies` can be specified in storage class to restrict the topology of provisioned volumes to specific zones and should be used as replacement of `availability` parameter.
* To disable: set `--feature-gates=Topology=false` in external-provisioner (container `csi-provisioner` of `csi-cinder-controllerplugin`).
  * If using Helm, it can be disabled by setting `Values.csi.provisioner.topology: "false"` 

For usage, refer [sample app](./examples.md#use-topology)

## Block Volume

Cinder volumes to be exposed inside containers as a block device instead of as a mounted file system. The corresponding CSI feature (CSIBlockVolume) is GA since Kubernetes v1.18.

Prerequisites to use the feature:
* Make sure the volumeMode is `Block` in Persistence Volume Claim Spec
* Make sure the pod consuming the Block device PVC use `volumeDevices` is used instead of `volumeMounts`

For usage, refer [sample app](./examples.md#using-block-volume)

## Volume Expansion

Driver supports both `Offline` and `Online` resize of cinder volumes. Cinder online resize support is available since cinder 3.42 microversion. 
The same should be supported by underlying OpenStack Cloud to avail the feature.

* As of Kubernetes v1.16, Volume Expansion is a beta feature and enabled by default.
* Make sure to set `allowVolumeExpansion` to `true` in Storage class spec.

For usage, refer [sample app](./examples.md#volume-expansion-example)

### Rescan on in-use volume resize

Some hypervisors (like VMware) don't automatically send a new volume size to a Linux kernel, when a volume is in-use. Sending a "1" to `/sys/class/block/XXX/device/rescan` is telling the SCSI block device to refresh it's information about where it's ending boundary is (among other things) to give the kernel information about it's updated size. When a `rescan-on-resize` flag is set in a CSI node driver cloud-config `[BlockStorage]` section, a CSI node driver will rescan block device and verify its size before expanding the filesystem. CSI driver will raise an error, when expected volume size cannot be detected.

Not all hypervisors have a `/sys/class/block/XXX/device/rescan` location, therefore if you enable this option and your hypervisor doesn't support this, you'll get a warning log on resize event. It is recommended to disable this option in this case.

## Volume Snapshots

This feature enables creating volume snapshots and restore volume from snapshot. The corresponding CSI feature (VolumeSnapshotDataSource) is GA since Kubernetes v1.20.

* To avail the feature. deploy the snapshot-controller and CRDs as part of their Kubernetes cluster management process (independent of any CSI Driver) . For more info, refer [Snapshot Controller](https://kubernetes-csi.github.io/docs/snapshot-controller.html)
* For example on using snapshot feature, refer [sample app](./examples.md#snapshot-create-and-restore)

## Ephemeral Volumes

Two different Kubernetes features allow volumes to follow the Pod's lifecycle: CSI Ephemeral Volumes and Generic Ephemeral Volumes

### [DEPRECATED] CSI Ephemeral Volumes

**Note: This feature support is deprecated in v1.24 release in favor of [Generic Ephemeral Volumes](#generic-ephemeral-volumes) and will no longer be available from v1.27.**

This feature allows CSI volumes to be directly embedded in the Pod specification instead of a PersistentVolume. Volumes specified in this way are ephemeral and do not persist across Pod restarts.

* As of Kubernetes v1.16 this feature is beta so enabled by default. 
* To enable this feature for CSI Driver, `volumeLifecycleModes` needs to be specified in [CSIDriver](../../manifests/cinder-csi-plugin/csi-cinder-driver.yaml) object. The driver can run in `Persistent` mode, `Ephemeral` or in both modes.
* `podInfoOnMount` must be `true` to use this feature.
* For usage, refer [sample app](./examples.md#deploy-app-using-inline-volumes)

### Generic Ephemeral Volumes

As of Kubernetes v1.21, this is beta feature and enabled by default.

The key design idea is that the parameters for a volume claim are allowed inside a volume source of the Pod. For sample app, refer [here](./examples.md#generic-ephemeral-volumes)

## Volume Cloning

This feature enables cloning a volume from existing PVCs in Kubernetes. As of Kubernetes v1.16, volume cloning is beta feature and enabled by default.

Prerequisites:
* The source PVC must be bound and available (not in use).
* Source and destination PVCs must be in the same namespace.
* Cloning is only supported within the same Storage Class. Destination volume must be the same storage class as the source

For example, refer [sample app](../../examples/cinder-csi-plugin/clone)

## Multi-Attach Volumes

To avail the multiattach feature of cinder, specify the ID/name of cinder volume type that includes an extra-spec capability setting of `multiattach=<is> True` in storage class `type` parameter.

This volume type must exist in cinder already (`openstack volume type list`)

This should enable to attach a volume to multiple hosts/servers simultaneously.

## Liveness probe

The [liveness probe](https://github.com/kubernetes-csi/livenessprobe) is a sidecar container that exposes an HTTP `/healthz` endpoint, which serves as kubelet's `livenessProbe` hook to monitor health of a CSI driver.

Cinder CSI driver added liveness probe side container by default and refer to [manifest](../../manifests/cinder-csi-plugin/cinder-csi-controllerplugin.yaml) and [charts](../../charts/cinder-csi-plugin) for more information.
