The ansible role builds and uploads cinder-csi-plugin image and deploys in a k8s cluster.

Prerequisites:

* The playbook is running on a host with devstack installed.
* golang, docker and kubectl should be installed.
* docker registry is up and running.
* k8s cluster is running inside VMs on the devstack host.
* `~/.kube/config` exists and is pointing at the k8s cluster
