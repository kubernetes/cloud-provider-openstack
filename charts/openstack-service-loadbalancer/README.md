# openstack-service-loadbalancer

Deploys a Kubernetes Service for OpenStack Cloud Controller Manager (OCCM).

## How to install

Install OCCM first

```
helm repo add cpo https://kubernetes.github.io/cloud-provider-openstack
helm repo update
helm install openstack-ccm cpo/openstack-cloud-controller-manager --values openstack-ccm.yaml
```

Install Service Loadbalancer Chart

```
helm install openstack-lb cpo/openstack-service-loadblancer
```
