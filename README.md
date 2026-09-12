# Cloud Provider OpenStack

Thank you for visiting the `Cloud Provider OpenStack` repository!

This repository hosts various plugins relevant to OpenStack and Kubernetes integration.

## Compatibility with Kubernetes

This project follows the Kubernetes release cycle. Each minor version of cloud-provider-openstack is compatible with the corresponding minor version of Kubernetes. For example, cloud-provider-openstack v1.30.x is compatible with Kubernetes v1.30.x.

## Components

### OpenStack Cloud Controller Manager

Implements the Kubernetes cloud controller manager interface for OpenStack, managing nodes, routes, and load balancers.
[Docs](/docs/openstack-cloud-controller-manager/using-openstack-cloud-controller-manager.md/)

### Cinder CSI Plugin

Provides persistent storage for Kubernetes workloads using OpenStack Cinder block storage.

[Docs](/docs/cinder-csi-plugin/using-cinder-csi-plugin.md/)

### Manila CSI Plugin

Provides persistent storage for Kubernetes workloads using OpenStack Manila shared file systems.

[Docs](/docs/manila-csi-plugin/using-manila-csi-plugin.md/)

### Octavia Ingress Controller

Manages Kubernetes Ingress resources using OpenStack Octavia load balancers.

[Docs](/docs/octavia-ingress-controller/using-octavia-ingress-controller.md/)

### Keystone Webhook Authentication Authorization

Enables Kubernetes authentication and authorization using OpenStack Keystone tokens.

[Docs](/docs/keystone-auth/using-keystone-webhook-authenticator-and-authorizer.md/)

### Client Keystone

A kubectl plugin for authenticating to Kubernetes clusters using OpenStack Keystone credentials.

[Docs](/docs/keystone-auth/using-client-keystone-auth.md/)

### Barbican KMS Plugin

Integrates OpenStack Barbican as a KMS provider for encrypting Kubernetes secrets at rest.

[Docs](/docs/barbican-kms-plugin/using-barbican-kms-plugin.md/)

### Magnum Auto Healer

Monitors node health in OpenStack Magnum clusters and automatically repairs or replaces failed nodes.

[Docs](/docs/magnum-auto-healer/using-magnum-auto-healer.md/)

## Contributing

Refer to [Getting Started Guide](/docs/developers-guide.md/) for information on setting up a development environment and contributing.

## Contact

Please join us on [Kubernetes provider-openstack slack channel](https://kubernetes.slack.com/messages/provider-openstack)

Project Co-Leads are listed in [OWNERS](/OWNERS).
