The ansible role builds and uploads cinder-csi-plugin image and deploys in a k8s cluster.

Prerequisites:

* The playbook is running on a host with devstack installed.
* golang, docker and kubectl should be installed.
* docker registry is up and running.
* GOPATH should be configured in {{ global_env }}
* KUBECONFIG should be configured in {{ global_env }}
* k8s cluster is running inside VMs on the devstack host.
