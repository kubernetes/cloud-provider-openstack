# Cloud Provider OpenStack

Thank you for visiting the `Cloud Provider OpenStack` repository!

This Repository hosts various plugins relevant to OpenStack and Kubernetes Integration

* [OpenStack Cloud Controller Manager](/docs/using-openstack-cloud-controller-manager.md/)
* [Octavia Ingress Controller](/docs/using-octavia-ingress-controller.md/)
* [Cinder CSI Plugin](/docs/using-cinder-csi-plugin.md/)
* [Keystone Webhook Authentication Authorization](/docs/using-keystone-webhook-authenticator-and-authorizer.md/)
* [Client Keystone](/docs/using-client-keystone-auth.md/)
* [Manila CSI Plugin](/docs/using-manila-csi-plugin.md/)
* [Barbican KMS Plugin](/docs/using-barbican-kms-plugin.md/)
* [Magnum Auto Healer](/docs/using-magnum-auto-healer.md/)

> NOTE: Cinder Standalone Provisioner, Manila Provisioner and Cinder FlexVolume Driver were removed since release v1.18.0.

> Version 1.17 was the last release of Manila Provisioner, which is unmaintained from now on. Due to dependency issues, we removed the code from master but it is still accessible in the [release-1.17](https://github.com/kubernetes/cloud-provider-openstack/tree/release-1.17) branch. Please consider migrating to Manila CSI Plugin.

## Developing

Please Refer [Getting Started Guide](/docs/getting-started-provider-dev.md/) for setting up development environment.

## Contact

Please join us on [Kubernetes provider-openstack slack channel](https://kubernetes.slack.com/messages/provider-openstack)

Project Co-Leads:
* @lxkong - Lingxian Kong
* @ramineni - Anusha Ramineni
* @chrigl - Christoph Glaubitz

## License

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
