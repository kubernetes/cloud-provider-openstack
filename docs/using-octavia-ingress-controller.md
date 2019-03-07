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
- The traditional ingress controllers(such as NGINX ingress controller,  HAProxy ingress controller, Traefik ingress controller, etc.) don't make much sense in the cloud environment because they still rely on the cloud load balancing service to expose themselves behind a Service of LoadBalancer type, not to mention the overhead of managing the extra softwares.

The octavia-ingress-controller could solve all the above problems in the OpenStack environment by creating a single load balancer for multiple [NodePort](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport) type services in an Ingress. In order to use the octavia-ingress-controller in Kubernetes cluster, set the annotation `kubernetes.io/ingress.class` in the `metadata` section of the Ingress resource as shown below:

```yaml
annotations:
  kubernetes.io/ingress.class: "openstack"
```

## Deploy octavia-ingress-controller in the Kubernetes cluster

In the guide, we will deploy octavia-ingress-controller as a StatefulSet(with only one pod) in the kube-system namespace in the cluster. Alternatively, you can also deploy the controller as a static pod by providing a manifest file in the `/etc/kubernetes/manifests` folder in a typical Kubernetes cluster installed by kubeadm. All the manifest files in this guide are saved in `/etc/kubernetes/octavia-ingress-controller` folder, so create the folder first.

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

The octavia-ingress-controller needs to communicate with OpenStack cloud to create resources corresponding to the Kubernetes Ingress resource, so the credentials of an OpenStack user(doesn't need to be the admin user) need to be provided in `openstack` section. Additionally, in order to differentiate the Ingresses between kubernetes clusters, `cluster-name` needs to be unique.

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
    cluster-name: ${cluster_name}
    openstack:
      auth-url: ${auth_url}
      user-id: ${user_id}
      password: ${password}
      project-id: ${project_id}
      region: ${region}
    octavia:
      subnet-id: ${subnet_id}
      floating-network-id: ${public_net_id}
EOF
kubectl apply -f /etc/kubernetes/octavia-ingress-controller/config.yaml
```

Here are several other config options are not included in the example configuration above:

- Options for connecting to the kubernetes cluster. The configuration above will leverage the service account credential which is going to be injected into the pod automatically(see more details [here](https://kubernetes.io/docs/tasks/access-application-cluster/access-cluster/#accessing-the-api-from-a-pod)). However, there may be some reasons to specify the configuration explicitly.  

    ```yaml
    kubernetes:
      api-host: https://127.0.0.1:6443
      kubeconfig: /home/ubuntu/.kube/config
    ```

- Options for security group management. The octavia-ingress-controller creates an Octavia load balancer per Ingress and adds the worker nodes as members of the load balancer. In order for the Octavia amphorae talking to the Service NodePort, either the kubernetes cluster administrator manually manages the security group for the worker nodes or leave it to octavia-ingress-controller. For the latter case, you should config:

    ```yaml
    octavia:
      manage-security-groups: true
    ```

    Notes for the security group:

    - The security group name is in the format: `k8s_ing_<cluster-name>_<ingress-namespace>_<ingress-name>`
    - The security group description is in the format: `Security group created for Ingress <ingress-namespace>/<ingress-name> from cluster <cluster-name>`
    - The security group has tags: `["octavia.ingress.kubernetes.io", "<ingress-namespace>_<ingress-name>"]`
    - The security group is associated with all the Neutron ports of the Kubernetes worker nodes. 

### Deploy octavia-ingress-controller

```shell
image="docker.io/k8scloudprovider/octavia-ingress-controller:latest"

cat <<EOF > /etc/kubernetes/octavia-ingress-controller/deployment.yaml
---
kind: StatefulSet
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

Wait until the StatefulSet is up and running.

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

The following command defines an Ingress resource that forwards traffic that requests `http://api.sample.com/ping` to the `hostname-server` Service:
```bash
cat <<EOF | kubectl apply -f -
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: test-octavia-ingress
  annotations:
    kubernetes.io/ingress.class: "openstack"
    octavia.ingress.kubernetes.io/internal: "false"
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

Kubernetes creates an Ingress resource on your cluster. The octavia-ingress-controller service running inside the cluster is responsible for creating/maintaining the corresponding resources in Octavia to route all external HTTP traffic (on port 80) to the `hostname-server` NodePort Service you exposed.

> If you don't want your Ingress to be accessible from the public internet, you could change the annotation `octavia.ingress.kubernetes.io/internal` to true.

Verify that Ingress Resource has been created. Please note that the IP address for the Ingress Resource will not be defined right away (wait for the ADDRESS field to get populated):

```bash
$ kubectl get ing
NAME                   HOSTS            ADDRESS   PORTS     AGE
test-octavia-ingress   api.sample.com             80        12s
$ # Wait until the ingress gets an IP address
$ kubectl get ing
NAME                   HOSTS            ADDRESS      PORTS     AGE
test-octavia-ingress   api.sample.com   172.24.4.9   80        9m
```

For testing purpose, you can log into a host that could connect to the floating IP, you should be able to access the backend service by sending HTTP request to the domain name specified in the Ingress resource:

```shell
$ IPADDRESS=172.24.4.9
$ curl -H "Host: api.sample.com" http://$IPADDRESS/ping
hostname-server-698fd44fc8-jptl2
```
