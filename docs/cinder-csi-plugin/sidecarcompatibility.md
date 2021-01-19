<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Plugin sidecar compatibility](#plugin-sidecar-compatibility)
  - [Set file type in provisioner](#set-file-type-in-provisioner)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Plugin sidecar compatibility

## Set file type in provisioner

There is a change in [csi-provisioner 2.0](https://github.com/kubernetes-csi/external-provisioner/blob/master/CHANGELOG/CHANGELOG-2.0.md): The fstype on provisioned PVs no longer defaults to "ext4". A defaultFStype arg is added to the provisioner. Admins can also specify this fstype via storage class parameter. If fstype is set in storage class parameter, it will be used. The sidecar arg is only checked if fstype is not set in the SC param.

By default, in the manifest file a `--default-fstype=ext4` default settings are added to [manifests](../../manifests/cinder-csi-plugin/cinder-csi-controllerplugin.yaml), if you want to update it , please add a `fsType: ext4` into the storageclass definition.
