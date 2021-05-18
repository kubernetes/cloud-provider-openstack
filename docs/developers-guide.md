<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Getting Started with Cloud Provider OpenStack Development](#getting-started-with-cloud-provider-openstack-development)
    - [Prerequisites](#prerequisites)
        - [OpenStack Cloud](#openstack-cloud)
        - [Kubernetes cluster](#kubernetes-cluster)
    - [Contribution](#contribution)
    - [Development](#development)
        - [Build openstack-cloud-controller-manager image](#build-openstack-cloud-controller-manager-image)
        - [Troubleshooting](#troubleshooting)
        - [Review process](#review-process)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Getting Started with Cloud Provider OpenStack Development

This guide will help you get started with building a development environment run a single node Kubernetes cluster with the OpenStack Cloud Provider
enabled.


## Prerequisites

### OpenStack Cloud
You will need access to an OpenStack cloud, either public or private. You can sign up
for a public OpenStack Cloud through the [OpenStack Passport](https://www.openstack.org/passport)
program, or you can install a small private development cloud with
[DevStack](https://docs.openstack.org/devstack/latest/).

openstack-cloud-controller-manager relies on [Octavia](https://docs.openstack.org/octavia/latest/) to create Kubernetes Service of type LoadBalancer, so make sure Octavia is available in the OpenStack cloud. For public cloud, you need to check the cloud documentation. For devstack, refer to Octavia deployment [quick start guide](https://docs.openstack.org/octavia/latest/contributor/guides/dev-quick-start.html). If you want to create service with TLS termination, [Barbican](https://docs.openstack.org/barbican/latest/) is also required.

DevStack is recommended for cloud provider openstack development as some features are dependent on latest versions of openstack services, which is not always the case for public cloud providers. To get better performance, it's recommended to enable [nested virtualization](https://docs.openstack.org/devstack/latest/guides/devstack-with-nested-kvm.html) on the devstack host.

### Kubernetes cluster
Deploy a kubernetes cluster in the openstack cloud.

There are many [deployment tools](https://kubernetes.io/docs/tasks/tools/) out there to help you set up a kubernetes cluster, you can also follow the [kubernetes contributor guide](https://github.com/kubernetes/community/blob/master/contributors/devel/running-locally.md) to run a cluster locally. As you already have openstack, you can take a look at [Cluster API Provider OpenStack](https://github.com/kubernetes-sigs/cluster-api-provider-openstack) as well.

Choose the one you are familiar with and easy to customize. Config the cluster with [external cloud controller manager](https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller).

Using kubeadm, openstack-cloud-controller-manager can be deployed easily with predefined manifests, see the [deployment guide with kubeadm](openstack-cloud-controller-manager/using-openstack-cloud-controller-manager.md#deploy-a-kubernetes-cluster-with-openstack-cloud-controller-manager-using-kubeadm).


## Contribution
Now you should have a kubernetes cluster running in openstack and openstack-cloud-controller-manager is deployed in the cluster. Over time, you may find a bug or have some feature requirements, it's time for contribution!

A contribution can be anything which helps this project improving. In addition to code contribution it can be testing, documentation, requirements gathering, bug reporting and so forth.

See [Contributing Guidelines](../CONTRIBUTING.md) for some general information about contribution.


## Development
If you are ready for code contribution, you need to have development environment.

1. Install [Golang](https://golang.org/doc/install). You can choose the golang version same with the version defined in [go.mod](https://github.com/kubernetes/cloud-provider-openstack/blob/master/go.mod#L3) file.
1. IDE. You can choose any IDE that supports Golang, e.g. Visual Studio Code, GoLand, etc.
1. Install [Docker Engine](https://docs.docker.com/engine/install/) or other tools that could build container images such as [podman](https://podman.io/). After writing the code, the best way to test openstack-cloud-controller-manager is to replace the container image specified in its Deployment/StatefulSet/DaemonSet manifest in the kubernetes cluster. We are using docker in this guide.

### Build openstack-cloud-controller-manager image
In cloud-provider-openstack repo directory, run:

```
REGISTRY=<your-dockerhub-account> \
VERSION=<image-tag> \
make image-openstack-cloud-controller-manager
```

The above command builds a container image locally with the name:

```
<your-dockerhub-account>/openstack-cloud-controller-manager-amd64:<image-tag>
```

You may notice there is a suffix `-amd64` because cloud-provider-openstack supports to build images with multiple architectures (`amd64 arm arm64 ppc64le s390x`) and `amd64` is the default. You can specify others with the command:

```
ARCH=amd64 \
REGISTRY=<your-dockerhub-account> \
VERSION=<image-tag> \
make image-openstack-cloud-controller-manager
```

If the kubernetes cluster can't access the image locally, you need to upload the image to container registry first by running `docker push`.


The image can also be uploaded automatically together with build:

```
REGISTRY=<your-dockerhub-account> \
VERSION=<image-tag> \
DOCKER_USERNAME=<your-dockerhub-account> \
DOCKER_PASSWORD=<your-dockerhub-password> \
IMAGE_NAMES=openstack-cloud-controller-manager \
make upload-image-amd64
```

Now, you can change openstack-cloud-controller-manager image in the kubernetes cluster, assuming the openstack-cloud-controller-manager is running as a DaemonSet:

```
kubectl -n kube-system set image \
  daemonset/openstack-cloud-controller-manager \
  openstack-cloud-controller-manager=<your-dockerhub-account>/openstack-cloud-controller-manager-amd64:<image-tag>
```

### Troubleshooting
* Show verbose information for openstack API request

  To get better understanding about communication with openstack, you can set `--v=6` to see more detailed API requests and responses. It is recommended to set `--concurrent-service-syncs=1` as well.

### Review process
Everyone is encouraged to review code. You don't need to know every detail of the code base. You need to understand only what the code related to the fix does.

As a reviewer, remember that you are providing a service to submitters, not the other way around. As a submitter, remember that everyone is subject to the same rules: even the repo maintainers voting on your patch put their changes through the same code review process.

Read [reviewing changes](https://docs.github.com/en/github/collaborating-with-issues-and-pull-requests/reviewing-changes-in-pull-requests) for how the github review process looks like.

Guidance for the code reviewer:

* We need at least two `/lgtm` from reviewers before `/approve` a PR. However, two `lgtm` is not equal to a `/approve`.
* Only the people in the `approvers` section of the OWNERS file have the `/approve` permission.
* Do not `/approve` in your own PR.
* In certain circumstances we allow `/approve` with only one `/lgtm` such as:
  * Typo fix
  * CI job quick fix
  * Changing document with no more than 3 lines
* If a PR needs further discussion and to avoid unintentional merge, use `/hold` (and remember to `/hold cancel` if all concerns are sorted).

Guidance for the code submitter:

* Follow the PR template by providing sufficient information for the reviewers.
* Make sure all the CI jobs pass before the PR is reviewed
* If the PR is not ready for review, add `[WIP]` in the PR title to save reviewer's time.
* If the job failure is unrelated to the PR, type `/test <job name>` in the comment to re-trigger the job, e.g. `/test cloud-provider-openstack-acceptance-test-lb-octavia`
* If the job keeps failing and you are not sure what to do, contact the repo maintainers for help (either using `/cc` in the PR or in `#provider-openstack` Slack channel).