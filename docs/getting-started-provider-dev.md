# Getting Started with Cloud Provider OpenStack Development

This guide will help you get started with building a development environment for you 
to build and run a single node Kubernetes cluster with the OpenStack Cloud Provider
enabled.

## Contents

* [Contents](#contents)
 * [Prerequisites](#prerequisites)
   + [OpenStack Cloud](#openstack-cloud)
   + [Docker](#docker)
   + [Development tools](#development-tools)
 * [Development](#development)
   + [Getting and Building Cloud Provider OpenStack](#getting-and-building-cloud-provider-openstack)
   + [Getting and Building Kubernetes](#getting-and-building-kubernetes)
   + [Running the Cloud Provider in MiniKube](#running-the-cloud-provider-in-minikube)

## Prerequisites

To get started, you will need to set up your development environment.

### OpenStack Cloud
You will need access to an OpenStack cloud, either public or private. You can sign up
for a public OpenStack Cloud through the [OpenStack Passport](https://www.openstack.org/passport)
program, or you can install a small private development cloud with
[DevStack](https://docs.openstack.org/devstack/latest/) or
[Getting Started With OpenStack](https://github.com/hogepodge/getting-started-with-openstack).

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

Your cloud instance will need to have Docker installed. If you're ok with working from the latest
release, it's simple enough to call the Get Docker script:

```
curl -sSL https://get.docker.io | bash
```

If you don't want to pipe a random script from the internet into your environment, you can
install the latest version of Docker with this script.

```
sudo yum update -y
sudo yum install -y -q epel-release yum-utils
sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
sudo yum-config-manager --enable docker-ce-edge
sudo yum install -y -q docker-ce
```

However, the Kubernetes community still recommends that you run Docker v1.12. To install that version
by hand you can use the following script.

```
sudo yum -y update
sudo yum -y -q install yum-utils
sudo yum-config-manager --add-repo https://yum.dockerproject.org/repo/main/centos/7
sudo yum -y --nogpgcheck install docker-engine-1.12.6-1.el7.centos.x86_64
```

You'll want to set up Docker to use the same cgroup driver as Kubernetes

```
sed -i '/^ExecStart=\/usr\/bin\/dockerd$/ s/$/ --exec-opt native.cgroupdriver=systemd/' \
       /usr/lib/systemd/system/docker.service
```

You may want to configure your environment to allow you to control Docker without sudo:

```
user="$(id -un 2\>/dev/null || true)"
sudo usermod -aG docker centos
```

Regardless of how you install, enable start the service:

```
sudo systemctl daemon-reload
sudo systemctl enable docker
sudo systemctl start docker
```

### Development tools

You're going to need a few basic development tools and applications to get, build, and run
the source code. With your package manager you can install `git`, `gcc` and `etcd`.

```
sudo yum install -y -q git gcc etcd

```

You will also need a recent version of Go and set your environment variables.

```
GO_VERSION=1.11
GO_ARCH=linux-amd64
curl -o go.tgz https://dl.google.com/go/go${GO_VERSION}.${GO_ARCH}.tar.gz
sudo tar -C /usr/local/ -xvzf go.tgz
export GOROOT=/usr/local/go
export GOPATH=$HOME/go
```

Install go dependency management tool dep

```
curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh

```

Finally, set up your Git identity and GitHub integrations.

More comprehensive setup instructions are available in the 
[Development Guide](https://github.com/kubernetes/community/blob/master/contributors/devel/development.md#building-kubernetes-on-a-local-osshell-environment)
in the Kubernetes repository. When in doubt, check there for additional setup
and versioning information.

## Development

### Getting and Building Cloud Provider OpenStack

Following the [GitHub Workflow](https://github.com/kubernetes/community/blob/master/contributors/guide/github-workflow.md)
guidelines for Kubernetes development, set up your environment and get the latest development repository. Begin
by forking both the Kubernetes and Cloud-Provider-OpenStack projects into your GitHub into your local
workspace (or bringing your current fork up to date with the current state of both repositories).

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

If you want to build the provider:

```
make
```

If you want to run unit tests:

```
make test
```

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
export HOSTNAME_OVERRIDE=$(hostname)

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
./cluster/kubectl.sh create -f cluster/addons/rbac/
```

Have a good time with OpenStack and Kubernetes!
