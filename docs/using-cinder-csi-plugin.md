# CSI Cinder driver

## Kubernetes

### Compatibility

CSI version | CSI Sidecar Version | Cinder CSI Plugin Version | Kubernetes Version
:------ | :------- | :------------ | :-----------
v1.0.x | v1.0.x | v1.0.0  docker image: k8scloudprovider/cinder-csi-plugin:latest | v1.13+
v0.3.0 | v0.3.x, v0.4.x | v0.3.0 docker image: k8scloudprovider/cinder-csi-plugin:1.13.x| v1.11, v1.12, v1.13
v0.2.0 | v0.2.x | v0.2.0 docker image: k8scloudprovider/cinder-csi-plugin:0.2.0 | v1.10, v1.9
v0.1.0 | v0.1.0 | v0.1.0 docker image: k8scloudprovider/cinder-csi-plugin:0.1.0| v1.9

### Requirements

```
RUNTIME_CONFIG="storage.k8s.io/v1alpha1=true"
```
MountPropagation requires support for privileged containers. So, make sure privileged containers are enabled in the cluster.

Check [kubernetes CSI Docs](https://kubernetes-csi.github.io/docs/) for flag details and latest update.

### Example local-up-cluster.sh

```ALLOW_PRIVILEGED=true RUNTIME_CONFIG="storage.k8s.io/v1alpha1=true" LOG_LEVEL=5 hack/local-up-cluster.sh```

### Deploy

If you already created the `cloud-config` secret used by the [cloud-controller-manager](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-controller-manager-with-kubeadm.md#steps) jump directly to the `kubectl apply ...` command.

Encode your ```$CLOUD_CONFIG``` file content using base64.

```base64 -w 0 $CLOUD_CONFIG```

Update ```cloud.conf``` configuration in ```manifests/cinder-csi-plugin/csi-secret-cinderplugin.yaml``` file
by using the result of the above command.

```kubectl -f manifests/cinder-csi-plugin apply```

This creates a set of cluster roles, cluster role bindings, and statefulsets etc to communicate with openstack(cinder).
For detailed list of created objects, explore the yaml files in the directory.
You should make sure following similar pods are ready before proceed:

```
NAME                                READY   STATUS    RESTARTS   AGE
csi-attacher-cinderplugin-0         2/2     Running   0          29h
csi-nodeplugin-cinderplugin-xxfh5   2/2     Running   0          46h
csi-provisioner-cinderplugin-0      2/2     Running   0          46h
```

### Example Nginx application usage

After performing above steps, you can try to create StorageClass, PersistentVolumeClaim and pod to consume it.
Try following command:

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

1. Enable `-feature-gates="VolumeSnapshotDataSource=true` in kube-apiserver
2. Make Sure, your csi deployment contains external-snapshotter sidecar container, external-snapshotter sidecar container will create three crd's for snapshot management VolumeSnapshot,VolumeSnapshotContent, and VolumeSnapshotClass.       
To deploy cinder csi driver and all required sidecars run
```
kubectl -f manifests/cinder-csi-plugin create
```

For Snapshot Creation and Volume Restore, please follow  below steps:

* Create Storage Class, Snapshot Class and PVC    
```
$ kubectl -f examples/cinder-csi-plugin/example.yaml create
```     
* Verify that pvc is bounded
``` 
$ kubectl describe pvc
```   
* Create Snapshot of the PVC    
```
$ kubectl -f examples/cinder-csi-plugin/snapshotcreate.yaml create
```       
* Verify that snapshot is created    
```
$ kubectl get volumesnapshot 
$ kubectl get volumesnapshotcontent
```   
* Restore volume from snapshot    
```
$ kubectl -f examples/cinder-csi-plugin/snapshotrestore.yaml create
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

#### Get plugin info
```
$ csc identity plugin-info --endpoint tcp://127.0.0.1:10000
"csi-cinderplugin"      "0.1.0"
```

#### Get supported capabilities
```
$ csc identity plugin-capabilities --endpoint tcp://127.0.0.1:10000
CONTROLLER_SERVICE
```

#### Get controller implemented capabilities
```
$ csc controller get-capabilities  --endpoint tcp://127.0.0.1:10000
&{type:CREATE_DELETE_VOLUME }
&{type:PUBLISH_UNPUBLISH_VOLUME }
&{type:LIST_VOLUMES }
```

#### Create a volume
Following sample creates a volume named ``CSIVolumeName`` and the
volume id returned is ``8a55f98f-e987-43ab-a9f5-973352bee19c`` with size ``1073741824`` bytes (1Gb)
```
$ csc controller create-volume --endpoint tcp://127.0.0.1:10000 CSIVolumeName
"8a55f98f-e987-43ab-a9f5-973352bee19c"  1073741824      "availability"="nova"
```

#### Delete a volume
Following sample deletes a volume ``01217e93-bd1b-4760-b5d8-18b8b3d47f91``
```
$ csc controller delete-volume --endpoint tcp://127.0.0.1:10000 01217e93-bd1b-4760-b5d8-18b8b3d47f91
01217e93-bd1b-4760-b5d8-18b8b3d47f91
```

#### ControllerPublish a volume
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

#### ControllerUnpublish a volume
ControllerUnpublish is reserver action of ControllerPublish, which is similar to ``nova volume-detach``
```
[root@kvm-017212 docs]# csc controller unpublish --endpoint tcp://127.0.0.1:10000 --node-id=17e540e6-8d08-4a5a-8835-668bc8fe913c ed893ce1-807d-4c6e-a558-88c61b439659

ed893ce1-807d-4c6e-a558-88c61b439659
```

#### NodePublish a volume
```
$ csc node publish --endpoint tcp://127.0.0.1:10000 --target-path /mnt/cinder --pub-info DevicePath="/dev/xxx" CSIVolumeID
CSIVolumeID
```

#### NodeUnpublish a volume
```
$ csc node unpublish --endpoint tcp://127.0.0.1:10000 --target-path /mnt/cinder CSIVolumeID
CSIVolumeID
```

#### Get NodeID
```
$ csc node get-id --endpoint tcp://127.0.0.1:10000
CSINodeID
```
