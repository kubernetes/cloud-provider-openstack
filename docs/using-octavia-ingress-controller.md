# Get started with octavia-ingress-controller for Kubernetes

This guide explains how to deploy and config the octavia-ingress-controller in Kubernetes cluster on top of OpenStack cloud.

## What is an Ingress Controller?
In Kubernetes, Ingress allows external users and client applications access to HTTP services. Ingress consists of two components.

- [Ingress Resource](https://kubernetes.io/docs/concepts/services-networking/ingress/#the-ingress-resource) is a collection of rules for the inbound traffic to reach Services. These are Layer 7 (L7) rules that allow hostnames (and optionally paths) to be directed to specific Services in Kubernetes.
- [Ingress Controller](https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-controllers) which acts upon the rules set by the Ingress Resource, typically via an HTTP or L7 load balancer.

It is vital that both pieces are properly configured to route traffic from an outside client to a Kubernetes Service.

## Why octavia-ingress-controller
As an OpenStack based public cloud provider in [Catalyst Cloud](https://catalystcloud.nz/), one of our goals is to continuously provide the customers the capability of innovation by delivering robust and comprehensive cloud services. After deploying Octavia and Magnum service in the public cloud, we are thinking about how to help customers to develop their applications running on the Kubernetes cluster and make their services accessible to the public in a high-performance way.

After creating a Kubernetes cluster in Magnum, the most common way to expose the application to the outside world is to use [LoadBalancer](https://kubernetes.io/docs/concepts/services-networking/service/#type-loadbalancer) type service. In the OpenStack cloud, Octavia(LBaaS v2) is the default implementation of LoadBalancer type service, as a result, for each LoadBalancer type service, there is a load balancer created in the cloud tenant account. We could see some drawbacks of this way:

- The cost of Kubernetes Service is a little bit high if it's one-to-one mapping from the service to Octavia load balancer, the customers have to pay for a load balancer per exposed service, which can get expensive.
- There is no filtering, no routing, etc. for the service. This means you can send almost any kind of traffic to it, like HTTP, TCP, UDP, Websockets, gRPC, or whatever.
- The traditional ingress controllers such as NGINX ingress controller,  HAProxy ingress controller, Træfik, etc. don't make much sense in the cloud environment because the user still needs to expose service for the ingress controller itself which may increase the network delay and decrease the performance of the application.

The octavia-ingress-controller could solve all the above problems in the OpenStack environment by creating a single load balancer for multiple [NodePort](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport) type services in an Ingress. In order to use the octavia-ingress-controller in Kubernetes cluster, use the value `openstack` for the annotation `kubernetes.io/ingress.class` in the metadata section of the Ingress Resource as shown below:

```bash
annotations: kubernetes.io/ingress.class: openstack
```

## How to deploy octavia-ingress-controller

In the guide, we will deploy octavia-ingress-controller as a deployment(with only one pod) in the kube-system namespace in the cluster. Alternatively, you can also deploy the controller as a static pod by providing a manifest file in the `/etc/kubernetes/manifests` folder in a typical Kubernetes cluster installed by kubeadm. All the manifest files in this guide are saved in `/etc/kubernetes/octavia-ingress-controller` folder, so create the folder first.

```shell
mkdir -p /etc/kubernetes/octavia-ingress-controller
```

### Create service account and grant permissions

For testing purpose, we grant the cluster admin role to the serviceaccount created.

```shell
cat <<EOF > /etc/kubernetes/octavia-ingress-controller/serviceaccount.yaml
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: octavia-ingress-controller
  namespace: kube-system
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: octavia-ingress-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: octavia-ingress-controller
    namespace: kube-system
EOF
kubectl apply -f /etc/kubernetes/octavia-ingress-controller/serviceaccount.yaml
```

### Prepare octavia-ingress-controller configuration

The octavia-ingress-controller needs to communicate with OpenStack cloud to create resources corresponding to the Kubernetes ingress resource, so the credentials of an OpenStack user(doesn't need to be the admin user) need to be provided. Additionally, in order to differentiate the ingresses from different kubernetes cluster, `cluster_name` needs to be unique.

```shell
cat <<EOF > /etc/kubernetes/octavia-ingress-controller/config.yaml
---
kind: ConfigMap
apiVersion: v1
metadata:
  name: octavia-ingress-controller-config
  namespace: kube-system
data:
  config: |
    cluster_name: ${cluster_name}
    openstack:
      auth_url: ${auth_url}
      user_id: ${user_id}
      password: ${password}
      project_id: ${project_id}
      region: ${region}
    octavia:
      subnet_id: ${subnet_id}
      floating_network_id: ${public_net_id}
EOF
kubectl apply -f /etc/kubernetes/octavia-ingress-controller/config.yaml
```

### Deploy octavia-ingress-controller

```shell
image="docker.io/k8scloudprovider/octavia-ingress-controller:lastest"

cat <<EOF > /etc/kubernetes/octavia-ingress-controller/deployment.yaml
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: octavia-ingress-controller
  namespace: kube-system
  labels:
    k8s-app: octavia-ingress-controller
spec:
  replicas: 1
  selector:
    matchLabels:
      k8s-app: octavia-ingress-controller
  template:
    metadata:
      labels:
        k8s-app: octavia-ingress-controller
    spec:
      serviceAccountName: octavia-ingress-controller
      tolerations:
        - effect: NoSchedule # Make sure the pod can be scheduled on master kubelet.
          operator: Exists
        - key: CriticalAddonsOnly # Mark the pod as a critical add-on for rescheduling.
          operator: Exists
        - effect: NoExecute
          operator: Exists
      containers:
        - name: octavia-ingress-controller
          image: ${image}
          imagePullPolicy: IfNotPresent
          args:
            - /bin/octavia-ingress-controller
            - --config=/etc/config/octavia-ingress-controller-config.yaml
          volumeMounts:
            - mountPath: /etc/kubernetes
              name: kubernetes-config
              readOnly: true
            - name: ingress-config
              mountPath: /etc/config
      hostNetwork: true
      volumes:
        - name: kubernetes-config
          hostPath:
            path: /etc/kubernetes
            type: Directory
        - name: ingress-config
          configMap:
            name: octavia-ingress-controller-config
            items:
              - key: config
                path: octavia-ingress-controller-config.yaml
EOF
kubectl apply -f /etc/kubernetes/octavia-ingress-controller/deployment.yaml
```

Wait until the deployment is up and running.

## Setting up HTTP Load Balancing with Ingress

### Create a backend service
Create a simple service(echo hostname) that is listening on a HTTP server on port 8080.

```bash
$ kubectl run hostname-server --image=lingxiankong/alpine-test --port=8080
$ kubectl expose deployment hostname-server --type=NodePort --target-port=8080
$ kubectl get svc
NAME                TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)          AGE
hostname-server     NodePort    10.106.36.88   <none>        8080:32066/TCP   33s
```

When you create a Service of type NodePort, Kubernetes makes your Service available on a randomly- selected high port number (e.g. 32066) on all the nodes in your cluster. Generally the Kubernetes nodes are not externally accessible by default, creating this Service does not make your application accessible from the Internet. However, we could verify the service using its `CLUSTER-IP` on Kubernetes master node:

```bash
$ curl http://10.106.36.88:8080
hostname-server-698fd44fc8-jptl2
```

Next, we create an Ingress resource to make your HTTP web server application publicly accessible.

### Create an Ingress resource

The following command defines an Ingress resource that directs traffic that requests `http://api.sample.com/ping` to the `hostname-server` Service:
```bash
cat <<EOF | kubectl apply -f -
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: test-octavia-ingress
  annotations:
    kubernetes.io/ingress.class: "openstack"
spec:
  rules:
  - host: api.sample.com
    http:
      paths:
      - path: /ping
        backend:
          serviceName: hostname-server
          servicePort: 8080
EOF
```

Kubernetes creates an Ingress resource on your cluster. The octavia-ingress-controller service running in your cluster is responsible for creating/maintaining the corresponding resources in Octavia to route all external HTTP traffic (on port 80) to the `hostname-server` NodePort Service you exposed.

Verify that Ingress Resource has been created. Please note that the IP address for the Ingress Resource will not be defined right away (wait a few moments for the ADDRESS field to get populated):

```bash
$ kubectl get ing
NAME                   HOSTS            ADDRESS   PORTS     AGE
test-octavia-ingress   api.sample.com             80        12s
$ # Wait until the ingress gets an IP address
$ kubectl get ing
NAME                   HOSTS            ADDRESS      PORTS     AGE
test-octavia-ingress   api.sample.com   172.24.4.9   80        9m
```

For testing purpose, you can log into a host that has network connection with the OpenStack cloud, you should be able to access the backend service by sending HTTP request to the domain name specified in the Ingress Resource:

```shell
$ IPADDRESS=172.24.4.9
$ curl -H "Host: api.sample.com" http://$IPADDRESS/ping
hostname-server-698fd44fc8-jptl2
```
