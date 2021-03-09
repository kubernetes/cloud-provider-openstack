<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Development tips for Cinder CSI](#development-tips-for-cinder-csi)
  - [Update CSI spec version and Cinder CSI driver version](#update-csi-spec-version-and-cinder-csi-driver-version)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->


# Development tips for Cinder CSI

## Update CSI spec version and Cinder CSI driver version

There are two versions (`specVersion` and `Version`) defined at [driver.go](../../pkg/csi/cinder/driver.go) and both of them are in `x.y.z` format. `specVersion` indicates the version of [CSI spec](https://github.com/container-storage-interface/spec) that Cinder CSI supports whereas `Version` is the version of Cinder CSI driver itself. For new each release or major functionalities update such as options/params updated, you need increase `.z` version. If the CSI spec version is upgraded, the Cinder CSI version need bump as well.

For example, `specVersion` is `1.2.0` and `Version` is `1.2.1` then there's a new feature or option added but CSI spec remains same, the `specVersion` need to be kept as `1.2.0` and `Version` need to be bumped to `1.2.2`. If the CSI spec is bumpped to `1.3.0`, the `specVersion` and `Version` need to be bumped to `1.3.0` accordingly.
