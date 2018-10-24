# CSI Cinder driver

## Kubernetes

### Requirements

The following feature gates and runtime config have to be enabled to deploy the driver.

```
FEATURE_GATES=CSIPersistentVolume=true,MountPropagation=true
RUNTIME_CONFIG="storage.k8s.io/v1alpha1=true"
```

MountPropagation requires support for privileged containers. So, make sure privileged containers are enabled in the cluster.

### Example local-up-cluster.sh

```ALLOW_PRIVILEGED=true FEATURE_GATES=CSIPersistentVolume=true,MountPropagation=true RUNTIME_CONFIG="storage.k8s.io/v1alpha1=true" LOG_LEVEL=5 hack/local-up-cluster.sh```

### Deploy

Encode your ```cloud.conf``` file content using base64.

```base64 -w 0 cloud.conf```

Update ```cloud.conf``` configuration in ```manifests/cinder-csi-plugin/csi-secret-cinderplugin.yaml``` file
by using the result of the above command.

```kubectl -f manifests/cinder-csi-plugin create```

### Example Nginx application

```kubectl -f examples/cinder-csi-plugin/nginx.yaml create```

## Using CSC tool

### Test using csc
Get ```csc``` tool from https://github.com/thecodeteam/gocsi/tree/master/csc

### Start Cinder driver

First, you need to start the plugin as daemon to accept request from csc.
Following example is starting listening at localhost port 10000 with cloud configuration
given in /etc/cloud.conf and the node id is CSINodeID.

```
$ sudo cinder-csi-plugin --endpoint tcp://127.0.0.1:10000 --cloud-config /etc/cloud.conf --nodeid CSINodeID
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
$  csc controller create-volume --endpoint tcp://127.0.0.1:10000 CSIVolumeName
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
