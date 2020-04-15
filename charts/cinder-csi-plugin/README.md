# Cinder CSI volume provisioner

Deploys a Cinder csi provisioner to your cluster, with the appropriate storageClass.

## How To install
- Enable deployment of storageclasses using `storageClass.enabled`
- Tag the retain or delete class as default class using `storageClass.delete.isDefault` in your value yaml
- Set `storageClass.<reclaim-policy>.allowVolumeExpansion` to `true` or `false`