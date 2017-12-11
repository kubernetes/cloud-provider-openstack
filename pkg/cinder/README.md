# CSI Cinder driver

## Usage:

### Start Cinder driver
```
$ sudo ./_output/cinderplugin --endpoint tcp://127.0.0.1:10000 --config /etc/openstack.conf --nodeid CSINodeID
```

### Test using csc
Get ```csc``` tool from https://github.com/chakri-nelluri/gocsi/tree/master/csc

#### Get plugin info
```
$ csc identity plugin-info --endpoint tcp://127.0.0.1:10000
"Cinder"	"0.1.0"
```

#### Get supported versions
```
$ csc identity supported-versions --endpoint tcp://127.0.0.1:10000
0.1.0
```

#### Create a volume
```
$ csc controller new --endpoint tcp://127.0.0.1:10000 CSIVolumeName
CSIVolumeID
```

#### Delete a volume
```
$ csc controller del --endpoint tcp://127.0.0.1:10000 CSIVolumeID
CSIVolumeID
```

#### ControllerPublish a volume
```
$ csc controller publish --endpoint tcp://127.0.0.1:10000 --node-id=CSINodeID CSIVolumeID
CSIVolumeID	"DevicePath"="/dev/xxx"
```

#### ControllerUnpublish a volume
```
$ csc controller unpublish --endpoint tcp://127.0.0.1:10000 --node-id=CSINodeID CSIVolumeID
CSIVolumeID
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
