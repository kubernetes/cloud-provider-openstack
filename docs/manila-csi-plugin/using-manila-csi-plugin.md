<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [CSI Manila driver](#csi-manila-driver)
  - [Configuration](#configuration)
    - [Command line arguments](#command-line-arguments)
    - [Controller Service volume parameters](#controller-service-volume-parameters)
    - [Node Service volume context](#node-service-volume-context)
    - [Secrets, authentication](#secrets-authentication)
    - [Topology-aware dynamic provisioning](#topology-aware-dynamic-provisioning)
    - [Runtime configuration file](#runtime-configuration-file)
  - [Deployment](#deployment)
    - [Kubernetes 1.17+](#kubernetes-117)
      - [Verifying the deployment](#verifying-the-deployment)
      - [Enabling topology awareness](#enabling-topology-awareness)
  - [Share protocol support matrix](#share-protocol-support-matrix)
  - [Supported PVC annotations](#supported-pvc-annotations)
  - [For developers](#for-developers)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# CSI Manila driver

The CSI Manila driver is able to create, expand, snapshot, restore and mount OpenStack Manila shares.

Currently supported Manila backends are NFS and native CephFS.

## Configuration

### Command line arguments

Option | Default value | Description
-------|---------------|------------
`--endpoint` | `unix:///tmp/csi.sock` | CSI Manila's CSI endpoint
`--drivername` | `manila.csi.openstack.org` | Name of this driver
`--nodeid` | _none_ | **DEPRECATED** ID of this node. This value is now automatically retrieved from the metadata service.
`--nodeaz` | _none_ | **DEPRECATED** Availability zone of this node. This value is now automatically retrieved from the metadata service.
`--runtime-config-file` | _none_ | Path to the [runtime configuration file](#runtime-configuration-file)
`--with-topology` | _none_ | CSI Manila is topology-aware. See [Topology-aware dynamic provisioning](#topology-aware-dynamic-provisioning) for more info
`--share-protocol-selector` | _none_ | Specifies which Manila share protocol to use for this instance of the driver. See [supported protocols](#share-protocol-support-matrix) for valid values.
`--fwdendpoint` | _none_ | [CSI Node Plugin](https://github.com/container-storage-interface/spec/blob/master/spec.md#rpc-interface) endpoint to which all Node Service RPCs are forwarded. Must be able to handle the file-system specified in `share-protocol-selector`. Check out the [Deployment](#deployment) section to see why this is necessary.
`--cluster-id` | _none_ | The identifier of the cluster that the plugin is running in. If set then the plugin will add "manila.csi.openstack.org/cluster: \<clusterID\>" to metadata of created shares.
`--provide-controller-service` | `true` | If set to true then the CSI driver does provide the controller service.
`--provide-node-service` | `true` | If set to true then the CSI driver does provide the node service.
`--pvc-annotations` | `false` | If set to true then the CSI driver will use PVC annotations as an additional information when creating shares. See [Supported PVC annotations](#supported-pvc-annotations) for more info.

### Controller Service volume parameters

_Kubernetes storage class parameters for dynamically provisioned volumes_

Parameter | Required | Description
----------|----------|------------
`type` | _yes_ | Manila [share type](https://wiki.openstack.org/wiki/Manila/Concepts#share_type)
`shareNetworkID` | _no_ | Manila [share network ID](https://wiki.openstack.org/wiki/Manila/Concepts#share_network)
`availability` | _no_ | Manila availability zone of the provisioned share. If none is provided, the default Manila zone will be used. Note that this parameter is opaque to the CO and does not influence placement of workloads that will consume this share, meaning they may be scheduled onto any node of the cluster. If the specified Manila AZ is not equally accessible from all compute nodes of the cluster, use [Topology-aware dynamic provisioning](#topology-aware-dynamic-provisioning).
`autoTopology` | _no_ | When set to "true" and the `availability` parameter is empty, the Manila CSI controller will map the Manila availability zone to the target compute node availability zone.
`groupID` | _no_ | The UUID of the share group to which the provisioned share belongs. If not empty, the share will be created in the specified share group. The share group must be created in advance before the PVC is created.
`appendShareMetadata` | _no_ | Append user-defined metadata to the provisioned share. If not empty, this field must be a string with a valid JSON object. The object must consist of key-value pairs of type string. Example: `"{..., \"key\": \"value\"}"`.
`cephfs-mounter` | _no_ | Relevant for CephFS Manila shares. Specifies which mounting method to use with the CSI CephFS driver. Available options are `kernel` and `fuse`, defaults to `fuse`. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.
`cephfs-kernelMountOptions` | _no_ | Relevant for CephFS Manila shares. Specifies mount options for CephFS kernel client. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.
`cephfs-fuseMountOptions` | _no_ | Relevant for CephFS Manila shares. Specifies mount options for CephFS FUSE client. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.
`cephfs-clientID` | _no_ | Relevant for CephFS Manila shares. Specifies the cephx client ID when creating an access rule for the provisioned share. The same cephx client ID may be shared with multiple Manila shares. If providing access to multiple cephx client IDs, set it as a comma separated list. If no value is provided, client ID for the provisioned Manila share will be set to some unique value (PersistentVolume name).
`nfs-shareClient` | _no_ | Relevant for NFS Manila shares. Specifies what address has access to the NFS share. Use a comma separated list for granting access to multiple IP addresses or subnets. Defaults to `0.0.0.0/0`, i.e. anyone.

### Node Service volume context

_Kubernetes PV CSI volume attributes for pre-provisioned volumes_

Parameter | Required | Description
----------|----------|------------
`shareID` | if `shareName` is not given | The UUID of the share
`shareName` | if `shareID` is not given | The name of the share
`shareAccessID` | _no_ | The UUID of the access rule for the share. This parameter is being deprecated and replaced by `shareAccessIDs`.
`shareAccessIDs` | _yes_ | Comma separated UUIDs of access rules for the share
`cephfs-mounter` | _no_ | Relevant for CephFS Manila shares. Specifies which mounting method to use with the CSI CephFS driver. Available options are `kernel` and `fuse`, defaults to `fuse`. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.
`cephfs-kernelMountOptions` | _no_ | Relevant for CephFS Manila shares. Specifies mount options for CephFS kernel client. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.
`cephfs-fuseMountOptions` | _no_ | Relevant for CephFS Manila shares. Specifies mount options for CephFS FUSE client. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.

_Note that the Node Plugin of CSI Manila doesn't care about the origin of a share. As long as the share protocol is supported, CSI Manila is able to consume dynamically provisioned as well as pre-provisioned shares (e.g. shares created manually)._

### Secrets, authentication

Authentication with OpenStack may be done using either user or trustee credentials.

Mandatory secrets: `os-authURL`, `os-region`.

Mandatory secrets for _user authentication:_ `os-password`, `os-userID` or `os-userName`, `os-domainID` or `os-domainName`, `os-projectID` or `os-projectName`.

Optional secrets for _user authentication:_ `os-projectDomainID` or `os-projectDomainName`, `os-userDomainID` or `os-userDomainName`

Mandatory secrets for _application credential authentication:_ `os-applicationCredentialID` or `os-applicationCredentialName` (when `os-userID` or `os-userName` and `os-domainName` are set), `os-applicationCredentialSecret`.

Mandatory secrets for _trustee authentication:_ `os-trustID`, `os-trusteeID`, `os-trusteePassword`.

Optionally, a custom certificate may be sourced via `os-certAuthorityPath` (path to a PEM file inside the plugin container). By default, the usual TLS verification is performed. To override this behavior and accept insecure certificates, set `os-TLSInsecure` to `true` (defaults to `false`).

For a client TLS authentication use both `os-clientCertPath` and `os-clientKeyPath` (paths to TLS keypair PEM files inside the plugin container).

### Topology-aware dynamic provisioning

Topology-aware dynamic provisioning makes it possible to reliably provision and use shares that are _not_ equally accessible from all compute nodes due to storage topology constraints.
With topology awareness enabled, administrators can specify the mapping between compute and Manila availability zones.
Doing so will instruct the CO scheduler to place the workloads+shares only on nodes that are able to reach the underlying storage.

CSI Manila uses `topology.manila.csi.openstack.org/zone` _topology key_ to identify node's affinity to a certain compute availability zone.
Each node of the cluster then gets labeled with the `topology.manila.csi.openstack.org/zone` where the value is the value of the AZ retrieved from the Nova metadata service.

This label may be used as a node selector when defining topology constraints for dynamic provisioning.
Administrators are also free to pass arbitrary labels, and as long as they are valid node selectors, they will be honored by the scheduler.

```
                          Topology-aware storage class example:


Storage AZ does not influence
 the placement of workloads.                                   Compute AZs do.

        +-----------+                                         +---------------+
        | Manila AZ |                                         |  Compute AZs  |
        |   zone-a  |    apiVersion: storage.k8s.io/v1        |     nova-1    |
        +-----------+    kind: StorageClass                   |     nova-2    |
              |          metadata:                            +---------------+
              |            name: cephfs-gold                          |
              |          provisioner: cephfs.manila.csi.openstack.org |
              |          parameters:                                  |
              +---------+  availability: zone-a                       |
                           ...                                        |
                         allowedTopologies:  +------------------------+
                           - matchLabelExpressions:
                             - key: topology.manila.csi.openstack.org/zone
                               values:
                                 - nova-1
                                 - nova-2


          Shares in zone-a are accessible only from nodes in nova-1 and nova-2.
```

In cases when the Manila availability zone must correspond to the Nova
availability zone, you can set the `autoTopology: "true"` along with the
`volumeBindingMode: WaitForFirstConsumer` and omit the `availability`
parameter. By doing so, the share will be provisioned in the target compute
node availability zone.

```
                          Auto topology-aware storage class example:


           Both Compute and Storage AZs influence the placement of workloads.

        +-----------+                                         +---------------+
        | Manila AZ |                                         |  Compute AZs  |
        |   zone-1  |    apiVersion: storage.k8s.io/v1        |     zone-1    |
        |   zone-2  |    kind: StorageClass                   |     zone-2    |
        +-----------+    metadata:                            +---------------+
              |            name: nfs-gold                             |
              |          provisioner: nfs.manila.csi.openstack.org    |
              |          parameters:                                  |
              +---------+  autoTopology: "true"  +--------------------+
                           ...
                         volumeBindingMode: WaitForFirstConsumer
                           ...

Shares for workloads in zone-1 will be created in zone-1 and accessible only from nodes in zone-1.
Shares for workloads in zone-2 will be created in zone-2 and accessible only from nodes in zone-2.
```

[Enabling topology awareness in Kubernetes](#enabling-topology-awareness)

### Runtime configuration file

CSI Manila's runtime configuration file is a JSON document for modifying behavior of the driver at runtime.

Schema:

* Root object:
  Attribute | Type | Description
  ----------|------|------------
  `nfs` | `NfsConfig` | Configuration for NFS shares. Optional.
* `NfsConfig`:
  Attribute | Type | Description
  ----------|------|------------
  `matchExportLocationAddress` | `string` | When mounting an NFS share, select an export location with matching IP address. No match between this address and at least a single export location for this share will result in an error. Expects a CIDR-formatted address. If prefix is not provided, /32 or /128 prefix is assumed for IPv4 and IPv6 respectively. Optional.

In Kubernetes, you may store this configuration in a [ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/) and expose it to CSI Manila pods as a [volume](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#add-configmap-data-to-a-volume). Then enter the path to the file populated by the ConfigMap into `--runtime-config-file`. Demo ConfigMap is located in `examples/manila-csi-plugin/runtimeconfig-cm.yaml`. If you're deploying CSI Manila with Helm, setting `csimanila.runtimeConfig.enabled` to `true` will take care of the setup.

## Deployment

The CSI Manila driver deals with the Manila service only. All node-related operations (attachments, mounts) are performed by a dedicated CSI Node Plugin, to which all Node Service RPCs are forwarded. This means that the operator is expected to already have a working deployment of that dedicated CSI Node Plugin.

A single instance of the driver may serve only a single Manila share protocol. To serve multiple share protocols, multiple deployments of the driver need to be made. In order to avoid deployment collisions, each instance of the driver should be named differently, e.g. `csi-manila-cephfs`, `csi-manila-nfs`.

### Kubernetes 1.17+

The deployment consists of two main components: Controller and Node plugins along with their respective RBACs. Controller plugin is deployed as a StatefulSet and runs CSI Manila, [external-provisioner](https://github.com/kubernetes-csi/external-provisioner) and [external-snapshotter](https://github.com/kubernetes-csi/external-snapshotter). Node plugin is deployed as a DaemonSet and runs CSI Manila and [csi-node-driver-registrar](https://github.com/kubernetes-csi/node-driver-registrar).

Note: Snapshotting feature now requires a separate snapshot controller and CRDs to be deployed in the cluster. Please follow the related official [installation instructions](https://github.com/kubernetes-csi/external-snapshotter/blob/master/README.md#usage) for [external-snapshotter](https://github.com/kubernetes-csi/external-snapshotter) before continuing.

**Deploying with Helm**

This is the preferred way of deployment because it greatly simplifies the difficulties with managing multiple share protocols.

CSI Manila Helm chart is located in `charts/manila-csi-plugin`.

First, modify `values.yaml` to suite your environment, and then simply install the Helm chart with `$ helm install ./charts/manila-csi-plugin`.

Note that the release name generated by `helm install` may not be suitable due to their length. The chart generates object names with the release name included in them, which may cause the names to exceed 63 characters and result in chart installation failure. You may use `--name` flag to set the release name manually. See [helm installation docs](https://helm.sh/docs/helm/#helm-install) for more info. Alternatively, you may also use `nameOverride` or `fullnameOverride` variables in `values.yaml` to override the respective names.

**Manual deployment**

All Kubernetes YAML manifests are located in `manifests/manila-csi-plugin`.

First, deploy the RBACs:
```
kubectl create -f csi-controllerplugin-rbac.yaml
kubectl create -f csi-nodeplugin-rbac.yaml
```

Deploy the CSIDriver CRD:
```
kubectl create -f csidriver.yaml
```

Before continuing, `csi-controllerplugin.yaml` and `csi-nodeplugin.yaml` manifests need to be modified: `fwd-plugin-dir` needs to point to the correct path containing the `.sock` file of the other CSI driver.
Next, `MANILA_SHARE_PROTO` and `FWD_CSI_ENDPOINT` environment variables must be set. Consult `--share-protocol-selector` and `--fwdendpoint` [command line flags](#command-line-arguments) for valid values.

Finally, deploy Controller and Node plugins:
```
kubectl create -f csi-controllerplugin.yaml
kubectl create -f csi-nodeplugin.yaml
```

#### Verifying the deployment

Successful deployment should look similar to this:

```
$ kubectl get all
NAME                                          READY   STATUS    RESTARTS   AGE
pod/openstack-manila-csi-controllerplugin-0   3/3     Running   0          2m8s
pod/openstack-manila-csi-nodeplugin-ln2xk     2/2     Running   0          2m2s

NAME                                            TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)     AGE
service/openstack-manila-csi-controllerplugin   ClusterIP   10.102.188.171   <none>        12345/TCP   2m8s

NAME                                             DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR   AGE
daemonset.apps/openstack-manila-csi-nodeplugin   1         1         1       1            1           <none>          2m2s

NAME                                                     READY   AGE
statefulset.apps/openstack-manila-csi-controllerplugin   1/1     2m8s

...
```

To test the deployment further, see `examples/csi-manila-plugin`.

#### Enabling topology awareness

If you're deploying CSI Manila with Helm:
1. Set `csimanila.topologyAwarenessEnabled` to `true`

If you're deploying CSI Manila manually:
1. Run the [external-provisioner](https://github.com/kubernetes-csi/external-provisioner) with `--feature-gates=Topology=true` cmd flag.
2. Run CSI Manila with [`--with-topology`](#command-line-arguments).

See `examples/csi-manila-plugin/nfs/topology-aware` for examples on defining topology constraints.

## Share protocol support matrix

The table below shows Manila share protocols currently supported by CSI Manila and their corresponding CSI Node Plugins which must be deployed alongside CSI Manila.

Manila share protocol | CSI Node Plugin
----------------------|----------------
`CEPHFS` | [CSI CephFS](https://github.com/ceph/ceph-csi) : v1.0.0
`NFS` | [CSI NFS](https://github.com/kubernetes-csi/csi-driver-nfs) : v1.0.0

## Supported PVC Annotations

The PVC annotations support must be enabled in the Manila CSI controller with
the `--pvc-annotations` flag. The PVC annotations take effect only when the PVC
is created. The scheduler hints are not updated when the PVC is updated. The
minimum Manila API microversion required for scheduler hints is 2.65. Make sure
that the Manila API microversion is supported by the Manila backend. The
following PVC annotations are supported:

| Annotation Name            | Description      | Example |
|-------------------------   |-----------------|----------|
| `manila.csi.openstack.org/affinity` | Share affinity to existing share or shares names/UUIDs. The value should be a comma-separated list of share names or UUIDs. | `manila.csi.openstack.org/affinity: "1b4e28ba-2fa1-11ec-8d3d-0242ac130003"` |
| `manila.csi.openstack.org/anti-affinity` | Share anti-affinity to existing share or shares names/UUIDs. The value should be a comma-separated list of share names/UUIDs. | `manila.csi.openstack.org/anti-affinity: "1b4e28ba-2fa1-11ec-8d3d-0242ac130004,pv-default-50c5a3b3-e0b5-48d6-a163-4e68956aeb54"` |
| `manila.csi.openstack.org/group-id` | The UUID of the share group to which the provisioned share must belong. The share group must be created before the PVC is created. | `manila.csi.openstack.org/group-id: "1b4e28ba-2fa1-11ec-8d3d-0242ac130006"` |

If the PVC annotation is set, the share will be created according to the
existing share names/UUIDs placements, i.e. on the same host as the
`1b4e28ba-2fa1-11ec-8d3d-0242ac130003` share and not on the same host as the
`1b4e28ba-2fa1-11ec-8d3d-0242ac130004` and
`pv-default-50c5a3b3-e0b5-48d6-a163-4e68956aeb54` shares.

The `manila.csi.openstack.org/group-id` annotation value overrides the storage
class `groupID` parameter if both are set.

## For developers

If you'd like to contribute to CSI Manila, check out `docs/manila-csi-plugin/developers-csi-manila.md` to get you started.
