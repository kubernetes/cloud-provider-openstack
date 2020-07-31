<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Getting Started with Cloud Provider OpenStack Development](#getting-started-with-cloud-provider-openstack-development)
  - [Prerequisites](#prerequisites)
    - [OpenStack Cloud](#openstack-cloud)
    - [Docker](#docker)
    - [Development tools](#development-tools)
  - [Development](#development)
    - [Getting and Building Cloud Provider OpenStack](#getting-and-building-cloud-provider-openstack)
      - [Building inside container](#building-inside-container)
    - [Getting and Building Kubernetes](#getting-and-building-kubernetes)
    - [Running the Cloud Provider](#running-the-cloud-provider)
  - [Publish Container images](#publish-container-images)
    - [Build and push images](#build-and-push-images)
    - [Only Build Image](#only-build-image)
  - [Troubleshooting](#troubleshooting)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Getting Started with Cloud Provider OpenStack Development

This guide will help you get started with building a development environment for you
to build and run a single node Kubernetes cluster with the OpenStack Cloud Provider
enabled.

## Prerequisites

To get started, you will need to set up your development environment.

### OpenStack Cloud
You will need access to an OpenStack cloud, either public or private. You can sign up
for a public OpenStack Cloud through the [OpenStack Passport](https://www.openstack.org/passport)
program, or you can install a small private development cloud with
[DevStack](https://docs.openstack.org/devstack/latest/).

Once you have obtained access to an OpenStack cloud, you will need to start a development
VM. The rest of this guide assumes a CentOS 7 cloud image, but should be easily transferrable
to whatever development environment you prefer. You will need to have your cloud credentials
loaded into your environment. For example, I use this `openrc` file:

```
export OS_PROJECT_DOMAIN_NAME=Default
export OS_USER_DOMAIN_NAME=Default
export OS_DOMAIN_ID=<domain_id_that_matches_name>
export OS_PROJECT_NAME=<project_name>
export OS_TENANT_NAME=<project_name>
export OS_TENANT_ID=<project_id_that_matches_name>
export OS_USERNAME=<username>
export OS_PASSWORD=<password>
export OS_AUTH_URL=http://<openstack_keystone_endpoint>/v3
export OS_INTERFACE=public
export OS_IDENTITY_API_VERSION=3
export OS_REGION_NAME=<region_name>
```

The specific values you use will vary based on your particular environment. You may
notice that several values are aliases of one another. This is in part because the
values expected by the OpenStack client and
[Gopher Cloud](https://github.com/gophercloud/gophercloud) are slightly different,
especially with respect to the change from using `tenant` to `project`. One of our
development goals is to make this setup easier and more consistent.

### Docker

Your cloud instance will need to have Docker installed. To install, please refer to [Docker Container runtime](https://kubernetes.io/docs/setup/production-environment/container-runtimes/#docker).

### Development tools

You're going to need a few basic development tools and applications to get, build, and run
the source code. With your package manager you can install `git`, `gcc` and `etcd`.

```
sudo yum install -y -q git gcc etcd

```

You will also need a recent version of Go and set your environment variables.

```
GO_VERSION=1.13.4
GO_ARCH=linux-amd64
curl -o go.tgz https://dl.google.com/go/go${GO_VERSION}.${GO_ARCH}.tar.gz
sudo tar -C /usr/local/ -xvzf go.tgz
export GOROOT=/usr/local/go
export GOPATH=$HOME/go
```

Finally, set up your Git identity and GitHub integrations.

More comprehensive setup instructions are available in the
[Development Guide](https://github.com/kubernetes/community/blob/master/contributors/devel/development.md#building-kubernetes-on-a-local-osshell-environment)
in the Kubernetes repository. When in doubt, check there for additional setup
and versioning information.

## Development

### Getting and Building Cloud Provider OpenStack

Following the [GitHub Workflow](https://github.com/kubernetes/community/blob/master/contributors/guide/github-workflow.md) guidelines for Kubernetes development, set up your environment and get the latest development repository. Begin by forking both the Kubernetes and Cloud-Provider-OpenStack projects into your GitHub into your local workspace (or bringing your current fork up to date with the current state of both repositories).

`make` will build, test, and package this project. This project uses [go modules](https://github.com/golang/go/wiki/Modules) for dependency management since v1.17.

Set up some environment variables to help download the repositories

```
export GOPATH=$HOME/go
export GOROOT=/usr/local/go
export PATH=$PATH:$GOROOT/bin
export user={your github profile name}
export working_dir=$GOPATH/src/k8s.io
```

With your environment variables set up, clone the forks into your go environment.

```
mkdir -p $working_dir
cd $working_dir
git clone https://github.com/{user}/cloud-provider-openstack
cd cloud-provider-openstack
```

If you want to build the plugins for single architecture:

```
# ARCH default to amd64
export ARCH=arm64
make build
```

And if you want to build the plugins for multiple architectures:

```
# ARCHS default to 'amd64 arm arm64 ppc64le s390x'
export ARCHS='amd64 arm arm64 ppc64le s390x'
make build-all-archs
```

If you want to run unit tests:

```
make test
```

#### Building inside container

If you don't have a Go Environment setup, we also offer the ability to run make
in a Docker Container.  The only requirement for this is that you have Docker
installed and configured (of course).  You don't need to have a Golang
environment setup, and you don't need to follow rules in terms of directory
structure for the code checkout.

To use this method, just call the `hack/make.sh` script with the desired argument:
    `hack/make.sh build`  for example will run `make build` in a container.

> NOTE: You MUST run the script from the root source directory as shown above,
attempting to do something like `cd hack && make.sh build` will not work
because we won't bind mount the source files into the container.

### Getting and Building Kubernetes

To get and build Kubernetes

```
cd $working_dir
export KUBE_FASTBUILD=true
git clone https://github.com/{user}/kubernetes
cd kubernetes
make cross
```

### Running the Cloud Provider

To run the OpenStack provider, integrated with your cloud, be sure to have sourced the
environment variables. You will also need to create an `/etc/kubernetes/cloud-config` file
with the minimum options:

```
[Global]
username=<username>
password=<password>
auth-url=http://<auth_endpoint>/v3
tenant-id=<project_id>
domain-id=<domain_id>
```

Start your cluster with the `hack/local-up-cluster.sh` with the proper environment variable set to
enable the external cloud provider:

```
export EXTERNAL_CLOUD_PROVIDER_BINARY=$GOPATH/src/k8s.io/cloud-provider-openstack/openstack-cloud-controller-manager
export EXTERNAL_CLOUD_PROVIDER=true
export CLOUD_PROVIDER=openstack
export CLOUD_CONFIG=/etc/kubernetes/cloud-config
./hack/local-up-cluster.sh
```

After giving the cluster time to build and start, you can access it through the directions
provided by the script:

```
export KUBECONFIG=/var/run/kubernetes/admin.kubeconfig
./cluster/kubectl.sh
```

The cluster/addons/rbac has a set of yaml files, currently it has
cloud-controller-manager-role-bindings.yaml
cloud-controller-manager-roles.yaml

you need use following command to create ClusterRole and ClusterRoleBinding
otherwise the cloud-controller-manager is not able to access k8s API.
```
./cluster/kubectl.sh create -f $working_dir/cloud-provider-openstack/cluster/addons/rbac/
```

Have a good time with OpenStack and Kubernetes!

## Publish Container images

We allow build and publish container images for multiple architectures.

### Build and push images

To build and push all supported images for all architecture.

```
# default value of VERSION is "{part of latest commit id}-dirty"
export VERSION=latest
# default registry is "k8scloudprovider"
export REGISTRY=k8scloudprovider
# No need to provide "DOCKER_PASSWORD" and "DOCKER_USERNAME" if environment already logged in.
export DOCKER_PASSWORD=password
export DOCKER_USERNAME=username

# default value for IMAGE_NAMES includes all supported images.
export IMAGE_NAMES='openstack-cloud-controller-manager cinder-flex-volume-driver cinder-provisioner cinder-csi-plugin k8s-keystone-auth octavia-ingress-controller manila-provisioner manila-csi-plugin barbican-kms-plugin magnum-auto-healer'
# default value for ARCHS includes all supported architectures.
export ARCHS='amd64 arm arm64 ppc64le s390x'

make upload-images
```

Here's example to explain `make upload-images`
`upload-images` will first build images with architectures.
For example with `REGISTRY=k8scloudprovider`, `VERSION=latest`, `IMAGE_NAMES=openstack-cloud-controller-manager`,
and `ARCHS=arm64 amd64` will build following two images
- `k8scloudprovider/openstack-cloud-controller-manager-arm64:latest`
- `k8scloudprovider/openstack-cloud-controller-manager-amd64:latest`

Second, it will than upload above two images.
And finally, it will build image manifest to upload image `k8scloudprovider/openstack-cloud-controller-manager:latest` (with no architecture in image name).

Image `k8scloudprovider/openstack-cloud-controller-manager:latest` will support two different `OS/ARCH` tag: `linux/arm64` and `linux/amd64`.

From this point all images are ready in image repositories.
To start using image with specific architecture. You can use `--platform` or without if you plan to use default platform:

```
docker pull --platform=linux/amd64 k8scloudprovider/openstack-cloud-controller-manager:latest
```

Or you can use image with architecture tagged in name:

```
docker pull k8scloudprovider/openstack-cloud-controller-manager-amd64:latest
```

Note that since image `k8scloudprovider/openstack-cloud-controller-manager:latest` is build by manifest,
the image digest for image pull from `k8scloudprovider/openstack-cloud-controller-manager:latest` will be different to
image you pull from `k8scloudprovider/openstack-cloud-controller-manager-amd64:latest`.

### Only Build Image

It's generally more recommended to use `make upload-images` to build and push image.
But you might need to build image and run test on it if you're running a test job to verify that image.

To prepare default environment variables:

```
# default value of VERSION is "{part of latest commit id}-dirty"
export VERSION=latest

# default value for IMAGE_NAMES includes all supported images.
export IMAGE_NAMES='openstack-cloud-controller-manager magnum-auto-healer'
# default value for ARCHS includes all supported architectures.
```

If you not plan to build image, you can directly run `make images-all-archs` to support build for all specified images with all specified architectures.

```
export ARCHS='amd64 arm arm64 ppc64le s390x'
make images-all-archs
```

If you plan to build image for single architecture, you can either specify `ARCHS` as `amd64` or run below commands

```
export ARCH=amd64
make build-images
```

Or if you wish to ony build for specific architecture with specific image

```
export ARCH=amd64
make image-openstack-cloud-controller-manager
```
In case of building images, by default, images built are appended with this -$ARCH tag at the end.
For example, for the above case, openstack-cloud-controller-manager-amd64 is built.
In case needed without it, tag it separately as required.

```
docker image tag openstack-cloud-controller-manager-amd64:latest openstack-cloud-controller-manager:latest
```

This will create image with tag `openstack-cloud-controller-manager:latest` in your local environment.

## Troubleshooting

You can increase a log level verbosity (`-v` parameter) to know better what is happening during the OpenStack Cloud Controller Manager runtime. Setting the log level to **6** allows you to see OpenStack API JSON requests and responses. It is recommended to set a `--concurrent-service-syncs` parameter to **1** for a better output tracking.
