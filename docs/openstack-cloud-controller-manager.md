# OpenStack Cloud Controller Manager

OpenStack Cloud Controller Manager - An external cloud controller manager for running kubernetes
in an OpenStack cluster.

## Introduction

External cloud providers were introduced as an Alpha feature in Kubernetes release 1.6. openstack-cloud-controller-manager
is the implementation of external cloud provider for OpenStack clusters. An external cloud provider
is a kubernetes controller that runs cloud provider-specific loops required for the functioning of
kubernetes. These loops were originally a part of the `kube-controller-manager`, but they were tightly
coupling the `kube-controller-manager` to cloud-provider specific code. In order to free the kubernetes
project of this dependency, the `cloud-controller-manager` was introduced.

`cloud-controller-manager` allows cloud vendors and kubernetes core to evolve independent of each other.
In prior releases, the core Kubernetes code was dependent upon cloud provider-specific code for functionality.
In future releases, code specific to cloud vendors should be maintained by the cloud vendor themselves, and
linked to `cloud-controller-manager` while running Kubernetes.

As such, you must disable these controller loops in the `kube-controller-manager` if you are running the
`openstack-cloud-controller-manager`. You can disable the controller loops by setting the `--cloud-provider`
flag to `external` when starting the kube-controller-manager.

For more details, please see:

- <https://github.com/kubernetes/enhancements/blob/master/keps/sig-cloud-provider/20180530-cloud-controller-manager.md>
- <https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller/#running-cloud-controller-manager>
- <https://kubernetes.io/docs/tasks/administer-cluster/developing-cloud-controller-manager/>

## Examples

Here are some examples of how you could leverage `openstack-cloud-controller-manager`:

- [loadbalancers](../examples/loadbalancers/)
