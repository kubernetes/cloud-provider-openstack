<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [CSI Cinder driver](#csi-cinder-driver)
  - [Kubernetes](#kubernetes)
    - [Compatibility](#compatibility)
    - [Requirements](#requirements)
    - [Example local-up-cluster.sh](#example-local-up-clustersh)
    - [Deploy](#deploy)
      - [Using the Helm chart](#using-the-helm-chart)
      - [Using the manifests](#using-the-manifests)
    - [Example Nginx application usage](#example-nginx-application-usage)
    - [Enable Topology-aware dynamic provisioning for Cinder Volumes](#enable-topology-aware-dynamic-provisioning-for-cinder-volumes)
    - [Example Snapshot Create and Restore](#example-snapshot-create-and-restore)
    - [Example: Raw Block Volume](#example-raw-block-volume)
    - [Volume Expansion](#volume-expansion)
      - [Rescan block device geometry on in-use volume resize](#rescan-block-device-geometry-on-in-use-volume-resize)
    - [Inline Volumes](#inline-volumes)
    - [Volume Cloning](#volume-cloning)
  - [Running Sanity Tests](#running-sanity-tests)
  - [Using CSC tool](#using-csc-tool)
    - [Test using csc](#test-using-csc)
    - [Start Cinder driver](#start-cinder-driver)
    - [Get plugin info](#get-plugin-info)
    - [Get supported capabilities](#get-supported-capabilities)
    - [Get controller implemented capabilities](#get-controller-implemented-capabilities)
    - [Create a volume](#create-a-volume)
    - [List volumes](#list-volumes)
    - [Delete a volume](#delete-a-volume)
    - [Create a snapshot from volume](#create-a-snapshot-from-volume)
    - [List snapshots](#list-snapshots)
    - [Delete a snapshot](#delete-a-snapshot)
    - [ControllerPublish a volume](#controllerpublish-a-volume)
    - [ControllerUnpublish a volume](#controllerunpublish-a-volume)
    - [NodePublish a volume](#nodepublish-a-volume)
    - [NodeUnpublish a volume](#nodeunpublish-a-volume)
    - [Get NodeID](#get-nodeid)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# CSI Cinder driver

## Kubernetes

### Compatibility

CSI version | Cinder CSI Plugin Version | Kubernetes Version
:------     | :------------             | :-----------
v1.1.0 |  v1.0.0, v1.1.0  docker image: k8scloudprovider/cinder-csi-plugin:latest | v1.15+
v1.0.0 |  v1.0.0  docker image: k8scloudprovider/cinder-csi-plugin:v1.14 | v1.13, v1.14
v0.3.0 |  v0.3.0 docker image: k8scloudprovider/cinder-csi-plugin:1.13.x| v1.11, v1.12, v1.13
v0.2.0 |  v0.2.0 docker image: k8scloudprovider/cinder-csi-plugin:v0.2.0 | v1.10, v1.9
v0.1.0 |  v0.1.0 docker image: k8scloudprovider/cinder-csi-plugin:v0.1.0| v1.9

For sidecar version compatibility , please refer compatibility matrix for each sidecar here -
https://kubernetes-csi.github.io/docs/sidecar-containers.html

### Requirements

```
RUNTIME_CONFIG="storage.k8s.io/v1=true"
```
MountPropagation requires support for privileged containers. So, make sure privileged containers are enabled in the cluster.

Check [kubernetes CSI Docs](https://kubernetes-csi.github.io/docs/) for flag details and latest update.

> NOTE: All following examples need to be used inside instance(s) provisoned by openstack, otherwise the attach action will fail due to fail to find instance ID from given openstack cloud.

### Example local-up-cluster.sh

```ALLOW_PRIVILEGED=true RUNTIME_CONFIG="storage.k8s.io/v1=true" LOG_LEVEL=5 hack/local-up-cluster.sh```

### Deploy

You can either use the manifests under `manifests/cinder-csi-plugin` or the Helm chart `charts/cinder-csi-plugin`.

#### Using the Helm chart

> NOTE: This chart assumes that the `cloud-config` is found on the host under `/etc/kubernetes/` and that your OpenStack cloud has cert under `/etc/cacert`.

To install the chart use the following command:
```
helm install --namespace kube-system --name cinder-csi ./charts/cinder-csi-plugin
```

#### Using the manifests

If you already created the `cloud-config` secret used by the [cloud-controller-manager](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-openstack-cloud-controller-manager.md#steps), remove the file ```manifests/cinder-csi-plugin/csi-secret-cinderplugin.yaml``` from [manifests](https://github.com/kubernetes/cloud-provider-openstack/tree/master/manifests/cinder-csi-plugin) and then jump directly to the `kubectl apply ...` command.

Encode your ```$CLOUD_CONFIG``` file content using base64.

```base64 -w 0 $CLOUD_CONFIG```

Update ```cloud.conf``` configuration in ```manifests/cinder-csi-plugin/csi-secret-cinderplugin.yaml``` file
by using the result of the above command.

> NOTE: In OpenStack, the compute instance uses either config drive or metadata service to retrieve instance-specific data. As the cluster administrator, you are able to config the order in the cloud config file for cinder-csi-plugin, the default configuration is as follows:
```
[Metadata]
search-order = configDrive,metadataService
```

> NOTE: if your openstack cloud has cert (which means you already has [ca-file](provider-configuration.md#global-optional-parameters) definition in cloud-config), please make sure that you also updated the volumes list of `cinder-csi-controllerplugin.yaml` and `cinder-csi-nodeplugin.yaml` to include the cacert. e.g following sample then mount the volume to the pod as well.

```
volumes:
...
- name: cacert
  hostPath:
    path: /etc/cacert
    type: Directory
```

Then call following command by using existing [manifests](https://github.com/kubernetes/cloud-provider-openstack/tree/master/manifests/cinder-csi-plugin):

```kubectl -f manifests/cinder-csi-plugin apply```

This creates a set of cluster roles, cluster role bindings, and statefulsets etc to communicate with openstack(cinder).
For detailed list of created objects, explore the yaml files in the directory.
You should make sure following similar pods are ready before proceed:

```
NAME                                READY   STATUS    RESTARTS   AGE
csi-cinder-controllerplugin         5/5     Running   0          29h
csi-cinder-nodeplugin               2/2     Running   0          46h
```

you can get information about CSI Drivers running in a cluster, using **CSIDriver** object

```
$ kubectl get csidrivers.storage.k8s.io
NAME                       CREATED AT
cinder.csi.openstack.org   2019-07-29T09:02:40Z

$ kubectl describe csidrivers.storage.k8s.io
Name:         cinder.csi.openstack.org
Namespace:    
Labels:       <none>
Annotations:  <none>
API Version:  storage.k8s.io/v1beta1
Kind:         CSIDriver
Metadata:
  Creation Timestamp:  2019-07-29T09:02:40Z
  Resource Version:    1891
  Self Link:           /apis/storage.k8s.io/v1beta1/csidrivers/cinder.csi.openstack.org
  UID:                 2bd1f3bf-3c41-46a8-b99b-5773cb5eacd3
Spec:
  Attach Required:    true
  Pod Info On Mount:  false
Events:               <none>
```

### Example Nginx application usage

After performing above steps, you can try to create StorageClass, PersistentVolumeClaim and pod to consume it.
Try following command by using [examples](https://github.com/kubernetes/cloud-provider-openstack/blob/master/examples/cinder-csi-plugin/nginx.yaml):

```kubectl -f examples/cinder-csi-plugin/nginx.yaml create```

You will get pvc which claims one volume from cinder

```
$ kubectl get pvc
NAME                   STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS          AGE
csi-pvc-cinderplugin   Bound    pvc-72a8f9c9-f462-11e8-b6b6-fa163e18b7b5   1Gi        RWO            csi-sc-cinderplugin   58m

$ openstack volume list
+--------------------------------------+--------+------------------------------------------+------+-------------+----------+--------------------------------------+
|                  ID                  | Status |                   Name                   | Size | Volume Type | Bootable |             Attached to              |
+--------------------------------------+--------+------------------------------------------+------+-------------+----------+--------------------------------------+
| b2e7be02-cdd7-487f-8eb9-6f10f3df790b | in-use | pvc-72a8f9c9-f462-11e8-b6b6-fa163e18b7b5 |  1   | lvmdriver-1 |  false   | 39859899-2985-43cf-8cdf-21de2548dfd9 |
+--------------------------------------+--------+------------------------------------------+------+-------------+----------+--------------------------------------+
```

Check the volume created and attached to the pod
```
$ ls /dev/vdb
/dev/vdb

$ mount | grep vdb
/dev/vdb on /var/lib/kubelet/pods/8196212e-f462-11e8-b6b6-fa163e18b7b5/volumes/kubernetes.io~csi/pvc-72a8f9c9-f462-11e8-b6b6-fa163e18b7b5/mount type ext4 (rw,relatime,data=ordered)

$ fdisk -l /dev/vdb | grep Disk
Disk /dev/vdb: 1 GiB, 1073741824 bytes, 2097152 sectors
```

Then try to add a file in the pod's mounted position (in our case, /var/lib/www/html)
```
$ kubectl exec -it nginx bash

root@nginx:/# ls /var/lib/www/html
lost+found

root@nginx:/# touch /var/lib/www/html/index.html

root@nginx:/# exit
exit
```

Next, make sure the pod is deleted so that the persistent volume will be freed
```
kubectl delete pod nginx
```

Then the volume is back to available state:
```
$ ls /dev/vdb
ls: cannot access '/dev/vdb': No such file or directory

$ openstack volume list
+--------------------------------------+-----------+------------------------------------------+------+-------------+----------+-------------+
|                  ID                  |   Status  |                   Name                   | Size | Volume Type | Bootable | Attached to |
+--------------------------------------+-----------+------------------------------------------+------+-------------+----------+-------------+
| b2e7be02-cdd7-487f-8eb9-6f10f3df790b | available | pvc-72a8f9c9-f462-11e8-b6b6-fa163e18b7b5 |  1   | lvmdriver-1 |  false   |             |
+--------------------------------------+-----------+------------------------------------------+------+-------------+----------+-------------+
```

Optionally you can verify the volume does contain the info we created in pod by attaching to a VM in openstack:
```
$ nova volume-attach ji1 b2e7be02-cdd7-487f-8eb9-6f10f3df790b
+----------+--------------------------------------+
| Property | Value                                |
+----------+--------------------------------------+
| device   | /dev/vdb                             |
| id       | b2e7be02-cdd7-487f-8eb9-6f10f3df790b |
| serverId | 39859899-2985-43cf-8cdf-21de2548dfd9 |
| volumeId | b2e7be02-cdd7-487f-8eb9-6f10f3df790b |
+----------+--------------------------------------+

$ ls /dev/vdb
/dev/vdb
$ mount /dev/vdb /mnt; ls /mnt
index.html  lost+found
```

### Enable Topology-aware dynamic provisioning for Cinder Volumes

Following feature gates needs to be enabled as below:
1. `--feature-gates=CSINodeInfo=true,CSIDriverRegistry=true` in the manifest entries of kubelet and kube-apiserver. (Enabled by default in kubernetes v1.14)
2. `--feature-gates=Topology=true` needs to be enabled in external-provisioner.

Currently, driver supports only one topology key: `topology.cinder.csi.openstack.org/zone` that represents availability by zone.

Note: `allowedTopologies` can be specified in storage class to restrict the topology of provisioned volumes to specific zones and should be used as replacement of `availability` parameter.

### Example Snapshot Create and Restore

Following prerequisite needed for volume snapshot feature to work.

1. Download all yaml files from [snapshot crd](https://github.com/kubernetes-csi/external-snapshotter/tree/v2.1.1/config/crd)
2. Apply all yaml files downloaded at step 1) by using `kubectl apply -f ` command, as currently there is no released yaml file from `external-snapshotter` community.
3. Download all yaml files from [snapshot-controller](https://github.com/kubernetes-csi/external-snapshotter/tree/master/deploy/kubernetes/snapshot-controller) and optionally update all namespaces from `default` to `kube-system` in the yaml files.
4. Apply all yaml files downloaded at step 3) by using `kubectl apply -f ` command.

Supported parameters for VolumeSnapshotClass:
* `force-create`: Support creating snapshot for a volume in in-use status.

For Snapshot Creation and Volume Restore, please follow below steps:
* Create Storage Class, Snapshot Class and PVC
```
$ kubectl -f examples/cinder-csi-plugin/snapshot/example.yaml create
storageclass.storage.k8s.io/csi-sc-cinderplugin created
volumesnapshotclass.snapshot.storage.k8s.io/csi-cinder-snapclass created
persistentvolumeclaim/pvc-snapshot-demo created
```

* Verify that pvc is bounded
```
$ kubectl get pvc --all-namespaces
NAMESPACE   NAME                STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS          AGE
default     pvc-snapshot-demo   Bound    pvc-4699fa78-4149-4772-b900-9536891fe200   1Gi        RWO 
```
* Create Snapshot of the PVC
```
$ kubectl -f examples/cinder-csi-plugin/snapshot/snapshotcreate.yaml create
volumesnapshot.snapshot.storage.k8s.io/new-snapshot-demo created
```
* Verify that snapshot is created
```
$ kubectl get volumesnapshot
NAME                AGE
new-snapshot-demo   2m54s
$ openstack snapshot list
+--------------------------------------+-----------------------------------------------+-------------+-----------+------+
| ID                                   | Name                                          | Description | Status    | Size |
+--------------------------------------+-----------------------------------------------+-------------+-----------+------+
| 1b673af2-3a69-4cc6-8dd0-9ac62e29df9e | snapshot-332a8a7e-c5f2-4df9-b6a0-cf52e18e72b1 | None        | available |    1 |
+--------------------------------------+-----------------------------------------------+-------------+-----------+------+
```
* Restore volume from snapshot
```
$ kubectl -f examples/cinder-csi-plugin/snapshot/snapshotrestore.yaml create
persistentvolumeclaim/snapshot-demo-restore created
```
* Verify that volume from snapshot is created
```
$ kubectl get pvc
NAME                    STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS          AGE
pvc-snapshot-demo       Bound    pvc-4699fa78-4149-4772-b900-9536891fe200   1Gi        RWO            csi-sc-cinderplugin   4m5s
snapshot-demo-restore   Bound    pvc-400b1ca8-8786-435f-a6cc-f684afddbbea   1Gi        RWO            csi-sc-cinderplugin   8s

$ openstack volume list
+--------------------------------------+------------------------------------------+-----------+------+-------------------------------------------------+
| ID                                   | Display Name                             | Status    | Size | Attached to                                     |
+--------------------------------------+------------------------------------------+-----------+------+-------------------------------------------------+
| 07522a3b-95db-4bfa-847c-ffa179d08c39 | pvc-400b1ca8-8786-435f-a6cc-f684afddbbea | available |    1 |                                                 |
| bf8f9ae9-87b4-42bb-b74c-ba4645634be6 | pvc-4699fa78-4149-4772-b900-9536891fe200 | available |    1 |                                                 |
```
### Example: Raw Block Volume

For consuming a cinder volume as a raw block device

1. Make sure the volumeMode is `Block` in Persistence Volume Claim Spec
2. Make sure the pod is consuming the PVC with the defined name and `volumeDevices` is used instead of `volumeMounts`
3. Deploy the Application

Example :

```
$ kubectl create -f examples/cinder-csi-plugin/block/block.yaml
```

Make sure Pod is running
```
$ kubectl get pods
```

Verify the device node is mounted inside the container

```
$ kubectl exec -ti test-block -- ls -al /dev/xvda
brw-rw----    1 root     disk      202, 23296 Mar 12 04:23 /dev/xvda
```

### Volume Expansion

1. As of kubernetes v1.16, Volume Expansion is a beta feature and enabled by default.
2. Make sure to set `allowVolumeExpansion` to `true` in Storage class spec.
3. Deploy the application.

Example:

```
$ kubectl create -f examples/cinder-csi-plugin/resize/example.yaml
```

Verify PV is created and bound to PVC

```
$ kubectl get pvc
NAME                     STATUS    VOLUME                                     CAPACITY  ACCESS MODES   STORAGECLASS   AGE
csi-pvc-cinderplugin     Bound     pvc-e36abf50-84f3-11e8-8538-42010a800002   1Gi       RWO            csi-sc-cinderplugin     9s
```
Make sure Pod is running
```
$ kubectl get pods
NAME                 READY     STATUS    RESTARTS   AGE
nginx                1/1       Running   0          1m
```

Check current filesystem size on the running pod
```
$ kubectl exec nginx -- df -h /var/lib/www/html
Filesystem      Size  Used Avail Use% Mounted on
/dev/vdb        1.0G   24M  0.98G   1% /var/lib/www/html
```
Resize volume by modifying the field `spec -> resources -> requests -> storage`
```
$ kubectl edit pvc csi-pvc-cinderplugin
apiVersion: v1
kind: PersistentVolumeClaim
...
spec:
  resources:
    requests:
      storage: 2Gi
  ...
...
```

Verify filesystem resized on the running pod
```
$ kubectl exec nginx -- df -h /var/lib/www/html
Filesystem      Size  Used Avail Use% Mounted on
/dev/vdb        2.0G   27M  1.97G   1% /var/lib/www/html
```

#### Rescan block device geometry on in-use volume resize

Some hypervizors (like VMware) don't automatically send a new volume size to a Linux kernel, when a volume is in-use. Sending a "1" to `/sys/class/block/XXX/device/rescan` is telling the SCSI block device to refresh it's information about where it's ending boundary is (among other things) to give the kernel information about it's updated size. When a `rescan-on-resize` flag is set in a CSI node driver cloud-config `[BlockStorage]` section, a CSI node driver will rescan block device and verify its size before expanding the filesystem. CSI driver will raise an error, when expected volume size cannot be detected.

Not all hypervizors have a `/sys/class/block/XXX/device/rescan` location, therefore if you enable this option and your hypervizor doesn't support this, you'll get a warning log on resize event. It is recommended to disable this option in this case.

### Inline Volumes

This feature allows CSI volumes to be directly embedded in the Pod specification instead of a PersistentVolume. Volumes specified in this way are ephemeral and do not persist across Pod restarts. As of Kubernetes v1.16 this feature is beta so enabled by default. To enable this feature for CSI Driver, `volumeLifecycleModes` needs to be specified in [CSIDriver](https://github.com/kubernetes/cloud-provider-openstack/blob/master/manifests/cinder-csi-plugin/csi-cinder-driver.yaml) object. The driver can run in `Persistent` mode, `Ephemeral` or in both modes. `podInfoOnMount` must be `true` to use this feature.

Example:
1. Deploy CSI Driver, in default yamls both modes are enabled.
```
$ kubectl create -f manifests/cinder-csi-plugin/
```
2. Create a pod with inline volume
```
$ kubectl create -f examples/cinder-csi-plugin/inline/inline-example.yaml
```
3. Get the pod description, verify created volume in Volumes section.
```
$ kubectl describe pod

Volumes:
  my-csi-volume:
    Type:              CSI (a Container Storage Interface (CSI) volume source)
    Driver:            cinder.csi.openstack.org
    FSType:            ext4
    ReadOnly:          false
    VolumeAttributes:      capacity=1Gi
  default-token-dn78p:
    Type:        Secret (a volume populated by a Secret)
    SecretName:  default-token-dn78p
    Optional:    false

```

### Volume Cloning

As of Kubernetes v1.16, volume cloning is beta feature and enabled by default.
This feature enables support of cloning a volume from existing PVCs.

Following prerequisites needed for volume cloning to work :
1. The source PVC must be bound and available (not in use).
2. source and destination PVCs must be in the same namespace.
3. Cloning is only supported within the same Storage Class. Destination volume must
   be the same storage class as the source

Sample yamls can be found [here](https://github.com/kubernetes/cloud-provider-openstack/tree/master/examples/cinder-csi-plugin/clone)

### Multi-Attach Volumes

To avail the multiattach feature of cinder, specify the ID/name of cinder volume type that includes an extra-spec capability setting of `multiattach=<is> True` in storage class parameters as shown below.

> Note: This volume type must exist in cinder already (`openstack volume type list`)

This should enable to attach a volume to multiple hosts/servers simultaneously.

Example:

```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: csi-sc-cinderplugin
provisioner: cinder.csi.openstack.org
parameters:
  type: <multiattach-volume-type>

## Running Sanity Tests

Sanity tests create a real instance of driver and fake cloud provider.
see [Sanity check](https://github.com/kubernetes-csi/csi-test/tree/master/pkg/sanity) for more info.
```
$ make test-cinder-csi-sanity
```

## Using CSC tool

### Test using csc
Get ```csc``` tool from https://github.com/thecodeteam/gocsi/tree/master/csc

### Start Cinder driver

First, you need to start the plugin as daemon to accept request from csc.
Following example is starting listening at localhost port 10000 with cloud configuration
given in /etc/cloud.conf and the node id is CSINodeID. ClusterID is the identifier of
the cluster in which the plugin is running.

```
$ sudo cinder-csi-plugin --endpoint tcp://127.0.0.1:10000 --cloud-config /etc/cloud.conf --nodeid CSINodeID --cluster ClusterID
```

### Get plugin info
```
$ csc identity plugin-info --endpoint tcp://127.0.0.1:10000
"cinder.csi.openstack.org"      "1.0.0"
```

### Get supported capabilities
```
$ csc identity plugin-capabilities --endpoint tcp://127.0.0.1:10000
CONTROLLER_SERVICE
VOLUME_ACCESSIBILITY_CONSTRAINTS
```

### Get controller implemented capabilities
```
$ csc controller get-capabilities  --endpoint tcp://127.0.0.1:10000
&{type:LIST_VOLUMES }
&{type:CREATE_DELETE_VOLUME }
&{type:PUBLISH_UNPUBLISH_VOLUME }
&{type:CREATE_DELETE_SNAPSHOT }
&{type:LIST_SNAPSHOTS }
```

### Create a volume
Following sample creates a volume named ``CSIVolumeName`` and the
volume id returned is ``8a55f98f-e987-43ab-a9f5-973352bee19c`` with size ``1073741824`` bytes (1Gb)
```
$ csc controller create-volume --endpoint tcp://127.0.0.1:10000 CSIVolumeName
"8a55f98f-e987-43ab-a9f5-973352bee19c"  1073741824      "availability"="nova"
```

### List volumes
Following sample list all volumes:
```
$ csc controller list-volumes --endpoint tcp://127.0.0.1:10000
"8a55f98f-e987-43ab-a9f5-973352bee19c"  1073741824
```

### Delete a volume
Following sample deletes a volume ``01217e93-bd1b-4760-b5d8-18b8b3d47f91``
```
$ csc controller delete-volume --endpoint tcp://127.0.0.1:10000 01217e93-bd1b-4760-b5d8-18b8b3d47f91
01217e93-bd1b-4760-b5d8-18b8b3d47f91
```

### Create a snapshot from volume
Following sample creates a snapshot from volume `40615da4-3fda-4e78-bf58-820692536e68`.
After execution, snapshot `e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f` will be created.
```
$ csc controller create-snapshot --source-volume 40615da4-3fda-4e78-bf58-820692536e68 --endpoint tcp://127.0.0.1:10000 s1
"e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f"  1073741824      40615da4-3fda-4e78-bf58-820692536e68    seconds:1561530261      true
```

Use openstack command to verify:
```
openstack volume snapshot list
+--------------------------------------+------+-------------+-----------+------+
| ID                                   | Name | Description | Status    | Size |
+--------------------------------------+------+-------------+-----------+------+
| e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f | s1   | None        | available |    1 |
+--------------------------------------+------+-------------+-----------+------+
```

### List snapshots

Following sample lists all snapshots:
```
$ csc controller  list-snapshots --endpoint tcp://127.0.0.1:10000
"e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f" 1073741824      40615da4-3fda-4e78-bf58-820692536e68    seconds:1561532425      true
```

### Delete a snapshot

Following sample deletes the snapshot `e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f`.
```
$ csc controller delete-snapshot e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f --endpoint tcp://127.0.0.1:10000
e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f
```

Use openstack command to verify:
```
$ openstack volume snapshot list

$
```

### ControllerPublish a volume
The action has similar result to ``nova volume-attach`` command:

Assume we have following result in openstack now:
```
$ openstack server list
+--------------------------------------+-------+--------+--------------------------------+--------+---------+
| ID                                   | Name  | Status | Networks                       | Image  | Flavor  |
+--------------------------------------+-------+--------+--------------------------------+--------+---------+
| 17e540e6-8d08-4a5a-8835-668bc8fe913c | demo1 | ACTIVE | demo-net=10.0.0.13             | cirros | m1.tiny |
+--------------------------------------+-------+--------+--------------------------------+--------+---------+

$ openstack volume list
+--------------------------------------+-----------------------------------+-----------+------+-------------+
| ID                                   | Name                              | Status    | Size | Attached to |
+--------------------------------------+-----------------------------------+-----------+------+-------------+
| ed893ce1-807d-4c6e-a558-88c61b439659 | v1                                | available |    1 |             |
+--------------------------------------+-----------------------------------+-----------+------+-------------+
```

Then execute:

```
# csc controller publish --endpoint tcp://127.0.0.1:10000 --node-id=17e540e6-8d08-4a5a-8835-668bc8fe913c ed893ce1-807d-4c6e-a558-88c61b439659
"ed893ce1-807d-4c6e-a558-88c61b439659"  "DevicePath"="/dev/vdb"
```

From openstack side you will see following result:

```
$ openstack server list
+--------------------------------------+-------+--------+--------------------------------+--------+---------+
| ID                                   | Name  | Status | Networks                       | Image  | Flavor  |
+--------------------------------------+-------+--------+--------------------------------+--------+---------+
| 17e540e6-8d08-4a5a-8835-668bc8fe913c | demo1 | ACTIVE | demo-net=10.0.0.13             | cirros | m1.tiny |
+--------------------------------------+-------+--------+--------------------------------+--------+---------+
$ openstack volume list
+--------------------------------------+-----------------------------------+-----------+------+--------------------------------+
| ID                                   | Name                              | Status    | Size | Attached to                    |
+--------------------------------------+-----------------------------------+-----------+------+--------------------------------+
| ed893ce1-807d-4c6e-a558-88c61b439659 | v1                                | in-use    |    1 | Attached to demo1 on /dev/vdb  |
+--------------------------------------+-----------------------------------+-----------+------+--------------------------------+

Note:
volume "Status" field will change to "in-use" afterwards.
"Attached to" field will change to volume mount point.
```

### ControllerUnpublish a volume
ControllerUnpublish is reserver action of ControllerPublish, which is similar to ``nova volume-detach``
```
[root@kvm-017212 docs]# csc controller unpublish --endpoint tcp://127.0.0.1:10000 --node-id=17e540e6-8d08-4a5a-8835-668bc8fe913c ed893ce1-807d-4c6e-a558-88c61b439659

ed893ce1-807d-4c6e-a558-88c61b439659
```

### NodePublish a volume
```
$ csc node publish --endpoint tcp://127.0.0.1:10000 --target-path /mnt/cinder --pub-info DevicePath="/dev/xxx" CSIVolumeID
CSIVolumeID
```

### NodeUnpublish a volume
```
$ csc node unpublish --endpoint tcp://127.0.0.1:10000 --target-path /mnt/cinder CSIVolumeID
CSIVolumeID
```

### Get NodeID
```
$ csc node get-id --endpoint tcp://127.0.0.1:10000
CSINodeID
```
