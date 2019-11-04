# Using with kubeadm

Tested on Ubuntu 18.04.

## Prerequisites

- kubeadm, kubelet and kubectl has been installed.

## Steps

- Create the kubeadm config file. You can find an example at [`manifests/controller-manager/kubeadm.conf`](https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/controller-manager/kubeadm.conf)

- Create the cloud config file `/etc/kubernetes/cloud-config` on each node. You can find an example file in [`manifests/controller-manager/cloud-config`](https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/controller-manager/cloud-config).

    > `/etc/kubernetes/cloud-config` is the default cloud config file path used by controller-manager and kubelet.

- Bootstrap the cluster on the master node.

    ```
    kubeadm init --config kubeadm.conf
    ```

- Boostrap any additional master nodes and worker nodes. You will need to add `KUBELET_EXTRA_ARGS="--cloud-provider=external"` to `/etc/default/kubelet` (`/etc/sysconfig/kubelet` for RPMs) before running `kubeadm join`.

- Create a secret containing the cloud configuration for cloud-controller-manager.

    ```shell
    cp /etc/kubernetes/cloud-config cloud.conf
    kubectl create secret generic -n kube-system cloud-config --from-file=cloud.conf
    ```

- Create RBAC resources and cloud-controller-manager deamonset.

    ```shell
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/cluster/addons/rbac/cloud-controller-manager-roles.yaml
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/cluster/addons/rbac/cloud-controller-manager-role-bindings.yaml
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/controller-manager/openstack-cloud-controller-manager-ds.yaml
    ```

- Install a CNI
    This example uses weavenet. _Note: The example kubeadm configuration is set to use CIDR range of 10.244.0.0/16. So we're specifying env.IPALLOC_RANGE here in addition to the version for weavenet._
    ```
    kubectl apply -f "https://cloud.weave.works/k8s/net?k8s-version=$(kubectl version | base64 | tr -d '\n')&env.IPALLOC_RANGE=10.244.0.0/16"
    ```

- After the cloud-controller-manager deamonset and CNI are up and running, the node taint above will be removed. You can also see some more information in the node label.


# Where to go from here 
Create Persistent Volume Claims using using [Cinder Container Storage Interface](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-cinder-csi-plugin.md)

Encrypt Secrets at rest using the [Barbican KMS plugin](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-barbican-kms-plugin.md)

Expose appliactions using service type [Load Balancer](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/expose-applications-using-loadbalancer-type-service.md)

Route applications at layer 7 using [Octavia Ingress Controller](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-octavia-ingress-controller.md)
