# OpenStack Cloud Controller Manager

Thank you for visiting the `openstack-cloud-controller-manager` repository!

OpenStack Cloud Controller Manager - An external cloud controller manager for running kubernetes
in an OpenStack cluster.

## Introduction

External cloud providers were introduced as an Alpha feature in Kubernetes release 1.6. This repository
contains an implementation of external cloud provider for OpenStack clusters. An external cloud provider
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
- https://github.com/kubernetes/community/blob/master/keps/0002-controller-manager.md
- https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller/#running-cloud-controller-manager
- https://kubernetes.io/docs/tasks/administer-cluster/developing-cloud-controller-manager/

## Examples

Here are some examples of how you could leverage `openstack-cloud-controller-manager`:

- [loadbalancers](examples/loadbalancers/)

## Developing

`make` will build, test, and package this project. This project uses trash Glide for dependency management.

If you don't have a Go Environment setup, we also offer the ability to run make
in a Docker Container.  The only requirement for this is that you have Docker
installed and configured (of course).  You don't need to have a Golang
environment setup, and you don't need to follow rules in terms of directory
structure for the code checkout.

To use this method, just call the `hack/make.sh` script with the desired argument:
    `hack/make.sh build`  for example will run `make build` in a container.

NOTE You MUST run the script from the root source directory as shown above,
attempting to do something like `cd hack && make.sh build` will not work
because we won't bind mount the source files into the container.

## License
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
