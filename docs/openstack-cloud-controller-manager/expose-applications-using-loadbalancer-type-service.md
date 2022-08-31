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
    - [Use PROXY protocol to preserve client IP](#use-proxy-protocol-to-preserve-client-ip)
    - [Sharing load balancer with multiple Services](#sharing-load-balancer-with-multiple-services)
    - [IPv4 / IPv6 dual-stack services](#ipv4--ipv6-dual-stack-services)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Exposing applications using services of LoadBalancer type

This page shows how to create Services of LoadBalancer type in Kubernetes cluster which is running inside OpenStack. For an explanation of the Service concept and a discussion of the various types of Services, see [Services](https://kubernetes.io/docs/concepts/services-networking/service/).

A LoadBalancer type Service is a typical way to expose an application to the internet. It relies on the cloud provider to create an external load balancer with an IP address in the relevant network space. Any traffic that is then directed to this IP address is forwarded on to the application’s service.

**NOTE: for test/PoC with only 1 master node environment, you need remove the label `node.kubernetes.io/exclude-from-external-load-balancers` of the master node otherwise the loadbalancer will not be created. search the label [here](https://pkg.go.dev/k8s.io/api/core/v1) for further information.**

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

  This annotation is the ID of a subnet belonging to the floating network, if specified, it takes precedence over `loadbalancer.openstack.org/floating-subnet` or `loadbalancer.openstack.org/floating-tag`.

- `loadbalancer.openstack.org/floating-subnet-tags`

  This annotation is the tag of a subnet belonging to the floating network.

- `loadbalancer.openstack.org/class`

  The name of a preconfigured class in the config file. If provided, this config options included in the class section take precedence over the annotations of floating-subnet-id, floating-network-id, network-id, subnet-id and member-subnet-id . See the section below for how it works.

- `loadbalancer.openstack.org/subnet-id`

  VIP subnet ID of load balancer created.

- `loadbalancer.openstack.org/member-subnet-id`

  Member subnet ID of the load balancer created.

- `loadbalancer.openstack.org/network-id`

  The network ID which will allocate virtual IP for loadbalancer.

- `loadbalancer.openstack.org/port-id`

  The VIP port ID for load balancer created.

- `loadbalancer.openstack.org/connection-limit`

  The maximum number of connections per second allowed for the listener. Positive integer or -1 for unlimited (default). This annotation supports update operation.

- `loadbalancer.openstack.org/keep-floatingip`

  If 'true', the floating IP will **NOT** be deleted. Default is 'false'.

- `loadbalancer.openstack.org/proxy-protocol`

  If 'true', the loadbalancer pool protocol will be set as `PROXY`. Default is 'false'.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/x-forwarded-for`

  If 'true', `X-Forwarded-For` is inserted into the HTTP headers which contains the original client IP address so that the backend HTTP service is able to get the real source IP of the request. Please note that the cloud provider will force the creation of an Octavia listener of type `HTTP` if this option is set. Only applies when using Octavia.

  This annotation also works in conjunction with the `loadbalancer.openstack.org/default-tls-container-ref` annotation. In this case the cloud provider will create an Octavia listener of type `TERMINATED_HTTPS` instead of an `HTTP` listener.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/timeout-client-data`

  Frontend client inactivity timeout in milliseconds for the load balancer.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/timeout-member-connect`

  Backend member connection timeout in milliseconds for the load balancer.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/timeout-member-data`

  Backend member inactivity timeout in milliseconds for the load balancer.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/timeout-tcp-inspect`

  Time to wait for additional TCP packets for content inspection in milliseconds for the load balancer.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `service.beta.kubernetes.io/openstack-internal-load-balancer`

  If 'true', the loadbalancer VIP won't be associated with a floating IP. Default is 'false'. This annotation is ignored if only internal Service is allowed to create in the cluster.

- `loadbalancer.openstack.org/enable-health-monitor`

  Defines whether to create health monitor for the load balancer pool, if not specified, use `create-monitor` config. The health monitor can be created or deleted dynamically. A health monitor is required for services with `externalTrafficPolicy: Local`.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/health-monitor-delay`

  Defines the health monitor delay in seconds for the loadbalancer pools.

- `loadbalancer.openstack.org/health-monitor-timeout`

  Defines the health monitor timeout in seconds for the loadbalancer pools. This value should be less than delay

- `loadbalancer.openstack.org/health-monitor-max-retries`

  Defines the health monitor retry count for the loadbalancer pools.

- `loadbalancer.openstack.org/flavor-id`

  The id of the flavor that is used for creating the loadbalancer.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/availability-zone`

  The name of the loadbalancer availability zone to use. It is ignored if the Octavia version doesn't support availability zones yet.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/default-tls-container-ref`

  Reference to a tls container. This option works with Octavia, when this option is set then the cloud provider will create an Octavia Listener of type `TERMINATED_HTTPS` for a TLS Terminated loadbalancer.
  Format for tls container ref: `https://{keymanager_host}/v1/containers/{uuid}`

  When `container-store` parameter is set to `external` format for `default-tls-container-ref` could be any string.

  Not supported when `lb-provider=ovn` is configured in openstack-cloud-controller-manager.

- `loadbalancer.openstack.org/load-balancer-id`

  This annotation is automatically added to the Service if it's not specified when creating. After the Service is created successfully it shouldn't be changed, otherwise the Service won't behave as expected.

  If this annotation is specified with a valid cloud load balancer ID when creating Service, the Service is reusing this load balancer rather than creating another one. Again, it shouldn't be changed after the Service is created.

  If this annotation is specified, the other annotations which define the load balancer features will be ignored.

- `loadbalancer.openstack.org/hostname`

  This annotations explicitly sets a hostname in the status of the load balancer service.

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

Within a `LoadBalancerClass` one of `floating-subnet-id`, `floating-subnet` or `floating-subnet-tags` is mandatory.
`floating-subnet-id` takes precedence over the other ones with must all match if specified.
If the pattern starts with a `!`, the match is negated.
The rest of the pattern can either be a direct name, a glob or a regular expression if it starts with a `~`.
`floating-subnet-tags` can be a comma separated list of tags. By default it matches a subnet if at least one tag is present.
If the list is preceded by a `&` all tags must be present. Again with a preceding `!` the condition be be negated.
`floating-network-id` is optional can be defined in case it differs from the default `floating-network-id` in the `LoadBalancer` section.

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

### Use PROXY protocol to preserve client IP

When exposing services like nginx-ingress-controller, it's a common requirement that the client connection information could pass through proxy servers and load balancers, therefore visible to the backend services. Knowing the originating IP address of a client may be useful for setting a particular language for a website, keeping a denylist of IP addresses, or simply for logging and statistics purposes.

This requires that not only the proxy server(e.g. NGINX) should support PROXY protocol, but also the external load balancer (created by openstack-cloud-controller-manager in our case) should be able to send the correct data traffic to the proxy server.

This guide uses nginx-ingress-controller as an example.

To enable PROXY protocol support, the either the openstack-cloud-controller-manager config option [enable-ingress-hostname](./using-openstack-cloud-controller-manager.md#load-balancer) should set to `true` or an explicit hostname should be set on the load balancer service via [annotation](./expose-applications-using-loadbalancer-type-service.md#service-annotations) `loadbalancer.openstack.org/hostname`.

1. Set up the nginx-ingress-controller

   Refer to https://docs.nginx.com/nginx-ingress-controller/installation for how to install nginx-ingress-controller deployment or daemonset. Before creating load balancer service, make sure to enable PROXY protocol in the nginx config.

   ```yaml
   proxy-protocol: "True"
   real-ip-header: "proxy_protocol"
   set-real-ip-from: "0.0.0.0/0"
   ```

2. Create load balancer service

   Use the following manifest to create nginx-ingress Service of LoadBalancer type.

   ```yaml
   apiVersion: v1
   kind: Service
   metadata:
     name: nginx-ingress
     namespace: nginx-ingress
     annotations:
       loadbalancer.openstack.org/proxy-protocol: "true"
   spec:
     externalTrafficPolicy: Cluster
     type: LoadBalancer
     ports:
     - port: 80
       targetPort: 80
       protocol: TCP
       name: http
     - port: 443
       targetPort: 443
       protocol: TCP
       name: https
     selector:
       app: nginx-ingress
   ```

   Wait until the service gets an external IP.

   ```bash
   $ kubectl -n nginx-ingress get svc
   NAME            TYPE           CLUSTER-IP       EXTERNAL-IP             PORT(S)                      AGE
   nginx-ingress   LoadBalancer   10.104.112.154   103.250.240.24.nip.io   80:32418/TCP,443:30009/TCP   107s
   ```

3. To validate the PROXY protocol is working, create a service that can print the request header and an ingress backed by nginx-ingress-controller.

   ```bash
   $ cat <<EOF | kubectl apply -f -
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: echoserver
     namespace: default
     labels:
       app: echoserver
   spec:
     replicas: 1
     selector:
       matchLabels:
         app: echoserver
     template:
       metadata:
         labels:
           app: echoserver
       spec:
         containers:
         - name: echoserver
           image: gcr.io/google-containers/echoserver:1.10
           imagePullPolicy: IfNotPresent
           ports:
             - containerPort: 8080
   EOF

   $ kubectl expose deployment echoserver --type=ClusterIP --target-port=8080

   $ cat <<EOF | kubectl apply -f -
   apiVersion: networking.k8s.io/v1
   kind: Ingress
   metadata:
     name: test-proxy-protocol
     namespace: default
     annotations:
       kubernetes.io/ingress.class: "nginx"
   spec:
     rules:
       - host: test.com
         http:
           paths:
           - path: /ping
             pathType: Exact
             backend:
               service:
                 name: echoserver
                 port:
                   number: 80
   EOF

   $ kubectl get ing
   NAME                   CLASS    HOSTS      ADDRESS                 PORTS   AGE
   test-proxy-protocol    <none>   test.com   103.250.240.24.nip.io   80      58m
   ```

   Now, send request to the ingress URL defined above, you should see your public IP address is shown in the Request Headers (`x-forwarded-for` or `x-real-ip`).

   ```bash
   $ ip=103.250.240.24.nip.io
   $ curl -sH "Host: test.com" http://$ip/ping | sed '/^\s*$/d'
   Hostname: echoserver-5c79dc5747-m4spf
   Pod Information:
           -no pod information available-
   Server values:
           server_version=nginx: 1.13.3 - lua: 10008
   Request Information:
           client_address=10.244.215.132
           method=GET
           real path=/ping
           query=
           request_version=1.1
           request_scheme=http
           request_uri=http://test.com:8080/ping
   Request Headers:
           accept=*/*
           connection=close
           host=test.com
           user-agent=curl/7.58.0
           x-forwarded-for=103.197.63.236
           x-forwarded-host=test.com
           x-forwarded-port=80
           x-forwarded-proto=http
           x-real-ip=103.197.63.236
   Request Body:
           -no body in request-
   ```

### Sharing load balancer with multiple Services

By default, different Services of LoadBalancer type should have different corresponding cloud load balancers, however, openstack-cloud-controller-manager allows multiple Services to share a single load balancer if the Octavia service supports the tag feature (since version 2.5).

The shared load balancer can be created either by other Services or outside the cluster, e.g. created manually by the user in the cloud or by Services from the other Kubernetes clusters. The load balancer is deleted only when the last attached Service is deleted, unless the load balancer was created outside the Kubernetes cluster.

The maximum number of Services that share a load balancer can be configured in `[LoadBalancer] max-shared-lb`, default value is 2. The ports of those Services shouldn't have collisions.

For example, create a Service `service-1` as before:

```yaml
kind: Service
apiVersion: v1
metadata:
  name: service-1
  namespace: default
spec:
  type: LoadBalancer
  selector:
    app: webserver
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
```

When `service-1` is created successfully, check the load balancer created in the cloud, which has its name in its tags.

```shell
$ openstack loadbalancer show 2b224530-9414-4302-8163-5abebdcdc84f -c name -c tags
+-------+---------------------------------------------+
| Field | Value                                       |
+-------+---------------------------------------------+
| name  | kube_service_cluster-name_default_service-1 |
| tags  | kube_service_cluster-name_default_service-1 |
+-------+---------------------------------------------+
```

Check the Service, you should notice a new annotation `loadbalancer.openstack.org/load-balancer-id` is added:

```shell
$ kubectl describe service service-1 | grep loadbalancer.openstack.org/load-balancer-id
                          loadbalancer.openstack.org/load-balancer-id: 2b224530-9414-4302-8163-5abebdcdc84f
```

> NOTE: Do not update the annotation `loadbalancer.openstack.org/load-balancer-id` after the Service is created successfully or the relationship between Service and the load balancer will be broken.

Now, create another Service `service-2` but re-use the load balancer created for `service-1` by specifying the annotation `loadbalancer.openstack.org/load-balancer-id`:

```yaml
kind: Service
apiVersion: v1
metadata:
  name: service-2
  namespace: default
  annotations:
    loadbalancer.openstack.org/load-balancer-id: "2b224530-9414-4302-8163-5abebdcdc84f"
spec:
  type: LoadBalancer
  selector:
    app: webserver
  ports:
    - protocol: TCP
      port: 8080
      targetPort: 8080
```

After `service-2` is created successfully, check the load balancer again, you'll see there is a new tag added. Now the load balancer should have 2 listeners, listening on the ports of the 2 Services respectively.

```shell
$ openstack loadbalancer show 2b224530-9414-4302-8163-5abebdcdc84f -c name -c tags
+-------+---------------------------------------------+
| Field | Value                                       |
+-------+---------------------------------------------+
| name  | kube_service_lingxian-k8s_default_service-1 |
| tags  | kube_service_lingxian-k8s_default_service-1 |
|       | kube_service_lingxian-k8s_default_service-2 |
+-------+---------------------------------------------+
$ openstack loadbalancer listener list --loadbalancer 2b224530-9414-4302-8163-5abebdcdc84f -c id -c protocol -c protocol_port
+--------------------------------------+----------+---------------+
| id                                   | protocol | protocol_port |
+--------------------------------------+----------+---------------+
| 05fbcc93-61e5-4eb4-be21-632ab8022d46 | TCP      |            80 |
| 50e94cc4-f08e-4c71-9ee4-4488350834f6 | TCP      |          8080 |
+--------------------------------------+----------+---------------+
```

Check the load balancer again after deleting `service-1`:

```shell
$ openstack loadbalancer show 2b224530-9414-4302-8163-5abebdcdc84f -c name -c tags
+-------+---------------------------------------------+
| Field | Value                                       |
+-------+---------------------------------------------+
| name  | kube_service_lingxian-k8s_default_service-1 |
| tags  | kube_service_lingxian-k8s_default_service-2 |
+-------+---------------------------------------------+
$ openstack loadbalancer listener list --loadbalancer 2b224530-9414-4302-8163-5abebdcdc84f -c id -c protocol -c protocol_port
+--------------------------------------+----------+---------------+
| id                                   | protocol | protocol_port |
+--------------------------------------+----------+---------------+
| 50e94cc4-f08e-4c71-9ee4-4488350834f6 | TCP      |          8080 |
+--------------------------------------+----------+---------------+
```

The load balancer will be deleted after `service-2` is deleted.

### IPv4 / IPv6 dual-stack services
Since Kubernetes 1.20, Kubernetes clusters can run in dual-stack mode,
which allows simultaneous usage of both IPv4 and IPv6 addresses in the cluster.
In dual-stack clusters, services can use IPv4, IPv6, or both address families, which
can be indicated in service's `spec.ipFamilies`.

If only one address family is specified in service's `spec.ipFamilies`, OCCM will respect
that and create an IPv4 or IPv6 load balancer based on that.

If two address families are specified in service's `spec.ipFamilies`, OCCM will respect the
specified order and create an IPv4 or IPv6 load balancer based on the first specified address
family. Note that creation of two load balancers for services with two `spec.ipFamilies`
is not yet supported by OCCM.

Internally, OCCM would automatically look for IPv4 or IPv6 subnet to allocate the load balancer
address from based on the service's address family preference. If the subnet with preferred
address family is not available, load balancer can not be created.
