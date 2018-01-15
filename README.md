# OpenStack Cloud Controller Manager

Thank you for visiting the `openstack-cloud-controller-manager` repository!

OpenStack Cloud Controller Manager - An external cloud controller manager for running kubernetes 
in a OpenStack cluster.

# Introduction

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
make build-image
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

Step 4: Create a configmap with the openstack cloud configuration (see `manifests/cloud-config`)
```
kubectl create configmap cloud-config --from-file=/etc/kubernetes/cloud-config -n kube-system
```

Step 5: Deploy controller manager

Option #1 - Using a single pod with the definition in `manifests/openstack-cloud-controller-manager-pod.yaml`
```
kubectl create -f manifests/openstack-cloud-controller-manager-pod.yaml
```
Option #2 - Using a daemonset
```
kubectl create -f manifests/openstack-cloud-controller-manager-ds.yaml
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

# Developing

`make` will build, test, and package this project. This project uses trash Glide for dependency management. 

# License
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
