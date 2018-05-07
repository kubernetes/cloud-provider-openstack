---
title: OpenStack cloud provider options
---

# OpenStack
This section describes all the possible configurations which can
be used when using OpenStack with Kubernetes. The OpenStack cloud provider
implementation for Kubernetes supports the use of these OpenStack services from
the underlying cloud, where available:

| Service                  | API Version(s) | Required |
|--------------------------|----------------|----------|
| Block Storage (Cinder)   | V1†, V2, V3    | No       |
| Compute (Nova)           | V2             | No       |
| Identity (Keystone)      | V2‡,  V3       | Yes      |
| Load Balancing (Neutron) | V1§, V2        | No       |
| Load Balancing (Octavia) | V2             | No       |

† Block Storage V1 API support is deprecated, Block Storage V3 API support was
added in Kubernetes 1.9.

‡ Identity V2 API support is deprecated and will be removed from the provider in
a future release. As of the "Queens" release, OpenStack will no longer expose the
Identity V2 API.

§ Load Balancing V1 API support was removed in Kubernetes 1.9.

Service discovery is achieved by listing the service catalog managed by
OpenStack Identity (Keystone) using the `auth-url` provided in the provider
configuration. The provider will gracefully degrade in functionality when
OpenStack services other than Keystone are not available and simply disclaim
support for impacted features. Certain features are also enabled or disabled
based on the list of extensions published by Neutron in the underlying cloud.

## cloud.conf
Kubernetes knows how to interact with OpenStack via the file cloud.conf. It is
the file that will provide Kubernetes with credentials and location for the OpenStack auth endpoint.
You can create a cloud.conf file by specifying the following details in it

### Typical configuration
This is an example of a typical configuration that touches the values that most
often need to be set. It points the provider at the OpenStack cloud's Keystone
endpoint, provides details for how to authenticate with it, and configures the
load balancer:

```yaml
[Global]
username=user
password=pass
auth-url=https://<keystone_ip>/identity/v3
tenant-id=c869168a828847f39f7f06edd7305637
domain-id=2a73b8f597c04551a0fdc8e95544be8a

[LoadBalancer]
subnet-id=6937f8fa-858d-4bc9-a3a5-18d2c957166a
```

#### Global
These configuration options for the OpenStack provider pertain to its global
configuration and should appear in the `[Global]` section of the `cloud.conf`
file:

* `auth-url` (Required): The URL of the keystone API used to authenticate. On
  OpenStack control panels, this can be found at Access and Security > API
  Access > Credentials.
* `username` (Required): Refers to the username of a valid user set in keystone.
* `password` (Required): Refers to the password of a valid user set in keystone.
* `tenant-id` (Required): Used to specify the id of the project where you want
  to create your resources.
* `tenant-name` (Optional): Used to specify the name of the project where you
  want to create your resources.
* `trust-id` (Optional): Used to specify the identifier of the trust to use for
  authorization. A trust represents a user's (the trustor) authorization to
  delegate roles to another user (the trustee), and optionally allow the trustee
  to impersonate the trustor. Available trusts are found under the
  `/v3/OS-TRUST/trusts` endpoint of the Keystone API.
* `domain-id` (Optional): Used to specify the id of the domain your user belongs
  to.
* `domain-name` (Optional): Used to specify the name of the domain your user
  belongs to.
* `region` (Optional): Used to specify the identifier of the region to use when
  running on a multi-region OpenStack cloud. A region is a general division of
  an OpenStack deployment. Although a region does not have a strict geographical
  connotation, a deployment can use a geographical name for a region identifier
  such as `us-east`. Available regions are found under the `/v3/regions`
  endpoint of the Keystone API.
* `ca-file` (Optional): Used to specify the path to your custom CA file. 


When using Keystone V3 - which changes tenant to project - the `tenant-id` value
is automatically mapped to the project construct in the API.

####  Load Balancer
These configuration options for the OpenStack provider pertain to the load
balancer and should appear in the `[LoadBalancer]` section of the `cloud.conf`
file:

* `lb-version` (Optional): Used to override automatic version detection. Valid
  values are `v1` or `v2`. Where no value is provided automatic detection will
  select the highest supported version exposed by the underlying OpenStack
  cloud.
* `use-octavia` (Optional): Used to determine whether to look for and use an
  Octavia LBaaS V2 service catalog endpoint. Valid values are `true` or `false`.
  Where `true` is specified and an Octavia LBaaS V2 entry can not be found, the
  provider will fall back and attempt to find a Neutron LBaaS V2 endpoint
  instead. The default value is `false`.
* `subnet-id` (Optional): Used to specify the id of the subnet you want to
  create your loadbalancer on. Can be found at Network > Networks. Click on the
  respective network to get its subnets.
