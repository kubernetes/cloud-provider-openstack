<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Exposing applications using services of LoadBalancer type](#exposing-applications-using-services-of-loadbalancer-type)
  - [Creating a Service of LoadBalancer type](#creating-a-service-of-loadbalancer-type)
  - [Supported Features](#supported-features)
    - [Service annotations](#service-annotations)
    - [Switching between Floating Subnets by using preconfigured Classes](#switching-between-floating-subnets-by-using-preconfigured-classes)
    - [Creating Service by specifying a floating IP](#creating-service-by-specifying-a-floating-ip)
    - [Restrict Access For LoadBalancer Service](#restrict-access-for-loadbalancer-service)
  - [Issues](#issues)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Exposing applications using services of LoadBalancer type

This page shows how to create Services of LoadBalancer type in Kubernetes cluster which is running inside OpenStack. For an explanation of the Service concept and a discussion of the various types of Services, see [Services](https://kubernetes.io/docs/concepts/services-networking/service/).

A LoadBalancer type Service is a typical way to expose an application to the internet. It relies on the cloud provider to create an external load balancer with an IP address in the relevant network space. Any traffic that is then directed to this IP address is forwarded on to the application’s service.

> Note: Different cloud providers may support different Service annotations and features.

## Creating a Service of LoadBalancer type

Create an application of Deployment as the Service backend:

```shell
kubectl run echoserver --image=gcr.io/google-containers/echoserver:1.10 --port=8080
```

To provide the echoserver application with an internet-facing loadbalancer we can simply run the following:

```shell
cat <<EOF | kubectl apply -f -
---
kind: Service
apiVersion: v1
metadata:
  name: loadbalanced-service
spec:
  selector:
    run: echoserver
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 8080
    protocol: TCP
EOF
```

Check the state the status of the loadbalanced-service until the `EXTERNAL-IP` status is no longer pending.

```shell
$ kubectl get service loadbalanced-service
NAME                   TYPE           CLUSTER-IP      EXTERNAL-IP    PORT(S)        AGE
loadbalanced-service   LoadBalancer   10.254.28.183   202.49.242.3   80:31177/TCP   2m18s
```

Once we can see that our service is active and has been assigned an external IP address we should be able to test our application via `curl` from any internet accessible machine.

```shell
$ curl 202.49.242.3
Hostname: echoserver-74dcfdbd78-fthv9
Pod Information:
        -no pod information available-
Server values:
        server_version=nginx: 1.13.3 - lua: 10008
Request Information:
        client_address=10.0.0.7
        method=GET
        real path=/
        query=
        request_version=1.1
        request_scheme=http
        request_uri=http://202.49.242.3:8080/
Request Headers:
        accept=*/*
        host=202.49.242.3
        user-agent=curl/7.47.0
Request Body:
        -no body in request-
```

## Supported Features

### Service annotations

- `loadbalancer.openstack.org/floating-network-id`

  The public network id which will allocate public IP for loadbalancer. This annotation works when the value of `service.beta.kubernetes.io/openstack-internal-load-balancer` is false.

- `loadbalancer.openstack.org/floating-subnet`

  A public network can have several subnets. This annotation is the name of subnet belonging to the floating network. This annotation is optional.

- `loadbalancer.openstack.org/floating-subnet-id`

  This annotation is the ID of a subnet belonging to the floating network, if specified, it takes precedence over `loadbalancer.openstack.org/floating-subnet`.

- `loadbalancer.openstack.org/class`

  The name of a preconfigured class in the config file. If provided, this config options included in the class section take precedence over the annotations of floating-subnet-id and floating-network-id. See the section below for how it works.

- `loadbalancer.openstack.org/subnet-id`

  VIP subnet ID of load balancer created.

- `loadbalancer.openstack.org/network-id`

  The network ID which will allocate virtual IP for loadbalancer.

- `loadbalancer.openstack.org/port-id`

  The VIP port ID for load balancer created.

- `loadbalancer.openstack.org/connection-limit`

  The maximum number of connections per second allowed for the listener. Positive integer or -1 for unlimited (default). This annotation supports update operation.

- `loadbalancer.openstack.org/keep-floatingip`

  If 'true', the floating IP will **NOT** be deleted. Default is 'false'.

- `loadbalancer.openstack.org/proxy-protocol`

  If 'true', the protocol for listener will be set as `PROXY`. Default is 'false'.

- `loadbalancer.openstack.org/x-forwarded-for`

  If 'true', `X-Forwarded-For` is inserted into the HTTP headers which contains the original client IP address so that the backend HTTP service is able to get the real source IP of the request. Only applies when using Octavia.

- `loadbalancer.openstack.org/timeout-client-data`

  Frontend client inactivity timeout in milliseconds for the load balancer.

- `loadbalancer.openstack.org/timeout-member-connect`

  Backend member connection timeout in milliseconds for the load balancer.

- `loadbalancer.openstack.org/timeout-member-data`

  Backend member inactivity timeout in milliseconds for the load balancer.

- `loadbalancer.openstack.org/timeout-tcp-inspect`

  Time to wait for additional TCP packets for content inspection in milliseconds for the load balancer.

- `service.beta.kubernetes.io/openstack-internal-load-balancer`

  If 'true', the loadbalancer VIP won't be associated with a floating IP. Default is 'false'. This annotation is ignored if only internal Service is allowed to create in the cluster.

- `loadbalancer.openstack.org/enable-health-monitor`

  Defines whether or not to create health monitor for the load balancer pool, if not specified, use `create-monitor` config. The health monitor can be created or deleted dynamically.
  
- `loadbalancer.openstack.org/flavor-id`

  The id of the flavor that is used for creating the loadbalancer.

### Switching between Floating Subnets by using preconfigured Classes

If you have multiple `FloatingIPPools` and/or `FloatingIPSubnets` it might be desirable to offer the user logical meanings for `LoadBalancers` like `internetFacing` or `DMZ` instead of requiring the user to select a dedicated network or subnet ID at the service object level as an annotation.

With a `LoadBalancerClass` it possible to specify to which floating network and corresponding subnetwork the `LoadBalancer` belong.

In the example `cloud.conf` below three `LoadBalancerClass`'es have been defined: `internetFacing`, `dmz` and `office`

```ini
[Global]
auth-url="https://someurl"
domain-name="mydomain"
tenant-name="mytenant"
username="myuser"
password="mypassword"

[LoadBalancer]
use-octavia=true
floating-network-id="a57af0a0-da92-49be-a98a-345ceca004b3"
floating-subnet-id="a02eb6c3-fc69-46ae-a3fe-fb43c1563cbc"
subnet-id="fa6a4e6c-6ae4-4dde-ae86-3e2f452c1f03"
create-monitor=true
monitor-delay=60s
monitor-timeout=30s
monitor-max-retries=5

[LoadBalancerClass "internetFacing"]
floating-network-id="c57af0a0-da92-49be-a98a-345ceca004b3"
floating-subnet-id="f90d2440-d3c6-417a-a696-04e55eeb9279"

[LoadBalancerClass "dmz"]
floating-subnet-id="a374bed4-e920-4c40-b646-2d8927f7f67b"

[LoadBalancerClass "office"]
floating-subnet-id="b374bed4-e920-4c40-b646-2d8927f7f67b"
```

Within a `LoadBalancerClass` the `floating-subnet-id` is mandatory. `floating-network-id` is optional can be defined in case it differs from the default `floating-network-id` in the `LoadBalancer` section.

By using the `loadbalancer.openstack.org/class` annotation on the service object, you can now select which floating subnets the `LoadBalancer` should be using.

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    loadbalancer.openstack.org/class: internetFacing
  name: nginx-internet
spec:
  type: LoadBalancer
  selector:
    app: nginx
  ports:
  - port: 80
    targetPort: 80
```

### Creating Service by specifying a floating IP

Sometimes it's useful to use an existing available floating IP rather than creating a new one, especially in the automation scenario. In the example below, 122.112.219.229 is an available floating IP created in the OpenStack Networking service.

> NOTE: If 122.112.219.229 is not available, a new floating IP will be created automatically from the configured public network. If 122.112.219.229 is already associated with another port, the Service creation will fail.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-internet
spec:
  type: LoadBalancer
  selector:
    app: nginx
  ports:
  - port: 80
    targetPort: 80
  loadBalancerIP: 122.112.219.229
```

### Restrict Access For LoadBalancer Service

When using a Service with `spec.type: LoadBalancer`, you can specify the IP ranges that are allowed to access the load balancer by using `spec.loadBalancerSourceRanges`. This field takes a list of IP CIDR ranges, which Kubernetes will use to configure firewall exceptions.

This feature is only supported in the OpenStack Cloud with Octavia(API version >= v2.12) service deployed, otherwise `loadBalancerSourceRanges` is ignored.

In the following example, a load balancer will be created that is only accessible to clients with IP addresses in 192.168.32.1/24.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: test
  namespace: default
spec:
  type: LoadBalancer
  loadBalancerSourceRanges:
    - 192.168.32.1/24
  selector:
    run: echoserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
```

`loadBalancerSourceRanges` field supports to be updated.

## Issues

- `spec.externalTrafficPolicy` is not supported.
