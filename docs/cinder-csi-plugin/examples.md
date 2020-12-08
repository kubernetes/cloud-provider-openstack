<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Cinder CSI Driver Usage Examples](#cinder-csi-driver-usage-examples)
  - [Dynamic Volume Provisioning](#dynamic-volume-provisioning)
  - [Deploy app using Inline volumes](#deploy-app-using-inline-volumes)
  - [Volume Expansion Example](#volume-expansion-example)
  - [Using Block Volume](#using-block-volume)
  - [Snapshot Create and Restore](#snapshot-create-and-restore)
  - [Disaster recovery of PV and PVC](#disaster-recovery-of-pv-and-pvc)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Cinder CSI Driver Usage Examples

All following examples need to be used inside instance(s) provisoned by openstack, otherwise the attach action will fail due to fail to find instance ID from given openstack cloud.

## Dynamic Volume Provisioning

For dynamic provisoning , create StorageClass, PersistentVolumeClaim and pod to consume it. 
Checkout [sample app](https://github.com/kubernetes/cloud-provider-openstack/blob/master/examples/cinder-csi-plugin/nginx.yaml) definition fore reference.

```kubectl -f examples/cinder-csi-plugin/nginx.yaml create```

Check the pvc is in `Bound` state  which claims one volume from cinder

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

To Check the volume created and attached to the pod

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

## Deploy app using Inline volumes

Sample App definition on using Inline volumes can be found at [here](https://github.com/kubernetes/cloud-provider-openstack/blob/master/examples/cinder-csi-plugin/inline/inline-example.yaml)

Prerequisites:
* Deploy CSI Driver, with both volumeLifecycleModes enabled as specified [here](https://github.com/kubernetes/cloud-provider-openstack/blob/master/manifests/cinder-csi-plugin/csi-cinder-driver.yaml)

Create a pod with inline volume
```
$ kubectl create -f examples/cinder-csi-plugin/inline/inline-example.yaml
```
Get the pod description, verify created volume in Volumes section.
```
$ kubectl describe pod inline-pod

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

## Volume Expansion Example

Sample App definition for volume resize could be found [here](https://github.com/kubernetes/cloud-provider-openstack/blob/master/examples/cinder-csi-plugin/resize/example.yaml)

Deploy the sample app 
```
$ kubectl create -f examples/cinder-csi-plugin/resize/example.yaml
```

Verify PV is created and bound to PVC

```
$ kubectl get pvc
NAME                     STATUS    VOLUME                                     CAPACITY  ACCESS MODES   STORAGECLASS   AGE
csi-pvc-cinderplugin     Bound     pvc-e36abf50-84f3-11e8-8538-42010a800002   1Gi       RWO            csi-sc-cinderplugin     9s
```
Check Pod is in `Running` state
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

## Using Block Volume 

Sample App definition of pod consuming block volume can be found [here](https://github.com/kubernetes/cloud-provider-openstack/blob/master/examples/cinder-csi-plugin/block/block.yaml)

Deploy the same app  
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

## Snapshot Create and Restore

Sample App definition using snapshots can be found [here](https://github.com/kubernetes/cloud-provider-openstack/tree/master/examples/cinder-csi-plugin/snapshot)

Deploy app , Create Storage Class, Snapshot Class and PVC
```
$ kubectl -f examples/cinder-csi-plugin/snapshot/example.yaml create
storageclass.storage.k8s.io/csi-sc-cinderplugin created
volumesnapshotclass.snapshot.storage.k8s.io/csi-cinder-snapclass created
persistentvolumeclaim/pvc-snapshot-demo created
```

Verify that pvc is bounded
```
$ kubectl get pvc --all-namespaces
NAMESPACE   NAME                STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS          AGE
default     pvc-snapshot-demo   Bound    pvc-4699fa78-4149-4772-b900-9536891fe200   1Gi        RWO 
```
Create Snapshot of the PVC
```
$ kubectl -f examples/cinder-csi-plugin/snapshot/snapshotcreate.yaml create
volumesnapshot.snapshot.storage.k8s.io/new-snapshot-demo created
```
Verify that snapshot is created
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
Restore volume from snapshot
```
$ kubectl -f examples/cinder-csi-plugin/snapshot/snapshotrestore.yaml create
persistentvolumeclaim/snapshot-demo-restore created
```
Verify that volume from snapshot is created
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

## Disaster recovery of PV and PVC

This example assume your Kubernetes cluster is crashed, but your volumes
are still available in the OpenStack cloud:

```
$ openstack volume show e129824a-16fa-486d-a8c7-482945a626ff
+--------------------------------+-----------------------------------------------------------------+
| Field                          | Value                                                           |
+--------------------------------+-----------------------------------------------------------------+
| attachments                    | []                                                              |
| availability_zone              | eu-de-01                                                        |
| bootable                       | false                                                           |
| consistencygroup_id            | None                                                            |
| created_at                     | 2020-11-30T15:16:23.788791                                      |
| description                    | Created by OpenStack Cinder CSI driver                          |
| encrypted                      | False                                                           |
| id                             | e129824a-16fa-486d-a8c7-482945a626ff                            |
| multiattach                    | True                                                            |
| name                           | pvc-ba482e24-2e76-48fd-9be5-504724922185                        |
| os-vol-host-attr:host          | pod07.eu-de-01#SATA                                             |
| os-vol-mig-status-attr:migstat | None                                                            |
| os-vol-mig-status-attr:name_id | None                                                            |
| os-vol-tenant-attr:tenant_id   | 7c3ec0b3db5f476990043258670caf82                                |
| properties                     | cinder.csi.openstack.org/cluster='kubernetes', readonly='False' |
| replication_status             | disabled                                                        |
| shareable                      | True                                                            |
| size                           | 9                                                               |
| snapshot_id                    | None                                                            |
| source_volid                   | None                                                            |
| status                         | available                                                       |
| storage_cluster_id             | None                                                            |
| type                           | SATA                                                            |
| updated_at                     | 2020-12-06T22:51:30.032404                                      |
| user_id                        | 2497435010e14245843bfe20e6f05024                                |
| volume_image_metadata          | {}                                                              |
+--------------------------------+-----------------------------------------------------------------+
```

You can recover the volume to a PV/PVC under the same name/id in a new cluster:

```
$ kubectl apply -f recover-pv.yaml
```
```
apiVersion: v1
kind: PersistentVolume
metadata:
  annotations:
    pv.kubernetes.io/provisioned-by: cinder.csi.openstack.org
  name: pvc-ba482e24-2e76-48fd-9be5-504724922185
spec:
  accessModes:
  - ReadWriteOnce
  capacity:
    storage: 9Gi
  claimRef:
    apiVersion: v1
    kind: PersistentVolumeClaim
    name: sib
    namespace: sib
  csi:
    driver: cinder.csi.openstack.org
    volumeAttributes:
      storage.kubernetes.io/csiProvisionerIdentity: 1606744152466-8081-cinder.csi.openstack.org
    volumeHandle: e129824a-16fa-486d-a8c7-482945a626ff
  nodeAffinity:
    required:
      nodeSelectorTerms:
      - matchExpressions:
        - key: topology.cinder.csi.openstack.org/zone
          operator: In
          values:
          - eu-de-01
  persistentVolumeReclaimPolicy: Delete
  storageClassName: sata
  volumeMode: Filesystem
```

```
cat recover-pvc.yaml
```
```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  annotations:
    volume.beta.kubernetes.io/storage-provisioner: cinder.csi.openstack.org
  name: sib
  namespace: sib
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: sata
  volumeMode: Filesystem
```
