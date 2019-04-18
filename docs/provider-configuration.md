# OpenStack Cloud Provider Configuration Options

This document describes all of the configuration options available
to the OpenStack Cloud Provider.

## Contents

- [OpenStack Cloud Provider Configuration Options](#openstack-cloud-provider-configuration-options)
  - [Contents](#contents)
  - [Supported Services](#supported-services)
  - [Cloud Configuration File](#cloud-configuration-file)
    - [Sample configuration](#sample-configuration)
    - [Configuration Options](#configuration-options)
      - [Global](#global)
        - [Global Required Parameters](#global-required-parameters)
        - [Global Optional Parameters](#global-optional-parameters)
      - [Networking](#networking)
        - [Networking](#networking-optional-parameters)
      - [Load Balancer](#load-balancer)
        - [Load Balancer Optional Parameters](#load-balancer-optional-parameters)
      - [Block Storage](#block-storage)
        - [Block Storage Optional Parameters](#block-storage-optional-parameters)
        - [Block Storage Notes](#block-storage-notes)
      - [Metadata](#metadata)
        - [Metadata Optional Parameters](#metadata-optional-parameters)
      - [Router](#router)
        - [Router Optional Parameters](#router-optional-parameters)

## Supported Services

The provider supports several OpenStack services:

| Service                  | API Version(s) | Required |
|--------------------------|----------------|----------|
| Identity (Keystone)      | v2, v3 †       | Yes      |
| Compute (Nova)           | v2             | No       |
| Block Storage (Cinder)   | v1, v2, v3‡    | No       |
| Load Balancing (Neutron) | v1§, v2        | No       |
| Load Balancing (Octavia) | v2             | No       |


† Identity v2 API support is deprecated and will be removed from the provider in
a future release. As of the "Queens" release, OpenStack no longer exposes the
Identity v2 API.

‡ Block Storage v1 API support is deprecated, Block Storage v3 API support was
added in Kubernetes 1.9.

§ Load Balancing v1 API support was removed in Kubernetes 1.9.

Service discovery is achieved by listing the service catalog managed by
OpenStack Identity (Keystone) using the `auth-url` provided in the provider
configuration. The provider will gracefully degrade in functionality when
OpenStack services other than Keystone are not available and simply disclaim
support for impacted features. Certain features are also enabled or disabled
based on the list of extensions published by Neutron in the underlying cloud.

## Cloud Configuration File
Kubernetes knows how to interact with OpenStack via configuration file
specified in `CLOUD_CONFIG` environment variable. It is a standard INI file
that provides Kubernetes with an OpenStack cloud endpoint, user authentication
credentials, and additional configuration specific to the host cloud.

### Sample configuration
This is an example of a typical configuration that touches the values that most
often need to be set. It points the provider at the OpenStack cloud's Keystone
endpoint, provides details for how to authenticate with it, and configures the
load balancer:

```yaml
[Global]
username=${OS_USERNAME}
password=${OS_PASSWORD}
auth-url=https://${OS_AUTH_URL}/identity/v3
tenant-id=${OS_TENANT_ID}
domain-id=${OS_DOMAIN_ID}

[LoadBalancer]
subnet-id=${SUBNET_ID}
```

### Configuration Options

The OpenStack Cloud Provider offers a wide range of configuration options for each
service it supports. The currently available configuration sections include:

* [Global](#global)
* [Block Storage](#block-storage)
* [Load Balancer](#load-balancer)
* [Metadata](#metadata)
* [Router](#router)


#### Global

These configuration options for the OpenStack provider pertain to its global
configuration and should appear in the `[Global]` section of the `$CLOUD_CONFIG`
file.

##### Global Required Parameters

* `auth-url`: The URL of the keystone API used to authenticate. On
  OpenStack control panels, this can be found at Access and Security > API
  Access > Credentials.
* `password`: Refers to the password of a valid user set in keystone.
* `tenant-id`: Used to specify the ID of the project where you want
  to create your resources. When using Keystone V3 - which changed the 
  identifier `tenant` to `project` - the `tenant-id` value is automatically
  mapped to the project construct in the API.
* `username`: Refers to the username of a valid user set in keystone.

##### Global Optional Parameters
* `ca-file`: Used to specify the path to your custom CA file. 
* `domain-id`: Used to specify the ID of the domain your user belongs
  to.
* `domain-name`: Used to specify the name of the domain your user
  belongs to.
* `region`: Used to specify the identifier of the region to use when
  running on a multi-region OpenStack cloud. A region is a general division of
  an OpenStack deployment. Although a region does not have a strict geographical
  connotation, a deployment can use a geographical name for a region identifier
  such as `us-east`. Available regions are found under the `/v3/regions`
  endpoint of the Keystone API.
* `tenant-name`: Used to specify the name of the project where you
  want to create your resources.
* `trust-id`: Used to specify the identifier of the trust to use for
  authorization. A trust represents a user's (the trustor) authorization to
  delegate roles to another user (the trustee), and optionally allow the trustee
  to impersonate the trustor. Available trusts are found under the
  `/v3/OS-TRUST/trusts` endpoint of the Keystone API.
* `UseClouds`: Set this flag to `true` to get authorization credentials from a clouds.yaml file. Options manually set in the `[Global]` section of $CLOUD_CONFIG file will be prioritized over values read from clouds.yaml. The recommended usage is to set the option `CloudsFile` with the path to your clouds.yaml file. However, by default a clouds.yaml file will be looked for in the following locations, in order, if it is not set:
    1. A file path stored in the environment variable `OS_CLIENT_CONFIG_FILE`
    2. The directory `pkg/cloudprovider/providers/openstack/`
    3. The directory `~/.config/openstack`
    4. The directory `/etc/openstack`
* `CloudsFile`: Used to specify the path to a clouds.yaml file that you want read authorization data from
* `Cloud`: Used to specify which named cloud in the clouds.yaml file that you want to use


####  Networking

These configuration options for the OpenStack provider pertain to the network
configuration and should appear in the `[Networking]` section of the `$CLOUD_CONFIG`
file.

##### Networking Optional Parameters

* `ipv6-support-disabled`: Indicates whether or not to use ipv6 addresses.
  The default is `false`. When `true` is specified then will ignore any
  ipv6 addresses assigned to the node.
* `public-network-name`: Used to specify external network.
  The default is `public`. Must be a network name, not id.
* `internal-network-name`: Used to override internal network selection.
  Where no value is provided automatic detection will select random node interface
  as internal. This option makes sense and recommended to specify only
  when you have more than one interface attached to kubernetes nodes.
  Must be a network name, not id.

####  Load Balancer

These configuration options for the OpenStack provider pertain to the load
balancer and should appear in the `[LoadBalancer]` section of the `$CLOUD_CONFIG`
file.

##### Load Balancer Optional Parameters

* `create-monitor`: Indicates whether or not to create a health
  monitor for the Neutron load balancer. Valid values are `true` and `false`.
  The default is `false`. When `true` is specified then `monitor-delay`,
  `monitor-timeout`, and `monitor-max-retries` must also be set.
* `floating-network-id`: If specified, will create a floating IP for
  the load balancer.
* `lb-method`: Used to specify algorithm by which load will be
  distributed amongst members of the load balancer pool. The value can be
  `ROUND_ROBIN`, `LEAST_CONNECTIONS`, or `SOURCE_IP`. The default behavior if
  none is specified is `ROUND_ROBIN`.
* `lb-provider`: Used to specify the provider of the load balancer.
  If not specified, the default provider service configured in neutron will be
  used.
* `lb-version`: Used to override automatic version detection. Valid
  values are `v1` or `v2`. Where no value is provided automatic detection will
  select the highest supported version exposed by the underlying OpenStack
  cloud. `v1` support was removed in Kubernetes 1.9.
* `subnet-id`: Used to specify the ID of the subnet you want to
  create your loadbalancer on. Can be found at Network > Networks. Click on the
  respective network to get its subnets.
* `manage-security-groups`: Determines whether or not the load
  balancer should automatically manage the security group rules. Valid values
  are `true` and `false`. The default is `false`. When `true` is specified
  `node-security-group` must also be supplied.
* `monitor-delay`: The time, in seconds, between sending probes to
  members of the load balancer.
* `monitor-max-retries`: Number of permissible ping failures before
  changing the load balancer member's status to INACTIVE. Must be a number
  between 1 and 10.
* `monitor-timeout`: Maximum number of seconds for a monitor to wait
  for a ping reply before it times out. The value must be less than the delay
  value.
* `node-security-group`: ID of the security group to manage.
* `use-octavia`: Used to determine whether to look for and use an
  Octavia LBaaS V2 service catalog endpoint. Valid values are `true` or `false`.
  Where `true` is specified and an Octavia LBaaS V2 entry can not be found, the
  provider will fall back and attempt to find a Neutron LBaaS V2 endpoint
  instead. The default value is `false`.
* `internal-lb`: Determines whether or not to create an internal load balancer
  (no floating IP) by default. The default value is `false`.

#### Block Storage

These configuration options for the OpenStack provider pertain to block storage
and should appear in the `[BlockStorage]` section of the `$CLOUD_CONFIG` file.

##### Block Storage Optional Parameters

* `bs-version`: Used to override automatic version detection. Valid
  values are `v1`, `v2`, `v3` and `auto`. When `auto` is specified automatic
  detection will select the highest supported version exposed by the underlying
  OpenStack cloud. The default value if none is provided is `auto`.
* `ignore-volume-az`: Used to influence availability zone use when
  attaching Cinder volumes. When Nova and Cinder have different availability
  zones, this should be set to `true`. This is most commonly the case where
  there are many Nova availability zones but only one Cinder availability zone.
  The default value is `false` to preserve the behavior used in earlier
  releases, but may change in the future.
* `trust-device-path`: In most scenarios the block device names
  provided by Cinder (e.g. `/dev/vda`) can not be trusted. This boolean toggles
  this behavior. Setting it to `true` results in trusting the block device names
  provided by Cinder. The default value of `false` results in the discovery of
  the device path based on its serial number and `/dev/disk/by-id` mapping and is
  the recommended approach.

##### Block Storage (CSI)

* `NodeVolumeAttachLimit`: Maximum volumes that can be attached to the node. Its
  default value is 256.

##### Block Storage Notes

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
should appear in the `[Metadata]` section of the `$CLOUD_CONFIG` file.

##### Metadata Optional Parameters

* `search-order`: This configuration key influences the way that the
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
`$CLOUD_CONFIG` file.

##### Router Optional Parameters

* `router-id`: If the underlying cloud's Neutron deployment supports
  the `extraroutes` extension then use `router-id` to specify a router to add
  routes to.  The router chosen must span the private networks containing your
  cluster nodes (typically there is only one node network, and this value should be
  the default router for the node network).  This value is required to use [kubenet]
  on OpenStack.

[kubenet]: https://kubernetes.io/docs/concepts/cluster-administration/network-plugins/#kubenet
