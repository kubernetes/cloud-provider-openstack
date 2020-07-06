<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Migrate from in-tree cloud provider to openstack-cloud-controller-manager and enable CSIMigration](#migrate-from-in-tree-cloud-provider-to-openstack-cloud-controller-manager-and-enable-csimigration)
  - [Before you begin](#before-you-begin)
  - [Migrate to openstack-cloud-controller-manager](#migrate-to-openstack-cloud-controller-manager)
  - [Enable CSIMigration](#enable-csimigration)
  - [Finalize the migration](#finalize-the-migration)
  - [Caveats](#caveats)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Migrate from in-tree cloud provider to openstack-cloud-controller-manager and enable CSIMigration

This guide walks you through the process of migrating from using the Kubernetes in-tree cloud provider (specified by using `--cloud-provider=openstack` on `kube-controller-manager`) to use the `cloud-controller-manager` (CCM) for OpenStack. This document tries to give an example for using multiple steps in order to get the migration fully done. We expect users to want to migrate to `cloud-provider-openstack` but stay with the in-tree `cinder-volume-provisioner` up until `CSIMigration` has become GA.


A little bit of background on CSI and `CSIMigration`. These days, storage providers should implement the [Container Storage Interface](https://github.com/container-storage-interface/spec/blob/master/spec.md) to provide storage for Kubernetes clusters (but not limited to Kubernetes). `cloud-provider-openstack` also provides [Cinder CSI Plugin](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-cinder-csi-plugin.md) to serve exact this purpose. This plugin can be installed and used alongside in-tree `cloud-provider`, because it requires its own `StorageClass`. What this means is: All volumes, created by the in-tree volume APIs will be handled by the old in-tree-provisioner one. Everything provisioned by Cinder CSI Plugin, will be handled by `cinder-csi-plugin`.

Sometimes it can be hard to migrate an entire cluster to use the new `StorageClass`. This is where `CSIMigration` comes into play (see [CSIMigration design proposal](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/csi-migration.md)). With this enabled, calls to the in-tree volume APIs will call out to the CSI plugins. See [In-tree Storage Plugin to CSI Migration Design Doc](https://github.com/kubernetes/enhancements/blob/master/keps/sig-storage/20190129-csi-migration.md) for more detail.

## Before you begin

Note: This guide works on a cluster created by `kubeadm`.

Example `kubeadm.yaml`:

```yaml
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
kubernetesVersion: 1.17.0-beta.1
networking:
  podSubnet: 10.96.0.0/16
  serviceSubnet: 10.97.0.0/16
controllerManager:
  extraArgs:
    cloud-provider: openstack
    cloud-config: /etc/kubernetes/cloud.conf
  extraVolumes:
  - name: cloud
    hostPath: /etc/kubernetes/cloud.conf
    mountPath: /etc/kubernetes/cloud.conf
    readOnly: true
---
apiVersion: kubeadm.k8s.io/v1beta2
kind: InitConfiguration
localAPIEndpoint:
  bindPort: 6443
nodeRegistration:
  kubeletExtraArgs:
    cloud-provider: openstack
    cloud-config: /etc/kubernetes/cloud.conf
```

## Migrate to openstack-cloud-controller-manager

Remember... In the first step, we want to keep the in-tree volume API, but for all of the rest, we want to use `openstack-cloud-controller-manager`. So we can't just remove the `--cloud-provider` flag from `kube-controller-manager`. In fact, we must disable all cloud related controllers.
Create the file `kubeadm-disable-controllers.yaml`:

```yaml
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
kubernetesVersion: 1.17.0-beta.1
networking:
  podSubnet: 10.96.0.0/16
  serviceSubnet: 10.97.0.0/16
controllerManager:
  extraArgs:
    cloud-provider: openstack
    cloud-config: /etc/kubernetes/cloud.conf
    controllers: '*,bootstrapsigner,tokencleaner,-cloud-node-lifecycle,-route,-service'
  extraVolumes:
  - name: cloud
    hostPath: /etc/kubernetes/cloud.conf
    mountPath: /etc/kubernetes/cloud.conf
    readOnly: true
```

And create new manifests for `kube-controller-manager`:

```bash
kubeadm init --config kubeadm-disable-controllers.yaml phase control-plane controller-manager
```

To verify, check the logs of `kube-controller-manager` for the following lines.

```
W1117 19:59:34.094182       1 controllermanager.go:513] "service" is disabled
W1117 19:59:34.094187       1 controllermanager.go:513] "route" is disabled
W1117 19:59:44.988353       1 controllermanager.go:513] "cloud-node-lifecycle" is disabled
```

You can now deploy `openstack-cloud-controller-manager` using [RBAC](https://github.com/kubernetes/cloud-provider-openstack/tree/master/cluster/addons/rbac) and the [CCM manifest](https://github.com/kubernetes/cloud-provider-openstack/blob/master/manifests/controller-manager/openstack-cloud-controller-manager-ds.yaml). But disable the controller `cloud-node` by adding the argument `--controllers=*,-cloud-node`. This will be done by the `kubelets` up until the entire migration is done. Note: You must create a secret `cloud-config` with a valid `cloud.conf` for the CCM. E.g.

```bash
kubectl -n kube-system create secret generic cloud-config --from-file=cloud.conf=/etc/kubernetes/cloud.conf
```

Note: Because you still want to use the in-tree volume API, you **must** keep the arguments on the `kubelets` untouched (meaning, stick with `--cloud-provider=openstack` and `--cloud-config=/etc/kubernetes/cloud.conf`) for now.

At this point, the cluster should work as before. LoadBalancers can be created and `StatefulSets` create and use persistent volumes. Note: The logs of `openstack-cloud-controller-manager` in namespace `kube-system` reveal, that this now takes care of creating LoadBalancers. All further management of existing LoadBalancers is taken over by `openstack-cloud-controller-manager` as well. There is no need to recreate any LoadBalancer.

At this point, you should also deploy [Cinder CSI Plugin](./using-cinder-csi-plugin.md). As mentioned above, this can be deployed alogside the in-tree provider. We would recommend to create a separate `StorageClass` for Cinder CSI Plugin, and make it the default. (see https://github.com/kubernetes/cloud-provider-openstack/tree/master/examples/cinder-csi-plugin for examples)

## Enable CSIMigration

This walks you through enabling needed `feature-gates` and explains necessary steps.

The first step is to ebale the `feature-gates` on the control plane. We again use `kubeadm.yaml` and `kubeadm` to perform necessary tasks.

```yaml
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
kubernetesVersion: "1.17.0-beta.1"
networking:
  podSubnet: 10.96.0.0/16
  serviceSubnet: 10.97.0.0/16
apiServer:
  extraArgs:
    feature-gates: "CSIMigration=true,CSIMigrationOpenStack=true,ExpandCSIVolumes=true"
controllerManager:
  extraArgs:
    feature-gates: "CSIMigration=true,CSIMigrationOpenStack=true,ExpandCSIVolumes=true"
    controllers: "*,bootstrapsigner,tokencleaner,-cloud-node-lifecycle,-route,-service"
    cloud-config: /etc/kubernetes/cloud.conf
    cloud-provider: openstack
  extraVolumes:
  - name: cloud
    hostPath: /etc/kubernetes/cloud.conf
    mountPath: /etc/kubernetes/cloud.conf
    readOnly: true
```

In this config, we still leave the `cloud-provider` and `cloud-config` arguments untouched, but enable the needed feature-gates.

```
# enable feature-gates for CSIMigration on apiserver and controller-manager
kubeadm init --config ~ubuntu/kubeadm-migrate-1.yaml phase control-plane apiserver
kubeadm init --config ~ubuntu/kubeadm-migrate-1.yaml phase control-plane controller-manager
```

You must now drain an existing node and enable the `feature-gates` on `kubelet`. (At this point, you could also change to `--cloud-provider=external` and remove the `--cloud-config` argument)

```bash
cat >> /var/lib/kubelet/config.yaml<<EOF
featureGates:
  CSIMigration: true
  CSIMigrationOpenStack: true
  ExpandCSIVolumes: true
EOF

systemctl restart kubelet
```

Verify the CSI settings for that particular node:

```
root@small-k8s-1:~# kubectl get csinode small-k8s-2 -oyaml
apiVersion: storage.k8s.io/v1
kind: CSINode
metadata:
  annotations:
    storage.alpha.kubernetes.io/migrated-plugins: kubernetes.io/cinder
  ...
```

The first `Pod` of a `StatefulSet` scheduled to that node, will get its own `VolumeAttachment`, and you will find `ATTACHER = cinder.csi.openstack.org`.

```
root@small-k8s-1:~# kubectl get volumeattachment
NAME                                                                   ATTACHER                   PV                                         NODE          ATTACHED   AGE
csi-1fa81053386026b068208e1522b3a8db31fd6f9e1828999c34ee555255a3ab13   cinder.csi.openstack.org   pvc-a94be977-8644-4a5f-900b-d1db2a1f8eb9   small-k8s-2   true       2m42s
```

To migrate all nodes, you must do the same for all of them. Drain, enable `feature-gates`, uncordon.

## Finalize the migration

There are a couple of loose ends remaining. `kubelets` might still run `--cloud-provider=openstack`, when you didn't change that during `CSIMigration`. CCM does not start the controller `cloud-node`. And `kube-controller-manager` also still uses a custom configuration. Let's get rid of all that.

On nodes, edit `/var/lib/kubelet/kubeadm-flags.env` to use `--cloud-provider=external` and remove `--cloud-config=...`, and restart `kubelet`. Head over, and edit the `StatefulSet` of `openstack-cloud-controller-manager` to remove the argument `--controller=*,-cloud-node`. Again, use `kubeadm.yaml` and `kubeadm` to remove `controllers`, `cloud-provider` and `cloud-config`.

```yaml
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
kubernetesVersion: "1.17.0-beta.1"
networking:
  podSubnet: 10.96.0.0/16
  serviceSubnet: 10.97.0.0/16
apiServer:
  extraArgs:
    feature-gates: "CSIMigration=true,CSIMigrationOpenStack=true,ExpandCSIVolumes=true"
controllerManager:
  extraArgs:
    feature-gates: "CSIMigration=true,CSIMigrationOpenStack=true,ExpandCSIVolumes=true"
```

```bash
kubeadm init --config ~ubuntu/kubeadm-migrate-final.yaml phase control-plane controller-manager
```

## Caveats

At the time of writing, Kubernetes 1.17.0 is at our doorstep. After a full migration (like you just did), we discovered a problem with deleting `PersistentVolumes` which have been created before enabling `CSIMigration`. They still reference the old in-tree provisioner, which is not available any more, because we removed the `cloud-config` from `kube-controller-manager`. The workaround for this is rather simple. At some point before removing the `PersistentVolume`, change the annotation `pv.kubernetes.io/provisioned-by` to `cinder.csi.openstack.org` for all old volumes.

e.g.
```bash
kubectl annotate --overwrite pv pvc-d4f3d362-66c1-41d5-92b1-b9c07705dec7 pv.kubernetes.io/provisioned-by=cinder.csi.openstack.org
```

Note: if you haven't done that, but deleted a `PersistentVolume`, it is still possible to perform this task and trigger another delete. The `PersistentVolume` will go away, but will leak the corresponding volume in Cinder.
