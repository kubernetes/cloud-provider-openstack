# CSI Manila driver

The CSI Manila driver is able to create and mount OpenStack Manila shares. Snapshots and recovering shares from snapshots is supported as well (support for CephFS snapshots will be added soon).

###### Table of contents

* [Configuration](#configuration)
  * [Command line arguments](#command-line-arguments)
  * [Controller Service volume parameters](#controller-service-volume-parameters)
  * [Node Service volume context](#node-service-volume-context)
  * [Secrets, authentication](#secrets-authentication)
* [Deployment](#deployment)
  * [Kubernetes 1.15+](#kubernetes-115)
    * [Verifying the deployment](#verifying-the-deployment)
* [Share protocol support matrix](#share-protocol-support-matrix)
* [For developers](#for-developers)

## Configuration

### Command line arguments

Option | Default value | Description
-------|---------------|------------
`--endpoint` | `unix:///tmp/csi.sock` | CSI Manila's CSI endpoint
`--drivername` | `manila.csi.openstack.org` | Name of this driver
`--nodeid` | _none_ | ID of this node
`--share-protocol-selector` | _none_ | Specifies which Manila share protocol to use for this instance of the driver. See [supported protocols](#share-protocol-support-matrix) for valid values.
`--fwdendpoint` | _none_ | [CSI Node Plugin](https://github.com/container-storage-interface/spec/blob/master/spec.md#rpc-interface) endpoint to which all Node Service RPCs are forwarded. Must be able to handle the file-system specified in `share-protocol-selector`. Check out the [Deployment](#deployment) section to see why this is necessary.

### Controller Service volume parameters

Parameter | Required | Description
----------|----------|------------
`type` | _yes_ | Manila [share type](https://wiki.openstack.org/wiki/Manila/Concepts#share_type)
`shareNetworkID` | _no_ | Manila [share network ID](https://wiki.openstack.org/wiki/Manila/Concepts#share_network)
`cephfs-mounter` | _no_ | Relevant for CephFS Manila shares. Specifies which mounting method to use with the CSI CephFS driver. Available options are `kernel` and `fuse`, defaults to `fuse`. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.
`nfs-shareClient` | _no_ | Relevant for NFS Manila shares. Specifies what address has access to the NFS share. Defaults to `0.0.0.0/0`, i.e. anyone. 

### Node Service volume context

Parameter | Required | Description
----------|----------|------------
`shareID` | if `shareName` is not given | The UUID of the share
`shareName` | if `shareID` is not given | The name of the share
`shareAccessID` | _yes_ | The UUID of the access rule for the share
`cephfs-mounter` | _no_ | Relevant for CephFS Manila shares. Specifies which mounting method to use with the CSI CephFS driver. Available options are `kernel` and `fuse`, defaults to `fuse`. See [CSI CephFS docs](https://github.com/ceph/ceph-csi/blob/csi-v1.0/docs/deploy-cephfs.md#configuration) for further information.

_Note that the Node Plugin of CSI Manila doesn't care about the origin of a share. As long as the share protocol is supported, CSI Manila is able to consume dynamically provisioned as well as pre-provisioned shares (e.g. shares created manually)._

### Secrets, authentication

Authentication with OpenStack may be done using either user or trustee credentials.

Mandatory secrets: `os-authURL`, `os-region`.

Mandatory secrets for _user authentication:_ `os-password`, `os-userID` or `os-userName`, `os-domainID` or `os-domainName`, `os-projectID` or `os-projectName`.

Mandatory secrets for _trustee authentication:_ `os-trustID`, `os-trusteeID`, `os-trusteePassword`.

Optionally, a custom certificate may be sourced via `os-certAuthorityPath` (path to a PEM file inside the plugin container). By default, the usual TLS verification is performed. To override this behavior and accept insecure certificates, set `os-TLSInsecure` to `true` (defaults to `false`).

## Deployment

The CSI Manila driver deals with the Manila service only. All node-related operations (attachments, mounts) are performed by a dedicated CSI Node Plugin, to which all Node Service RPCs are forwarded. This means that the operator is expected to already have a working deployment of that dedicated CSI Node Plugin.

A single instance of the driver may serve only a single Manila share protocol. To serve multiple share protocols, multiple deployments of the driver need to be made. In order to avoid deployment collisions, each instance of the driver should be named differently, e.g. `csi-manila-cephfs`, `csi-manila-nfs`.

### Kubernetes 1.15+

Snapshots require `VolumeSnapshotDataSource=true` feature gate.

The deployment consists of two main components: Controller and Node plugins along with their respective RBACs. Controller plugin is deployed as a StatefulSet and runs CSI Manila, [external-provisioner](https://github.com/kubernetes-csi/external-provisioner) and [external-snapshotter](https://github.com/kubernetes-csi/external-snapshotter). Node plugin is deployed as a DaemonSet and runs CSI Manila and [csi-node-driver-registrar](https://github.com/kubernetes-csi/node-driver-registrar).

**Deploying with Helm**

This is the preferred way of deployment because it greatly simplifies the difficulties with managing multiple share protocols.

CSI Manila Helm chart is located in `examples/manila-csi-plugin/helm-deployment`.

First, modify `values.yaml` to suite your environment, and then simply install the Helm chart with `$ helm install helm-deployment`.

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

## Share protocol support matrix

The table below shows Manila share protocols currently supported by CSI Manila and their corresponding CSI Node Plugins which must be deployed alongside CSI Manila.

Manila share protocol | CSI Node Plugin
----------------------|----------------
`CEPHFS` | [CSI CephFS](https://github.com/ceph/ceph-csi) : v1.0.0
`NFS` | [CSI NFS](https://github.com/kubernetes-csi/csi-driver-nfs) : v1.0.0

## For developers

If you'd like to contribute to CSI Manila, check out `docs/developers-csi-manila.md` to get you started.
