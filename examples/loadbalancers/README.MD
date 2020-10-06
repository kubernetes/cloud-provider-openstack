# Loadbalancers

OpenStack Cloud Controller Manager runs service controller,
which is responsible for watching services of type ```LoadBalancer```
and creating OpenStack loadbalancers to satisfy its requirements.
Here are some examples of how it's used.

## External HTTP loadbalancer

When you create a service with ```type: LoadBalancer```, an OpenStack load balancer will be created.
The example below will create a nginx deployment and expose it via an OpenStack External load balancer.

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-http-nginx-deployment
spec:
  replicas: 2
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx
        ports:
        - containerPort: 80
---
kind: Service
apiVersion: v1
metadata:
  name: external-http-nginx-service
  annotations:
    service.beta.kubernetes.io/openstack-internal-load-balancer: "false"
    loadbalancer.openstack.org/floating-network-id: "9be23551-38e2-4d27-b5ea-ea2ea1321bd6"
spec:
  selector:
    app: nginx
  type: LoadBalancer
  ports:
  - name: http
    port: 80
    targetPort: 80
```

The ```service.beta.kubernetes.io/openstack-internal-load-balancer``` annotation
is used on the service to indicate that we want an internal loadbalancer service.
If the value of ```service.beta.kubernetes.io/openstack-internal-load-balancer``` is false,
it indicates that we want an external loadbalancer service. Default to false.

The ```loadbalancer.openstack.org/floating-network-id``` annotation
indicates that it will create a floating IP for the external loadbalancer service
on the specified floating network id. This annotation works when the value of
```service.beta.kubernetes.io/openstack-internal-load-balancer``` is false.
If this annotation is not specified, it will use the default floating network id.


```bash
$ kubectl create -f examples/loadbalancers/external-http-nginx.yaml
```

Watch the service and await an ```EXTERNAL-IP``` by the following command.
This will be the load balancer IP which you can use to connect to your service.

```bash
$ watch kubectl get service
NAME                 CLUSTER-IP     EXTERNAL-IP       PORT(S)        AGE
http-nginx-service   10.0.0.10      122.112.219.229   80:30000/TCP   5m
```

You can now access your service via the provisioned load balancer.

```bash
$ curl http://122.112.219.229
```

## Internal HTTP loadbalancer

The example below will create a nginx deployment and expose it via an OpenStack Internal load balancer.

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: internal-http-nginx-deployment
spec:
  replicas: 2
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx
        ports:
        - containerPort: 80
---
kind: Service
apiVersion: v1
metadata:
  name: internal-http-nginx-service
  annotations:
    service.beta.kubernetes.io/openstack-internal-load-balancer: "true"
spec:
  selector:
    app: nginx
  type: LoadBalancer
  ports:
  - name: http
    port: 80
    targetPort: 80
```

The value of ```service.beta.kubernetes.io/openstack-internal-load-balancer``` is true,
it indicates that we want an internal loadbalancer service.

```bash
$ kubectl create -f examples/loadbalancers/internal-http-nginx.yaml
```

Watch the service and await an ```EXTERNAL-IP``` by the following command.
This will be the load balancer IP which you can use to connect to your service.

```bash
$ watch kubectl get service
NAME                 CLUSTER-IP     EXTERNAL-IP       PORT(S)        AGE
http-nginx-service   10.0.0.10      192.168.0.181     80:30000/TCP   5m
```

You can now access your service via the provisioned load balancer.

```bash
$ curl http://192.168.0.181
```