* `floating-network-id` (Optional): If specified, will create a floating IP for
  the load balancer.
* `lb-method` (Optional): Used to specify algorithm by which load will be
  distributed amongst members of the load balancer pool. The value can be
  `ROUND_ROBIN`, `LEAST_CONNECTIONS`, or `SOURCE_IP`. The default behavior if
  none is specified is `ROUND_ROBIN`.
* `lb-provider` (Optional): Used to specify the provider of the load balancer.
  If not specified, the default provider service configured in neutron will be
  used.
* `create-monitor` (Optional): Indicates whether or not to create a health
  monitor for the Neutron load balancer. Valid values are `true` and `false`.
  The default is `false`. When `true` is specified then `monitor-delay`,
  `monitor-timeout`, and `monitor-max-retries` must also be set.
* `monitor-delay` (Optional): The time, in seconds, between sending probes to
  members of the load balancer.
* `monitor-timeout` (Optional): Maximum number of seconds for a monitor to wait
  for a ping reply before it times out. The value must be less than the delay
  value.
* `monitor-max-retries` (Optional): Number of permissible ping failures before
  changing the load balancer member's status to INACTIVE. Must be a number
  between 1 and 10.
* `manage-security-groups` (Optional): Determines whether or not the load
  balancer should automatically manage the security group rules. Valid values
  are `true` and `false`. The default is `false`. When `true` is specified
  `node-security-group` must also be supplied.
* `node-security-group` (Optional): ID of the security group to manage.

#### Block Storage
These configuration options for the OpenStack provider pertain to block storage
and should appear in the `[BlockStorage]` section of the `cloud.conf` file:

* `bs-version` (Optional): Used to override automatic version detection. Valid
  values are `v1`, `v2`, `v3` and `auto`. When `auto` is specified automatic
  detection will select the highest supported version exposed by the underlying
  OpenStack cloud. The default value if none is provided is `auto`.
* `trust-device-path` (Optional): In most scenarios the block device names
  provided by Cinder (e.g. `/dev/vda`) can not be trusted. This boolean toggles
  this behavior. Setting it to `true` results in trusting the block device names
  provided by Cinder. The default value of `false` results in the discovery of
  the device path based on its serial number and `/dev/disk/by-id` mapping and is
  the recommended approach.
* `ignore-volume-az` (Optional): Used to influence availability zone use when
  attaching Cinder volumes. When Nova and Cinder have different availability
  zones, this should be set to `true`. This is most commonly the case where
  there are many Nova availability zones but only one Cinder availability zone.
  The default value is `false` to preserve the behavior used in earlier
  releases, but may change in the future.

If deploying Kubernetes versions <= 1.8 on an OpenStack deployment that uses
paths rather than ports to differentiate between endpoints it may be necessary
to explicitly set the `bs-version` parameter. A path based endpoint is of the
form `http://foo.bar/volume` while a port based endpoint is of the form
`http://foo.bar:xxx`.

In environments that use path based endpoints and Kubernetes is using the older
auto-detection logic a `BS API version autodetection failed.` error will be
returned on attempting volume detachment. To workaround this issue it is
possible to force the use of Cinder API version 2 by adding this to the cloud
provider configuration:

```yaml
[BlockStorage]
bs-version=v2
```

#### Metadata
These configuration options for the OpenStack provider pertain to metadata and
should appear in the `[Metadata]` section of the `cloud.conf` file:

* `search-order` (Optional): This configuration key influences the way that the
  provider retrieves metadata relating to the instance(s) in which it runs. The
  default value of `configDrive,metadataService` results in the provider
  retrieving metadata relating to the instance from the config drive first if
  available and then the metadata service. Alternative values are:
  * `configDrive` - Only retrieve instance metadata from the configuration
    drive.
  * `metadataService` - Only retrieve instance metadata from the metadata
    service.
  * `metadataService,configDrive` - Retrieve instance metadata from the metadata
    service first if available, then the configuration drive.

  Influencing this behavior may be desirable as the metadata on the
  configuration drive may grow stale over time, whereas the metadata service
  always provides the most up to date view. Not all OpenStack clouds provide
  both configuration drive and metadata service though and only one or the other
  may be available which is why the default is to check both.

#### Router

These configuration options for the OpenStack provider pertain to the [kubenet]
Kubernetes network plugin and should appear in the `[Router]` section of the
`cloud.conf` file:

* `router-id` (Optional): If the underlying cloud's Neutron deployment supports
  the `extraroutes` extension then use `router-id` to specify a router to add
  routes to.  The router chosen must span the private networks containing your
  cluster nodes (typically there is only one node network, and this value should be
  the default router for the node network).  This value is required to use [kubenet]
  on OpenStack.

[kubenet]: https://kubernetes.io/docs/concepts/cluster-administration/network-plugins/#kubenet
