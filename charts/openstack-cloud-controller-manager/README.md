# openstack-cloud-controller-manager

Helm Chart for OpenStack Cloud Controller Manager

## Unsupported configurations

* The chart does not support the use of a custom `clouds.yaml` file. Therefore, the following config values can’t be set for the `[Global]` section:
  * `use-clouds`
  * `clouds-file`
  * `cloud`
* The chart currently does not support the specification of custom `LoadBalancerClass`es.
