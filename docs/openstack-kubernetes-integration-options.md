## OpenStack and Kubernetes integration options

### In-tree OpenStack provider in Kubernetes repository

Traditional option `--cloud-provider` and `cloud-config` in `kubelet`, `kube-apiserver` and `kube-controller-manager`

### Cloud Controller Manager (CCM) in Kubernetes repository

Temporary stop gap binary `cloud-controller-manager` that has both `--cloud-provider` and `--cloud-config`
command line params. Need to set `--cloud-provider=external` for the other kubernetes binaries.

Also use the `--external-cloud-volume-plugin` command line parameter in `kube-controller-manager` to use the
in-tree cinder volume plugin. Note that the provisioner name for the in-tree volume plugin is `kubernetes.io/cinder`

### External OpenStack provider

Mostly the same code as CCM, but code moved out of the main kubernetes repository. `--cloud-provider` is hard coded
to `openstack`. `--cloud-config` needs to be specified.

Similar to CCM, you can use the `--external-cloud-volume-plugin` in `kube-controller-manager` until support for that
flag is dropped.

Scenarios tested:
- External LBaaS with Neutron LBaaSv2
- Internal LBaaS with Neutron LBaaSv2
- LVM / iSCSI with Cinder
- Ceph / RBD with Cinder

TODO:
- Test LBaaS scenarios with Octavia

### Kubernetes Keystone Webhooks

There are two scenarios, authentication and authorization. They can be configured/used independently. There is
support in the kubectl CLI for OpenStack auth provider. This provider can pick up the usual OS_* env vars and
use them to talk to kube api server. However you need the auth webhook to authenticate the tokens.

The authorization is a WIP. the initial thought was to provide a way similar to OpenStack Keystone policy files
to do some authorization checks. You can just use the kubernetes builtin RBAC support.

### Auth data synchronization

An additional feature of the authentication webhook is the ability to
[synchronize data](./using-auth-data-synchronization.md) between Keystone and Kubernetes.

For example, if a user belongs to some project in Keystone, then when attempting to authenticate in Kubernetes
using k8s-keystone-auth, a kubernetes namespace with the same name as the project id will be automatically created
for him along with his role assignments in the project.

To enable the feature two new arguments were added to the k8s-keystone-auth binary:

- ``--sync-config-file`` - points to a local file with sync configuration.
- ``--sync-configmap-name`` - defines a configmap containing the configuration.
  The ConfigMap must have ``syncConfig`` key containing config data.

### Cinder Standalone provisioner

Tested with `LVM / iSCSI` and `Ceph / RBD` scenarios. The provisioner name is `openstack.org/standalone-cinder`.
You can use this along with the External OpenStack provider or CCM.

### Cinder Flex volume driver

WIP - There is some code, needs to be tested

### Cinder CSI driver

WIP - There is some example code in a SIG-storage repo. Need to investigate