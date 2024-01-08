<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Get started with external openstack-cloud-controller-manager in Kubernetes](#get-started-with-external-openstack-cloud-controller-manager-in-kubernetes)
  - [Deploy a Kubernetes cluster with openstack-cloud-controller-manager using kubeadm](#deploy-a-kubernetes-cluster-with-openstack-cloud-controller-manager-using-kubeadm)
    - [Prerequisites](#prerequisites)
    - [Steps](#steps)
  - [Migrating from in-tree openstack cloud provider to external openstack-cloud-controller-manager](#migrating-from-in-tree-openstack-cloud-provider-to-external-openstack-cloud-controller-manager)
  - [Config openstack-cloud-controller-manager](#config-openstack-cloud-controller-manager)
    - [Global](#global)
    - [Networking](#networking)
    - [Load Balancer](#load-balancer)
    - [Metadata](#metadata)
  - [Exposing applications using services of LoadBalancer type](#exposing-applications-using-services-of-loadbalancer-type)
  - [Metrics](#metrics)
  - [Limitation](#limitation)
    - [OpenStack availability zone must not contain blank](#openstack-availability-zone-must-not-contain-blank)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Get started with external openstack-cloud-controller-manager in Kubernetes

External cloud providers were introduced as an Alpha feature in Kubernetes release 1.6. openstack-cloud-controller-manager is the implementation of external cloud provider for OpenStack clusters. An external cloud provider is a kubernetes controller that runs cloud provider-specific loops required for the functioning of kubernetes. These loops were originally a part of the `kube-controller-manager`, but they were tightly coupling the `kube-controller-manager` to cloud-provider specific code. In order to free the kubernetes project of this dependency, the `cloud-controller-manager` was introduced.

`cloud-controller-manager` allows cloud vendors and kubernetes core to evolve independent of each other. In prior releases, the core Kubernetes code was dependent upon cloud provider-specific code for functionality. In future releases, code specific to cloud vendors should be maintained by the cloud vendor themselves, and linked to `cloud-controller-manager` while running Kubernetes.

For more information about cloud-controller-manager, please see:

- <https://github.com/kubernetes/enhancements/tree/master/keps/sig-cloud-provider/2392-cloud-controller-manager>
- <https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller/#running-cloud-controller-manager>
- <https://kubernetes.io/docs/tasks/administer-cluster/developing-cloud-controller-manager/>

**NOTE: Now, the openstack-cloud-controller-manager implementation is based on OpenStack Octavia, Neutron-LBaaS has been removed in openstack-cloud-controller-manager since v1.26.0. So make sure to use Octavia if upgrade to the latest openstack-cloud-controller-manager docker image.**

## Deploy a Kubernetes cluster with openstack-cloud-controller-manager using kubeadm

The following guide has been tested to install Kubernetes v1.17 on Ubuntu 18.04.

### Prerequisites

- docker, kubeadm, kubelet and kubectl has been installed.

### Steps

- Create the kubeadm config file according to [`manifests/controller-manager/kubeadm.conf`](../../manifests/controller-manager/kubeadm.conf)

- Bootstrap the cluster, make sure to install the [CNI network plugin](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/create-cluster-kubeadm/#pod-network) as well.

    ```
    kubeadm init --config kubeadm.conf
    ```

- Bootstrap worker nodes. You need to set `--cloud-provider=external` for kubelet service before running `kubeadm join`.

- Create a secret containing the cloud configuration. You can find an example config file in [`manifests/controller-manager/cloud-config`](../../manifests/controller-manager/cloud-config). If you have certs you need put the cert file into folder `/etc/ssl/certs/` and update `ca-file` in the configuration file, refer to `ca-file` option [here](./using-openstack-cloud-controller-manager.md#global) for further information. After that, Save the configuration to a file named *cloud.conf*, then:

    ```shell
    kubectl create secret -n kube-system generic cloud-config --from-file=cloud.conf
    ```

- Create RBAC resources and openstack-cloud-controller-manager daemonset.

    ```shell
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/controller-manager/cloud-controller-manager-roles.yaml
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/controller-manager/cloud-controller-manager-role-bindings.yaml
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/controller-manager/openstack-cloud-controller-manager-ds.yaml
    ```

- Waiting for all the pods in kube-system namespace up and running.

## Migrating from in-tree openstack cloud provider to external openstack-cloud-controller-manager

If you are already running a Kubernetes cluster (installed by kubeadm) but using in-tree openstack cloud provider, switching to openstack-cloud-controller-manager is easy by following the steps in the demo below.

[![asciicast](https://asciinema.org/a/303399.svg)](https://asciinema.org/a/303399?speed=2)

Also, checkout the guide on [Migrate to CCM](./migrate-to-ccm-with-csimigration.md)

## Config openstack-cloud-controller-manager

Implementation of openstack-cloud-controller-manager relies on several OpenStack services.

| Service                        | API Version(s) | Deprecated | Required |
|--------------------------------|----------------|------------|----------|
| Identity (Keystone)            | v3             | No         | Yes      |
| Compute (Nova)                 | v2             | No         | Yes      |
| Load Balancing (Octavia)       | v2             | No         | Yes      |
| Key Manager (Barbican)         | v1             | No         | No       |

NOTE:

* Block Storage is not needed for openstack-cloud-controller-manager in favor of [cinder-csi-plugin](../cinder-csi-plugin/using-cinder-csi-plugin.md).
* Barbican is required to support creating Service of LoadBalancer type with TLS termination.

### Global

The options in `Global` section are used for openstack-cloud-controller-manager authentication with OpenStack Keystone, they are similar to the global options when using `openstack` CLI, see more information in [openstack man page](https://docs.openstack.org/python-openstackclient/latest/cli/man/openstack.html).

* `auth-url`
  Required. Keystone service URL, e.g. http://128.110.154.166/identity
* `os-endpoint-type`
  Optional. Specify which type of endpoint to use from the service catalog.
  If not set, public endpoints are used.
* `ca-file`
  Optional. CA certificate bundle file for communication with Keystone service, this is required when using the https protocol in the Keystone service URL.
* `cert-file`
  Optional. Client certificate path used for the client TLS authentication.
* `key-file`
  Optional. Client private key path used for the client TLS authentication.
* `username`
  Keystone user name. If you are using [Keystone application credential](https://docs.openstack.org/keystone/latest/user/application_credentials.html), this option is not required.
* `password`
  Keystone user password. If you are using [Keystone application credential](https://docs.openstack.org/keystone/latest/user/application_credentials.html), this option is not required.
* `region`
  Required. Keystone region name.
* `domain-id`
  Keystone user domain ID. If you are using [Keystone application credential](https://docs.openstack.org/keystone/latest/user/application_credentials.html), this option is not required.
* `domain-name`
  Keystone user domain name, not required if `domain-id` is set.
* `tenant-id`
  Keystone project ID. When using Keystone V3 - which changed the identifier `tenant` to `project` - the `tenant-id` value is automatically mapped to the project construct in the API.

  `tenant-id` is not needed when using `trust-id` or [Keystone application credential](https://docs.openstack.org/keystone/latest/user/application_credentials.html)
* `tenant-name`
  Keystone project name, not required if `tenant-id` is set.
* `tenant-domain-id`
  Keystone project domain ID.
* `tenant-domain-name`
  Keystone project domain name.
* `user-domain-id`
  Keystone user domain ID.
* `user-domain-name`
  Keystone user domain name.
* `trust-id`
  Keystone trust ID. A trust represents a user's (the trustor) authorization to delegate roles to another user (the trustee), and optionally allow the trustee to impersonate the trustor. Available trusts are found under the `/v3/OS-TRUST/trusts` endpoint of the Keystone API.
* `trustee-id`
  Keystone trustee user ID.
* `trustee-password`
  Keystone trustee user password.
* `use-clouds`
  Set this option to `true` to get authorization credentials from a clouds.yaml file. Options explicitly set in this section are prioritized over values read from clouds.yaml, the file path can be set in `clouds-file` option. Otherwise, the following order is applied:
  1. A file path stored in the environment variable `OS_CLIENT_CONFIG_FILE`
  2. The directory `pkg/openstack`
  3. The directory `~/.config/openstack`
  4. The directory `/etc/openstack`
* `clouds-file`
  File path of a clouds.yaml file, used together with `use-clouds=true`.
* `cloud`
  Used to specify which named cloud in the clouds.yaml file that you want to use, used together with `use-clouds=true`.
* `application-credential-id`
  The ID of an application credential to authenticate with. An `application-credential-secret` has to be set along with this parameter.
* `application-credential-name`
  The name of an application credential to authenticate with. If `application-credential-id` is not set, the user name and domain need to be set.
* `application-credential-secret`
  The secret of an application credential to authenticate with.
* `tls-insecure`
  If set to `true`, then the server’s certificate will not be verified. Default is `false`.

###  Networking

* `ipv6-support-disabled`
  Indicates whether or not IPv6 is supported. Default: false
* `public-network-name`
  The name of Neutron external network. openstack-cloud-controller-manager uses this option when getting the external IP of the Kubernetes node. Can be specified multiple times. Specified network names will be ORed. Default: ""
* `internal-network-name`
  The name of Neutron internal network. openstack-cloud-controller-manager uses this option when getting the internal IP of the Kubernetes node, this is useful if the node has multiple interfaces. Can be specified multiple times. Specified network names will be ORed. Default: ""
* `address-sort-order`
  This configuration key influences the way the provider reports the node addresses to the Kubernetes node resource. The default order depends on the hard-coded order the provider queries the addresses and what the cloud returns, which does not guarantee a specific order.

  To override this behavior it is possible to specify a comma separated list of CIDRs. Essentially, this will sort and group all addresses matching a CIDR in a prioritized manner, where the first item having a higher priority than the last. All non-matching addresses will remain in the same order they are already in.

  For example, this option can be useful when having multiple or dual-stack interfaces attached to a node and needing a user-controlled, deterministic way of sorting the addresses.
  Default: ""

### Route

* `router-id`
  Specifies the Neutron router ID to activate [route controller](https://kubernetes.io/docs/concepts/architecture/cloud-controller/#route-controller) to manage Kubernetes cluster routes.

  **NOTE: This require openstack-cloud-controller-manager's `--cluster-cidr` flag to be set.**

###  Load Balancer

Although the openstack-cloud-controller-manager was initially implemented with Neutron-LBaaS support, Octavia is mandatory now because Neutron-LBaaS has been deprecated since Queens OpenStack release cycle and no longer accepted new feature enhancements. As a result, since v1.26.0 the Neutron-LBaaS is not supported in openstack-cloud-controller-manager and removed from code repo.

* `enabled`
  Whether or not to enable the LoadBalancer type of Services integration at all.
   Default: true

* `floating-network-id`
  Optional. The external network used to create floating IP for the load balancer VIP. If there are multiple external networks in the cloud, either this option must be set or user must specify `loadbalancer.openstack.org/floating-network-id` in the Service annotation.

* `floating-subnet-id`
  Optional. The external network subnet used to create floating IP for the load balancer VIP. Can be overridden by the Service annotation `loadbalancer.openstack.org/floating-subnet-id`.

* `floating-subnet`
  Optional. A name pattern (glob or regexp if starting with `~`) for the external network subnet used to create floating IP for the load balancer VIP. Can be overridden by the Service annotation `loadbalancer.openstack.org/floating-subnet`. If multiple subnets match the first one with still available IPs is used.

* `floating-subnet-tags`
  Optional. Tags for the external network subnet used to create floating IP for the load balancer VIP. Can be overridden by the Service annotation `loadbalancer.openstack.org/floating-subnet-tags`. If multiple subnets match the first one with still available IPs is used.

* `lb-method`
  The load balancing algorithm used to create the load balancer pool.

  If `lb-provider` is set to "amphora" or "octavia" the value can be one of:
    * `ROUND_ROBIN` (default)
    * `LEAST_CONNECTIONS`
    * `SOURCE_IP`
    
  If `lb-provider` is set to "ovn" the value must be set to `SOURCE_IP_PORT`.

* `lb-provider`
  Optional. Used to specify the provider of the load balancer, e.g. "amphora" (default), "octavia" (deprecated alias for "amphora"), or "ovn". Only the "amphora", "octavia", and "ovn" providers are officially tested, other providers will cause a warning log.

* `lb-version`
  Optional. If specified, only "v2" is supported.

* `subnet-id`
  ID of the Neutron subnet on which to create load balancer VIP. This ID is also used to create pool members, if `member-subnet-id` is not set.

* `member-subnet-id`
  ID of the Neutron network on which to create the members of the load balancer. The load balancer gets another network port on this subnet. Defaults to `subnet-id` if not set.

* `network-id`
  ID of the Neutron network on which to create load balancer VIP, not needed if `subnet-id` is set.

* `manage-security-groups`
  If the Neutron security groups should be managed separately. Default: false

* `create-monitor`
  Indicates whether or not to create a health monitor for the service load balancer. A health monitor required for services that declare `externalTrafficPolicy: Local`. Default: false

  NOTE: Health monitors for the `ovn` provider are only supported on OpenStack Wallaby and later.

* `monitor-delay`
  The time, in seconds, between sending probes to members of the load balancer. Default: 5

* `monitor-max-retries`
  The number of successful checks before changing the operating status of the load balancer member to ONLINE. A valid value is from 1 to 10. Default: 1

* `monitor-max-retries-down`
  The number of unsuccessful checks before changing the operating status of the load balancer member to ERROR. A valid value is from 1 to 10. Default: 3

* `monitor-timeout`
  The maximum time, in seconds, that a monitor waits to connect backend before it times out. Default: 3

* `internal-lb`
  Determines whether or not to create an internal load balancer (no floating IP) by default. Default: false.

* `cascade-delete`
  Determines whether or not to perform cascade deletion of load balancers. Default: true.

* `flavor-id`
  The id of the loadbalancer flavor to use. Uses octavia default if not set.

* `availability-zone`
  The name of the loadbalancer availability zone to use. The Octavia availability zone capabilities will not be used if it is not set. The parameter will be ignored if the Octavia version doesn't support availability zones yet.

* `LoadBalancerClass "ClassName"`
  This is a config section including a set of config options. User can choose the `ClassName` by specifying the Service annotation `loadbalancer.openstack.org/class`. The following options are supported:

  * floating-network-id. The same with `floating-network-id` option above.
  * floating-subnet-id. The same with `floating-subnet-id` option above.
  * floating-subnet. The same with `floating-subnet` option above.
  * floating-subnet-tags. The same with `floating-subnet-tags` option above.
  * network-id. The same with `network-id` option above.
  * subnet-id. The same with `subnet-id` option above.
  * member-subnet-id. The same with `member-subnet-id` option above.

* `enable-ingress-hostname`

  Used with proxy protocol (set by annotation `loadbalancer.openstack.org/proxy-protocol: "true"`) by adding a dns suffix (nip.io) to the load balancer IP address. Default false.

  This option is currently a workaround for the issue https://github.com/kubernetes/ingress-nginx/issues/3996, should be removed or refactored after the Kubernetes [KEP-1860](https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/1860-kube-proxy-IP-node-binding) is implemented.

* `ingress-hostname-suffix`

  The dns suffix to the load balancer IP address when using proxy protocol. Default nip.io

  This option is currently a workaround for the issue https://github.com/kubernetes/ingress-nginx/issues/3996, should be removed or refactored after the Kubernetes [KEP-1860](https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/1860-kube-proxy-IP-node-binding) is implemented.

* `default-tls-container-ref`
  Reference to a tls container or secret. This option works with Octavia, when this option is set then the cloud provider will create an Octavia Listener of type TERMINATED_HTTPS for a TLS Terminated loadbalancer.

  Accepted format for tls container ref are `https://{keymanager_host}/v1/containers/{uuid}` and `https://{keymanager_host}/v1/secrets/{uuid}`.
  Check `container-store` parameter if you want to disable validation.

* `container-store`
  Optional. Used to specify the store of the tls-container-ref, e.g. "barbican" or "external" - other store will cause a warning log.
  Default value - `barbican` - existence of tls container ref would always be performed.

  If set to `external` format for tls container ref will not be validated.

* `max-shared-lb`
  The maximum number of Services that share a load balancer. Default: 2

* `provider-requires-serial-api-calls`
  Some Octavia providers do not support creating fully-populated loadbalancers using a single [API
  call](https://docs.openstack.org/api-ref/load-balancer/v2/?expanded=create-a-load-balancer-detail#creating-a-fully-populated-load-balancer).
  Setting this option to true will create loadbalancers using serial API calls which first create an unpopulated
  loadbalancer, then populate its listeners, pools and members. This is a compatibility option at the expense of
  increased load on the OpenStack API. Default: false 

NOTE:

* environment variable `OCCM_WAIT_LB_ACTIVE_STEPS` is used to provide steps of waiting loadbalancer to be ready. Current default wait steps is 23 and setup the environment variable overrides default value. Refer to [Backoff.Steps](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/wait#Backoff) for further information.

### Metadata

* `search-order`
  This configuration key influences the way that the provider retrieves metadata relating to the instance(s) in which it runs. The default value of `configDrive,metadataService` results in the provider retrieving metadata relating to the instance from the config drive first if available and then the metadata service. Alternative values are:
  * `configDrive` - Only retrieve instance metadata from the configuration drive.
  * `metadataService` - Only retrieve instance metadata from the metadata service.
  * `metadataService,configDrive` - Retrieve instance metadata from the metadata service first if available, then the configuration drive.

  Not all OpenStack clouds provide both configuration drive and metadata service though and only one or the other may be available which is why the default is to check both. Especially, the metadata on the config drive may grow stale over time, whereas the metadata service always provides the most up to date data.

### Multi region support (alpha)

* environment variable `OS_CCM_REGIONAL` is set to `true` - allow CCM to set ProviderID with region name `${ProviderName}://${REGION}/${instance-id}`. Default: false.

## Exposing applications using services of LoadBalancer type

Refer to [Exposing applications using services of LoadBalancer type](./expose-applications-using-loadbalancer-type-service.md)

## Metrics

Refer to [Metrics for openstack-cloud-controller-manager](../metrics.md)

## Limitation

### OpenStack availability zone must not contain blank

`topology.kubernetes.io/zone` is used to label node and its value comes from availability zone of the node, according to [label spec](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set) it does not support blank (' ') but OpenStack availability zone supports blank. So your OpenStack availability zone must not contain blank otherwise it will lead to node that belongs to this availability zone register failure, see [#1379](https://github.com/kubernetes/cloud-provider-openstack/issues/1379) for further information.
