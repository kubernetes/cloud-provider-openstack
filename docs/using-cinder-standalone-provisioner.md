# [DEPRECATED] 
Standalone Cinder Driver is deprecated will be removed in future releases. Please use [Cinder CSI Plugin](/docs/using-cinder-csi-plugin.md/)

## Standalone-cinder external provisioner
The standalone-cinder external provisioner fulfills persistent
volume claims by creating cinder volumes and mapping them
to natively supported volume sources.  This approach allows
cinder to be used as a storage service whether or not the
cluster is deployed on Openstack.  By mapping cinder
volume connection information to native volume sources,
persistent volumes can be attached and mounted using the
standard kubelet machinery and without help from cinder.

### Mapping cinder volumes to native PVs
The provisioner works by mapping cinder volume connections 
(iscsi, rbd, fc, etc) to the corresponding native/raw kubernetes
volume types.  New cinder types can be supported in the provisioner
by creating a new implementation of the volumeMapper interface.  The
implementation is responsible for building a PersistentVolumeSource
from cinder connection information and for setup and teardown of any
authentication (CHAP secret, cephx secret, etc) if required.

Support for cinder backends without a corresponding kubernetes raw
volume implementation could be added in the future by providing a
FlexVolume implementation for the type.

### Connecting to cinder
The provisioner directly uses the gophercloud SDK to connect to
cinder (as opposed to use of the cloudprovider interface).  The
intention is to support two modes of operation: a conventional
cinder deployment with keystone managing authentication and
providing the service catalog, and a standalone configuration where
cinder is accessed directly.

Conventional cinder deployments can be used by supplying a cloud
config file identical to the one you would use to configure an
openstack cloud provider.

### Workflows
| User       | Kubernetes   | Provisioner  | Cinder       |
| ---------- | ------------ | ------------ | ------------ |
| **Provision storage** | | | |
| Issue a PVC for a 100GB RWO volume | | | |
| | Posted the PVC on API server | | |
| | | Acquire the PVC and call cinder create | |
| | | | Create 100GB volume and return info |
| | | Call cinder reserve | |
| | | | Volume is marked as reserved/attaching |
| | | Call cinder os-initialize_connection | |
| | | | Create connection on storage server and return info (ISCSI target and LUN info) |
| | | Create PV using ISCSI as raw volume type using connection info ||
| | Bind the PV to the PVC | | |
| **Use storage** | | | |
| Create a Pod including the PVC | | | |
| | Attach ISCSI PV to node | | |
| | Create filesystem on device and mount into Pod | | |
| | Pod is running | | |
| Delete Pod | | | |
| | Pod is stopped | | |
| | Unmount filesystem | | |
| | Detach ISCSI PV | | |
| **Delete storage** | | | |
| Delete PVC | | | |
| | | Call cinder os-terminate_connection | |
| | | | Remove connection on storage server |
| | | Call cinder unreserve | |
| | | | Volume is marked as available |
| | | Call cinder delete | |
| | | | Delete volume from storage |
| | Delete PV | | |

### Cloning
Standalone cinder has support for provisioning new volumes as a clone of an
existing PVC.  To request a clone, the user should add a clone request
annotation to the PVC:

```
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: clone-claim
  annotations:
    k8s.io/CloneRequest: default/source-pvc
```

The provisioner will locate the indicated PVC and determine the underlying
associated cinder volume ID.  The provisioner will use this ID as the
source-vol-id parameter when provisioning the new volume resulting in a clone
at the storage level.

Cloning will only work if the cinder backend supports "smart-cloning".  This
means that the clone will complete without the need for cinder to perform a
bit-for-bit copy using the host.  Because of this, the cinder provisioner will
not attempt to clone unless the associated storage class contains the
parameter: "smartclone" to indicate that volumes provisioned from this storage
class can be cloned correctly.

If the provisioner was able to clone the volume it will apply the
'k8s.io/CloneOf' annotation to the PVC.

