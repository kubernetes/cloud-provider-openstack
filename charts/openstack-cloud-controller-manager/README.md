# openstack-cloud-controller-manager

Deploys the OpenStack Cloud Controller Manager to your cluster

## How To install

You need to configure an `openstack-ccm.yaml` values file with at least:

- `cloudConfig.global.authUrl` with the Keystone URL
- Authentication
  - with password: `cloudConfig.global.username` and `cloudconfig.global.password`
  - with application credentials: (`cloudConfig.global.applicationCredentialId` or `cloudConfig.global.applicationCredentialName`) and `cloudConfig.global.applicationCredentialSecret`
- Load balancing
  - `cloudConfig.loadbalancer.floatingNetworkId` **or**
  - `cloudConfig.loadbalancer.floatingSubnetId` **or**
  - `cloudConfig.loadbalancer.floatingSubnet`

Then run:

```
helm repo add cpo https://kubernetes.github.io/cloud-provider-openstack
helm repo update
helm install openstack-ccm cpo/openstack-cloud-controller-manager --values openstac-ccm.yaml
```

## Unsupported configurations

- The chart does not support the use of a custom `clouds.yaml` file. Therefore, the following config values can’t be set for the `[Global]` section:
  - `use-clouds`
  - `clouds-file`
  - `cloud`
- The chart currently does not support the specification of custom `LoadBalancerClass`es.
