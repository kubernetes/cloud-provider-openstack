<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Using CSC tool for Testing](#using-csc-tool-for-testing)
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

# Using CSC tool for Testing

## Test using csc
Get ```csc``` tool from https://github.com/thecodeteam/gocsi/tree/master/csc

## Start Cinder driver

First, you need to start the plugin as daemon to accept request from csc.
Following example is starting listening at localhost port 10000 with cloud configuration
given in /etc/cloud.conf and the node id is CSINodeID. ClusterID is the identifier of
the cluster in which the plugin is running.

```
$ sudo cinder-csi-plugin --endpoint tcp://127.0.0.1:10000 --cloud-config /etc/cloud.conf --nodeid CSINodeID --cluster ClusterID
```

## Get plugin info
```
$ csc identity plugin-info --endpoint tcp://127.0.0.1:10000
"cinder.csi.openstack.org"      "1.0.0"
```

## Get supported capabilities
```
$ csc identity plugin-capabilities --endpoint tcp://127.0.0.1:10000
CONTROLLER_SERVICE
VOLUME_ACCESSIBILITY_CONSTRAINTS
```

## Get controller implemented capabilities
```
$ csc controller get-capabilities  --endpoint tcp://127.0.0.1:10000
&{type:LIST_VOLUMES }
&{type:CREATE_DELETE_VOLUME }
&{type:PUBLISH_UNPUBLISH_VOLUME }
&{type:CREATE_DELETE_SNAPSHOT }
&{type:LIST_SNAPSHOTS }
```

## Create a volume
Following sample creates a volume named ``CSIVolumeName`` and the
volume id returned is ``8a55f98f-e987-43ab-a9f5-973352bee19c`` with size ``1073741824`` bytes (1Gb)
```
$ csc controller create-volume --endpoint tcp://127.0.0.1:10000 CSIVolumeName
"8a55f98f-e987-43ab-a9f5-973352bee19c"  1073741824      "availability"="nova"
```

## List volumes
Following sample list all volumes:
```
$ csc controller list-volumes --endpoint tcp://127.0.0.1:10000
"8a55f98f-e987-43ab-a9f5-973352bee19c"  1073741824
```

## Delete a volume
Following sample deletes a volume ``01217e93-bd1b-4760-b5d8-18b8b3d47f91``
```
$ csc controller delete-volume --endpoint tcp://127.0.0.1:10000 01217e93-bd1b-4760-b5d8-18b8b3d47f91
01217e93-bd1b-4760-b5d8-18b8b3d47f91
```

## Create a snapshot from volume
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

## List snapshots

Following sample lists all snapshots:
```
$ csc controller  list-snapshots --endpoint tcp://127.0.0.1:10000
"e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f" 1073741824      40615da4-3fda-4e78-bf58-820692536e68    seconds:1561532425      true
```

## Delete a snapshot

Following sample deletes the snapshot `e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f`.
```
$ csc controller delete-snapshot e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f --endpoint tcp://127.0.0.1:10000
e2df8c2a-58eb-40fb-8ec9-45aee5b8f39f
```

Use openstack command to verify:
```
$ openstack volume snapshot list

```

## ControllerPublish a volume
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

## ControllerUnpublish a volume
ControllerUnpublish is reserver action of ControllerPublish, which is similar to ``nova volume-detach``
```
[root@kvm-017212 docs]# csc controller unpublish --endpoint tcp://127.0.0.1:10000 --node-id=17e540e6-8d08-4a5a-8835-668bc8fe913c ed893ce1-807d-4c6e-a558-88c61b439659

ed893ce1-807d-4c6e-a558-88c61b439659
```

## NodePublish a volume
```
$ csc node publish --endpoint tcp://127.0.0.1:10000 --target-path /mnt/cinder --pub-info DevicePath="/dev/xxx" CSIVolumeID
CSIVolumeID
```

## NodeUnpublish a volume
```
$ csc node unpublish --endpoint tcp://127.0.0.1:10000 --target-path /mnt/cinder CSIVolumeID
CSIVolumeID
```

## Get NodeID
```
$ csc node get-id --endpoint tcp://127.0.0.1:10000
CSINodeID
```
