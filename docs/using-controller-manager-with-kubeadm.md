# Using with kubeadm

## Prerequisites

- kubeadm, kubelet and kubectl has been installed.
- This guide only deploys cloud-controller-manager, no cinder-provisioner, no cinder-csi-plugin.

## Steps

- Config kubelet arguments on each node.

    Edit `/etc/systemd/system/kubelet.service.d/10-kubeadm.conf` to add `--cloud-provider=external` to the kubelet arguments, e.g.

    ```
    Environment="KUBELET_KUBECONFIG_ARGS=--cloud-provider=external --bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --kubeconfig=/etc/kubernetes/kubelet.conf"
    ```

    Restart kubelet service after the modification.

- Create the cloud config file `/etc/kubernetes/cloud-config` on each node. You can find an example file in `manifests/controller-manager/cloud-config`.

    > `/etc/kubernetes/cloud-config` is the default cloud config file path used by controller-manager and kubelet.

- Bootstrap the cluster on the master node.

    ```
    kubeadm init --config kubeadm.conf
    ```

    You can find an example kubeadm.conf in `manifests/controller-manager/kubeadm.conf`. Follow the usual steps to install the network plugin and then bootstrap the other nodes using `kubeadm join`.
     >Note, your nodes may not have an External IP address. This will cause logs and CNI to have have issues until the cluster provider is complete. Reference https://github.com/kubernetes/kubernetes/pull/75229 for further information.

- Allow controller-manager to have access `/etc/kubernetes/cloud-config`.

    Modify `/etc/kubernetes/manifests/kube-controller-manager.yaml` file to mount an extra volume to the controller manager pod.

    ```
    ...
        volumeMounts:
        - mountPath: /etc/kubernetes/cloud-config
          name: cloud-config
          readOnly: true
    ...
      volumes:
      - name: cloud-config
        hostPath:
          path: /etc/kubernetes/cloud-config
          type: FileOrCreate
    ...
    ```

    Then wait for the controller manager to be restarted and running.

- Create a secret containing the cloud configuration for cloud-controller-manager.

   Update `cloud.conf` configuration in `manifests/controller-manager/cloud-config-secret.yaml`:

    ```shell
    kubectl create secret -n kube-system generic cloud-config --from-literal=cloud.conf="$(cat $CLOUD_CONFIG)" --dry-run -o yaml > manifests/controller-manager/cloud-config-secret.yaml
    kubectl -f manifests/controller-manager/cloud-config-secret.yaml apply
    ```

- Before we create cloud-controller-manager deamonset, you can find all the nodes have the taint `node.cloudprovider.kubernetes.io/uninitialized=true:NoSchedule` and waiting for being initialized by cloud-controller-manager.

- Create RBAC resources and cloud-controller-manager deamonset.

    ```shell
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/cluster/addons/rbac/cloud-controller-manager-roles.yaml
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/cluster/addons/rbac/cloud-controller-manager-role-bindings.yaml
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/controller-manager/openstack-cloud-controller-manager-ds.yaml
    ```

- After the cloud-controller-manager deamonset is up and running, the node taint above will be removed by cloud-controller-manager, you can also see some more information in the node label.
