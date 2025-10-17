<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [CSI Cinder driver](#csi-cinder-driver)
  - [CSI Compatibility](#csi-compatibility)
  - [Downloads](#downloads)
  - [Kubernetes Compatibility](#kubernetes-compatibility)
  - [Driver Deployment](#driver-deployment)
    - [Command-line arguments](#command-line-arguments)
  - [Driver Config](#driver-config)
    - [Global](#global)
    - [Block Storage](#block-storage)
    - [Metadata](#metadata)
    - [Using the manifests](#using-the-manifests)
    - [Using the Helm chart](#using-the-helm-chart)
  - [Supported Features](#supported-features)
  - [Sidecar Compatibility](#sidecar-compatibility)
  - [Supported Parameters](#supported-parameters)
  - [Supported PVC Annotations](#supported-pvc-annotations)
  - [Local Development](#local-development)
    - [Build](#build)
    - [Testing](#testing)
      - [Unit Tests](#unit-tests)
      - [Sanity Tests](#sanity-tests)
  - [In-tree Cinder provisioner to cinder CSI Migration](#in-tree-cinder-provisioner-to-cinder-csi-migration)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# CSI Cinder driver

The Cinder CSI Driver is a CSI Specification compliant driver used by Container Orchestrators to manage the lifecycle of OpenStack Cinder Volumes.

## CSI Compatibility

This plugin is compatible with CSI v1.8.0

## Downloads

Stable released version images of the plugin can be pulled from `registry.k8s.io/provider-os/cinder-csi-plugin:[release tag]`

## Kubernetes Compatibility

For each kubernetes official release, there is a corresponding release of Cinder CSI driver which is compatible with k8s release. It is recommended to use the corresponding version w.r.t kubernetes.

For sidecar version compatibility with kubernetes, please refer [Compatibility Matrix](https://kubernetes-csi.github.io/docs/sidecar-containers.html) of each sidecar.

## Driver Deployment

You can either use the manifests under `manifests/cinder-csi-plugin` or the Helm chart `charts/cinder-csi-plugin`.

### Command-line arguments

In addition to the standard set of klog flags, `cinder-csi-plugin` accepts the following command-line arguments:

<dl>
  <dt>--nodeid &lt;node id&gt;</dt>
  <dd>
  This argument is deprecated. It will be removed in future.

  An identifier for the current node which will be used in OpenStack API calls.  This can be either the UUID or name of the OpenStack server, but note that if using name it must be unique.
  </dd>

  <dt>--endpoint &lt;endpoint&gt;</dt>
  <dd>
  This argument is required.

  The endpoint of the gRPC server agents will use to connect to this CSI plugin, typically a local unix socket.

  The manifests default this to `unix://csi/csi.sock`, which is supplied via the `CSI_ENDPOINT` environment variable.
  </dd>

  <dt>--with-topology &lt;enabled&gt;</dt>
  <dd>
  If set to true then the CSI driver reports topology information and the controller respects it.

  Defaults to `true` (enabled).
  </dd>

  <dt>--cloud-config &lt;config file&gt; [--cloud-config &lt;config file&gt; ...]</dt>
  <dd>
  This argument must be given at least once.

  The path to a driver config file. The format of this file is specified in [Driver Config](#driver-config).

  If multiple configuration files are supplied they will be merged. In the case of a conflict, the last supplied config file will take precedence.

  The manifests default this to `/etc/config/cloud.conf`, which is supplied via the `CLOUD_CONFIG` environment variable.
  </dd>

  <dt>--cluster &lt;cluster name&gt;</dt>
  <dd>
  This argument is optional.

  The identifier of the cluster that the plugin is running in.

  This will be added as metadata to every Cinder volume created by this plugin.
  </dd>

  <dt>--http-endpoint &lt;HTTP server&gt;</dt>
  <dd>
  This argument is optional.

  The TCP network address where the HTTP server for providing metrics for diagnostics, will listen (example: `:8080`).

  The default is empty string, which means the server is disabled.
  </dd>

  <dt>--provide-controller-service &lt;enabled&gt;</dt>
  <dd>
  If set to true then the CSI driver provides the controller service.

  Defaults to `true` (enabled).
  </dd>

  <dt>--provide-node-service &lt;enabled&gt;</dt>
  <dd>
  If set to true then the CSI driver provides the node service.

  Defaults to `true` (enabled).
  </dd>

  <dt>--pvc-annotations &lt;disabled&gt;</dt>
  <dd>
  If set to true then the CSI driver will use PVC annotations to provide volume
  scheduler hints. See [Supported PVC Annotations](#supported-pvc-annotations)
  for more information.

  Defaults to `false` (disabled).
  </dd>
</dl>

## Driver Config

Implementation of `cinder-csi-plugin` relies on following OpenStack services.

| Service                        | API Version(s) | Deprecated | Required |
|--------------------------------|----------------|------------|----------|
| Identity (Keystone)            | v3             | No         | Yes      |
| Compute (Nova)                 | v2             | No         | Yes      |
| Block Storage (Cinder)         | v3             | No         | Yes      |


For Driver configuration, parameters must be passed via configuration file specified in `$CLOUD_CONFIG` environment variable.
The following sections are supported in configuration file.

### Global
For Cinder CSI Plugin to authenticate with OpenStack Keystone, required parameters needs to be passed in `[Global]` section of the file. For all supported parameters, please refer [Global](../openstack-cloud-controller-manager/using-openstack-cloud-controller-manager.md#global) section.

### Block Storage
These configuration options pertain to block storage and should appear in the `[BlockStorage]` section of the `$CLOUD_CONFIG` file.

* `node-volume-attach-limit`
  Optional. To configure maximum volumes that can be attached to the node. Defaults to `256`.
* `rescan-on-resize`
  Optional. Set to `true` to rescan block device and verify its size before expanding the filesystem. Not all hypervisors have a `/sys/class/block/XXX/device/rescan` location, therefore if you enable this option and your hypervisor doesn't support this, you'll get a warning log on resize event. It is recommended to disable this option in this case. Defaults to `false`
* `ignore-volume-az`
  Optional. Set to `true` if your set of Block Storage (Cinder) AZs does not match your set of Compute (Nova) AZs and you are manually setting the `topology` parameter on your Storage Class(es). For more information, refer to [When trying to use the topology feature, pods are not able to schedule](./troubleshooting.md#when-trying-to-use-the-topology-feature-pods-are-not-able-to-schedule). Defaults to `false`.
* `ignore-volume-microversion`
  Optional. Set to `true` only when your cinder microversion is older than 3.34. This might cause some features to not work as expected, but aims to allow basic operations like creating a volume. Defaults to `false`

### Metadata
These configuration options pertain to metadata and should appear in the `[Metadata]` section of the `$CLOUD_CONFIG` file.

* `search-order`: This configuration key influences the way that the driver retrieves metadata relating to the instance (s) in which it runs. The default value of `configDrive,metadataService` results in the provider retrieving metadata relating to the instance from the config drive first if available and then the metadata service. Alternative values are:
  * `configDrive` - Only retrieve instance metadata from the configuration
    drive.
  * `metadataService` - Only retrieve instance metadata from the metadata
    service.
  * `metadataService,configDrive` - Retrieve instance metadata from the metadata
    service first if available, then the configuration drive.

  Influencing this behavior may be desirable as the metadata on the configuration drive may grow stale over time, whereas the metadata service always provides the most up to date view. Not all OpenStack clouds provide both configuration drive and metadata service though and only one or the other may be available which is why the default is to check both.

### Using the manifests

All the manifests required for the deployment of the plugin are found at `manifests/cinder-csi-plugin`

Configuration file specified in `$CLOUD_CONFIG` is passed to cinder CSI driver via kubernetes `secret`. If the secret `cloud-config` is already created in the cluster, you can remove the file, `manifests/cinder-csi-plugin/csi-secret-cinderplugin.yaml` and directly proceed to the step of creating controller and node plugins.

To create a secret:

* Encode your `$CLOUD_CONFIG` file content using base64.

`$ base64 -w 0 $CLOUD_CONFIG`

* Update ```cloud.conf``` configuration in ```manifests/cinder-csi-plugin/csi-secret-cinderplugin.yaml``` file
by using the result of the above command.

* Create the secret.

``` $ kubectl create -f manifests/cinder-csi-plugin/csi-secret-cinderplugin.yaml```

This should create a secret name `cloud-config` in `kube-system` namespace.

Once the secret is created, Controller Plugin and Node Plugins can be deployed using respective manifests

```$ kubectl -f manifests/cinder-csi-plugin/ apply```

This creates a set of cluster roles, cluster role bindings, and statefulsets etc to communicate with openstack(cinder).
For detailed list of created objects, explore the yaml files in the directory.
You should make sure following similar pods are ready before proceed:

```
$ kubectl get pods -n kube-system
NAME                                READY   STATUS    RESTARTS   AGE
csi-cinder-controllerplugin         6/6     Running   0          29h
csi-cinder-nodeplugin               3/3     Running   0          46h
```

To get information about CSI Drivers running in a cluster -

```
$ kubectl get csidrivers.storage.k8s.io
NAME                       ATTACHREQUIRED   PODINFOONMOUNT   STORAGECAPACITY   TOKENREQUESTS   REQUIRESREPUBLISH   MODES                  AGE
cinder.csi.openstack.org   true             true             false             <unset>         false               Persistent,Ephemeral   19h

```

> NOTE: If using certs(`ca-file`), make sure to add the additional mount to the manifests (controller and node plugin) to mount the location of certs as volume onto container. For example, add `ca-cert` in `/etc/cacert` folder. Uncomment the related sections in `manifests/cinder-csi-plugin/cinder-csi-controllerplugin.yaml` and `manifests/cinder-csi-plugin/cinder-csi-nodeplugin.yaml` and replace the path with your own.

```
       volumeMounts:
          ....
          - name: cacert
              mountPath: /etc/cacert
              readOnly: true

     volumes:
        ....
        - name: cacert
          hostPath:
            path: /etc/cacert
```

### Using the Helm chart

> NOTE: With default values, this chart assumes that the `cloud.conf` is found on the host under `/etc/kubernetes/` and that your OpenStack cloud has cert under `/etc/cacert`.

You can specify a K8S Secret for `cloud.conf` :
```
secret:
  enabled: true
  name: yousecretname
```

You can also tell Helm to create the K8S Secret :

```
secret:
  enabled: true
  name: cinder-csi-cloud-config
  data:
    cloud.conf: |-
      ...
```


To install the chart, use the following command:
```
helm install --namespace kube-system --name cinder-csi ./charts/cinder-csi-plugin
```

## Supported Features

* [Dynamic Provisioning](./features.md#dynamic-provisioning)
* [Topology](./features.md#topology)
* [Raw Block Volume](./features.md#block-volume)
* [Volume Expansion](./features.md#volume-expansion)
* [Volume Cloning](./features.md#volume-cloning)
* [Volume Snapshots](./features.md#volume-snapshots)
* [Ephemeral Volumes](./features.md#inline-volumes)
* [Multiattach Volumes](./features.md#multi-attach-volumes)
* [Liveness probe](./features.md#liveness-probe)

## Sidecar Compatibility

* [Set file type in provisioner](./sidecarcompatibility.md#set-file-type-in-provisioner)

## Supported Parameters

| Parameter Type             | Parameter Name       |   Default       |Description      |
|-------------------------   |-----------------------|-----------------|-----------------|
| StorageClass `parameters`  | `availability`          | `nova`          | String. Volume Availability Zone |
| StorageClass `parameters`  | `type`                  | Empty String    | String. Name/ID of Volume type. Corresponding volume type should exist in cinder     |
| VolumeSnapshotClass `parameters` | `force-create`    | `false`         | Enable to support creating snapshot for a volume in in-use status |
| VolumeSnapshotClass `parameters` | `type`            | Empty String    | `snapshot` creates a VolumeSnapshot object linked to a Cinder volume snapshot. `backup` creates a VolumeSnapshot object linked to a cinder volume backup. Defaults to `snapshot` if not defined |
| VolumeSnapshotClass `parameters` | `backup-max-duration-seconds-per-gb`  | `20`    | Defines the amount of time to wait for a backup to complete in seconds per GB of volume size |
| VolumeSnapshotClass `parameters`  | `availability`          | Same as volume | String. Backup Availability Zone |
| Inline Volume `volumeAttributes`   | `capacity`              | `1Gi`       | volume size for creating inline volumes|
| Inline Volume `VolumeAttributes`   | `type`              | Empty String  | Name/ID of Volume type. Corresponding volume type should exist in cinder |

## Supported PVC Annotations

The PVC annotations support must be enabled in the Cinder CSI controller with
the `--pvc-annotations` flag. The PVC annotations take effect only when the PVC
is created. The scheduler hints are not updated when the PVC is updated. The
following PVC annotations are supported:

| Annotation Name            | Description      | Example |
|-------------------------   |-----------------|----------|
| `cinder.csi.openstack.org/affinity` | Volume affinity to existing volume or volumes names/UUIDs. The value should be a comma-separated list of volume names/UUIDs. | `cinder.csi.openstack.org/affinity: "1b4e28ba-2fa1-11ec-8d3d-0242ac130003"` |
| `cinder.csi.openstack.org/anti-affinity` | Volume anti-affinity to existing volume or volumes names/UUIDs. The value should be a comma-separated list of volume names/UUIDs. | `cinder.csi.openstack.org/anti-affinity: "1b4e28ba-2fa1-11ec-8d3d-0242ac130004,pv-k8s--cluster-1b5f47bf-0119-442e-8529-254c36e43644"` |

If the PVC annotation is set, the volume will be created according to the
existing volume names/UUIDs placements, i.e. on the same host as the
`1b4e28ba-2fa1-11ec-8d3d-0242ac130003` volume and not on the same host as the
`1b4e28ba-2fa1-11ec-8d3d-0242ac130004` and
`pv-k8s--cluster-1b5f47bf-0119-442e-8529-254c36e43644` volumes.

## Local Development

### Build

To build the plugin, run

```
$ export ARCH=amd64 # Defaults to amd64
$ make build-cmd-cinder-csi-plugin
```

To build cinder-csi-plugin image

```
$ export ARCH=amd64 # Defaults to amd64
$ make build-local-image-cinder-csi-plugin
```

### Testing

#### Unit Tests
To run all unit tests:

```
$ make test
```
#### Sanity Tests
Sanity tests ensures the CSI spec conformance of the driver. For more info, refer [Sanity check](https://github.com/kubernetes-csi/csi-test/tree/master/pkg/sanity)

Run sanity tests for cinder CSI driver using:

```
$ make test-cinder-csi-sanity
```

## In-tree Cinder provisioner to cinder CSI Migration

Starting from Kubernetes 1.21, OpenStack Cinder CSI migration is supported as beta feature and is `ON` by default. Cinder CSI driver must be installed on clusters on OpenStack for Cinder volumes to work. If you have persistence volumes that are created with in-tree `kubernetes.io/cinder` plugin, you could migrate to use `cinder.csi.openstack.org` Container Storage Interface (CSI) Driver.

* The CSI Migration feature for Cinder, when enabled, shims all plugin operations from the existing in-tree plugin to the `cinder.csi.openstack.org` CSI Driver.
* For more info, please refer [Migrate to CCM with CSI Migration](../openstack-cloud-controller-manager/migrate-to-ccm-with-csimigration.md#migrate-from-in-tree-cloud-provider-to-openstack-cloud-controller-manager-and-enable-csimigration) guide
