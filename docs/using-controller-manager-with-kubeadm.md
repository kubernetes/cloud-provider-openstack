# Using with kubeadm

Step 1: Edit your /etc/systemd/system/kubelet.service.d/10-kubeadm.conf to add `--cloud-provider=external` to the kubelet arguments
```
Environment="KUBELET_KUBECONFIG_ARGS=--cloud-provider=external --bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --kubeconfig=/etc/kubernetes/kubelet.conf"
```

Step 2: Use the `kubeadm.conf` in manifests directory, edit it as appropriate and use the kubeadm.conf like so.
```
kubeadm init --config kubeadm.conf
```

Then follow the usual steps to bootstrap the other nodes using `kubeadm join`

Step 3: build the container image using the following (`make bootstrap` will download go SDK and glide if needed)
```
GOOS=linux make images
```

Save the image using:
```
docker save openstack/openstack-cloud-controller-manager:v0.1.0 | gzip > openstack-cloud-controller-manager.tgz
```

Copy the tgz over to the nodes and load them up:
```
gzip -d openstack-cloud-controller-manager.tgz
docker load < openstack-cloud-controller-manager.tar
```

Step 4: Create a configmap with the openstack cloud configuration (see `manifests/controller-manager/cloud-config`)
```
kubectl create configmap cloud-config --from-file=/etc/kubernetes/cloud-config -n kube-system
```

Step 5: Deploy controller manager

Option #1 - Using a single pod with the definition in `manifests/controller-manager/openstack-cloud-controller-manager-pod.yaml`
```
kubectl create -f manifests/controller-manager/openstack-cloud-controller-manager-pod.yaml
```
Option #2 - Using a daemonset
```
kubectl create -f manifests/controller-manager/openstack-cloud-controller-manager-ds.yaml
```

Step 6: Monitor using kubectl, for example:

for Option #1:
```
kubectl get pods -n kube-system
kubectl get pods -n kube-system openstack-cloud-controller-manager -o json
kubectl describe pod/openstack-cloud-controller-manager -n kube-system
```

for Option #2:
```
kubectl get ds -n kube-system
kubectl get ds -n kube-system openstack-cloud-controller-manager -o json
kubectl describe ds/openstack-cloud-controller-manager -n kube-system
```

Step 7: TBD - test features
