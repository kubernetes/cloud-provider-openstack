# Exposing applications using services of LoadBalancer type

This page shows how to create Services of LoadBalancer type in Kubernetes cluster which is running inside OpenStack. For an explanation of the Service concept and a discussion of the various types of Services, see [Services](https://kubernetes.io/docs/concepts/services-networking/service/).

A LoadBalancer type Service is the typical way to expose an application to the internet. It relies on the cloud provider to create an external load balancer with an IP address in the relevant network space. Any traffic that is then directed to this IP address is forwarded on to the application’s service.

> Note: Different cloud providers may support different Service annotations and features.

## Creating a Service of LoadBalancer type

Create an application of Deployment as the Service backend:
```shell
kubectl run echoserver --image=gcr.io/google-containers/echoserver:1.10 --port=8080
```

To provide the echoserver application with an internet facing loadbalancer we can simply run the following:
```shell
cat <<EOF > loadbalancer.yaml
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
kubectl apply -f loadbalancer.yaml
```

Check the state the status of the loadbalanced-service until the EXTERNAL-IP status is no longer <pending>.

```shell
$ kubectl get service loadbalanced-service
NAME                   TYPE           CLUSTER-IP      EXTERNAL-IP    PORT(S)        AGE
loadbalanced-service   LoadBalancer   10.254.28.183   202.49.242.3   80:31177/TCP   2m18s
```

Once we can see that our service is active and has been assigned an external IP address we should be able to test our application via curl from any internet accessible machine.

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
- loadbalancer.openstack.org/floating-network-id
- loadbalancer.openstack.org/floating-subnet
- loadbalancer.openstack.org/floating-subnet-id
- loadbalancer.openstack.org/class
- loadbalancer.openstack.org/subnet-id
- loadbalancer.openstack.org/port-id
- loadbalancer.openstack.org/connection-limit
- loadbalancer.openstack.org/keep-floatingip
- loadbalancer.openstack.org/proxy-protocol
- loadbalancer.openstack.org/x-forwarded-for

  If 'true', `X-Forwarded-For` is inserted into the HTTP headers which contains the original client IP address so that the backend HTTP service is able to get the real source IP of the request.

### Switching between Floating Subnets by using preconfigured Classes

If you have multiple `FloatingIPPools` and/or `FloatingIPSubnets` it might be desirable to offer the user logical meanings for `LoadBalancers` like `internetFacing` or `DMZ` instead of requiring the user to select a dedicated network or subnet ID at the service object level as an annotation.

With a `LoadBalancerClass` it possible to specify to which floating network and corresponding subnetwork the `LoadBalancer` belongs.

In the example `cloud.conf` below three `LoadBalancerClass`'es have been defined: `internetFacing`, `dmz` and `office`

```shell
[Global]
auth-url="https://someurl"
domain-name="mydomain"
tenant-name="mytenant"
username="myuser"
password="mypassword"
[LoadBalancer]
lb-version=v2
lb-provider="f5networks"
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

```shell
apiVersion: v1
kind: Service
metadata:
  annotations:
    loadbalancer.openstack.org/class: internetFacing
  name: nginx-internet
spec:
  ports:
  - port: 80
    targetPort: 80
  selector:
    app: nginx
  type: LoadBalancer
```

### Creating Service by specifying a floating IP
TBD

## Issues
TBD
