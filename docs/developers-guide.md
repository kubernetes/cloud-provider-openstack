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
    - [Helm Charts](#helm-charts)

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

### DevStack-based testing environment
You can also use our CI scripts to setup a simple development environment based on DevStack and k3s. To do so you need a fresh VM with Ubuntu 24.04. We've tested this with 4 vCPUs and 16 GB of RAM and that's recommended, but we never tested the lower bound, so feel free to try with less resources.

Once the VM is up make sure your SSH keys allow logging in as `ubuntu` user and from your PC and cloud-provider-openstack directory run:

```
ansible-playbook -v \
  --user ubuntu \
  --inventory <PUBLIC_IP_OF_YOUR_VM>, \
  --ssh-common-args "-o StrictHostKeyChecking=no" \
  tests/playbooks/test-occm-e2e.yaml \
  -e octavia_provider=amphora \
  -e run_e2e=false
```

After it finishes you should be able to access both DevStack and Kubernetes:

```
# SSH to the VM
$ ssh ubuntu@<PUBLIC_IP_OF_YOUR_VM>

# Apparently we install K8s in root
$ sudo su

# Load OpenStack credentials
$ source /home/stack/devstack/openrc admin admin

# List all pods in K8s
$ kubectl get pods -A
NAMESPACE     NAME                                       READY   STATUS    RESTARTS   AGE
kube-system   openstack-cloud-controller-manager-55h4w   1/1     Running   0          56m
kube-system   local-path-provisioner-5d56847996-5fqmn    1/1     Running   0          60m
kube-system   coredns-5c6b6c5476-l5dz4                   1/1     Running   0          60m

# Deploy a simple pod
$ kubectl create deploy test --image quay.io/kuryr/demo:latest
deployment.apps/test created

# Expose it as a LoadBalancer Service
$ kubectl expose deploy test --type LoadBalancer --target-port 8080 --port 80
service/test exposed

# Check if LB got created
$ openstack loadbalancer list
+--------------------------------------+--------------------------------------+----------------------------------+-------------+---------------------+------------------+----------+
| id                                   | name                                 | project_id                       | vip_address | provisioning_status | operating_status | provider |
+--------------------------------------+--------------------------------------+----------------------------------+-------------+---------------------+------------------+----------+
| 9873d6d7-8ff1-4b5e-a8f0-6bb61f4dd58f | kube_service_kubernetes_default_test | deca4a226df049a689992105a65fbb66 | 10.1.0.36   | ACTIVE              | ONLINE           | amphora  |
+--------------------------------------+--------------------------------------+----------------------------------+-------------+---------------------+------------------+----------+

# Get the external IP of the service
$ kubectl get svc test
NAME   TYPE           CLUSTER-IP     EXTERNAL-IP    PORT(S)        AGE
test   LoadBalancer   10.43.71.235   172.24.5.121   80:30427/TCP   41m

# Call the LB
$ curl 172.24.5.121
test-846c6ffb69-w52vq: HELLO! I AM ALIVE!!!
```

## Contribution
Now you should have a kubernetes cluster running in openstack and openstack-cloud-controller-manager is deployed in the cluster. Over time, you may find a bug or have some feature requirements, it's time for contribution!

A contribution can be anything which helps this project improving. In addition to code contribution it can be testing, documentation, requirements gathering, bug reporting and so forth.

See [Contributing Guidelines](../CONTRIBUTING.md) for some general information about contribution.


## Development
If you are ready for code contribution, you need to have development environment.

1. Install [Golang](https://golang.org/doc/install). You can choose the golang version same with the version defined in [go.mod](https://github.com/kubernetes/cloud-provider-openstack/blob/master/go.mod#L3) file.
1. IDE. You can choose any IDE that supports Golang, e.g. Visual Studio Code, GoLand, etc.
1. Install [Docker Engine](https://docs.docker.com/engine/install/) or other tools that could build container images such as [podman](https://podman.io/). After writing the code, the best way to test openstack-cloud-controller-manager is to replace the container image specified in its Deployment/StatefulSet/DaemonSet manifest in the kubernetes cluster. We are using docker in this guide.

### Testing

*cloud-provider-openstack* includes a number of different types of test. As is usual for Kubernetes projects, jobs definitions live in the [test-infra](https://github.com/kubernetes/test-infra/tree/master/config/jobs/kubernetes/cloud-provider-openstack) project. A variety of tests run on each PR, however, you can also run your tests locally.

### Unit tests

Unit tests require a go environment with a suitable go version, as noted previously. Assuming you have this, you can run unit tests using the `unit` Makefile target.

```
make unit
```

### E2E tests

End-to-end or _e2e_ tests are more complex to run as they require a functioning OpenStack cloud and Kubernetes (well, k3s) deployment. Fortunately, you can rely on the infrastructure used for CI to run this on your own machines.

For example, to run the Cinder CSI e2e tests, the CI calls the `tests/ci-csi-cinder-e2e.sh` script. Inspecting this, you'll note that a lot of the commands in here are simply provisioning an instance on GCE, using [Boskos](https://github.com/kubernetes-sigs/boskos) to manage static resources (projects, in this case) if needed. If you have a set of GCE credentials, then in theory you could run this script as-is. However, all you need is a VM with sufficient resources and network connectivity running the correct image (Ubuntu 24.04 cloud image as of writing - check `tests/scripts/create-gce-vm.sh` for the latest info). For example, using OpenStack:

```
openstack server create \
  --image ubuntu2404 \
  --flavor m1.large \
  --key-name <key-name> \
  --network <network> \
  --wait \
  ${USER}-csi-cinder-e2e-tests
openstack server add floating ip <server> <ip-address>
```

Once done, you can run the same Ansible commands seen in `tests/ci-csi-cinder-e2e.sh`:

```
ansible-playbook \
  -v \
  --user ubuntu \
  --inventory 10.0.110.127, \
  --ssh-common-args "-o StrictHostKeyChecking=no" \
  tests/playbooks/test-csi-cinder-e2e.yaml
```

As you can see, this whole area needs improvement to make this a little more approachable for non-CI use cases. This should be enough direction to get started though.

### Build openstack-cloud-controller-manager image
In cloud-provider-openstack repo directory, run:

```
REGISTRY=<your-dockerhub-account> \
VERSION=<image-tag> \
make build-local-image-openstack-cloud-controller-manager
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
make build-local-image-openstack-cloud-controller-manager
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

Now you can change openstack-cloud-controller-manager image in the kubernetes cluster, assuming the openstack-cloud-controller-manager is running as a DaemonSet:

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

### Helm Charts

There are several helm charts maintained in this repository, the charts are placed under `/charts` directory at the top-level of the directory tree. To install the chart, e.g. openstack-cloud-controller-manager:

```shell
helm repo add cpo https://kubernetes.github.io/cloud-provider-openstack
helm repo update
helm install occm cpo/openstack-cloud-controller-manager
```

The chart version should be bumped in the following situations:

* Official release following the new Kubernetes major/minor version, e.g. v1.22.0, v1.23.0, etc. The chart should bump its major or minor version based on the fact that if there are backward incompatible changes committed during the past development cycle.
* cloud-provider-openstack own patch version release for stable branches, especially when backporting bugfix for the charts.
