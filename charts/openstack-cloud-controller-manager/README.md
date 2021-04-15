# openstack-cloud-controller-manager

Deploys the OpenStack Cloud Controller Manager to your cluster.

Default configuration values are the same as the CCM itself.

## How To install

You need to configure an `openstack-ccm.yaml` values file with at least:

- `cloudConfig.global.auth-url` with the Keystone URL
- Authentication
  - with password: `cloudConfig.global.username` and `cloudconfig.global.password`
  - with application credentials: (`cloudConfig.global.application-credential-id` or `cloudConfig.global.application-credential-name`) and `cloudConfig.global.application-credential-secret`
- Load balancing
  - `cloudConfig.loadbalancer.floating-network-id` **or**
  - `cloudConfig.loadbalancer.floating-subnet-id` **or**
  - `cloudConfig.loadbalancer.floating-subnet`

If you want to enable health checks for your Load Balancers (optional), set `cloudConfig.loadbalancer.create-monitor: true`.

Then run:

```
helm repo add cpo https://kubernetes.github.io/cloud-provider-openstack
helm repo update
helm install openstack-ccm cpo/openstack-cloud-controller-manager --values openstack-ccm.yaml
```

## Unsupported configurations

- The chart does not support the mounting of custom `clouds.yaml` files. Therefore, the following config values in the `[Global]` section won’t have any effect:
  - `use-clouds`
  - `clouds-file`
  - `cloud`
