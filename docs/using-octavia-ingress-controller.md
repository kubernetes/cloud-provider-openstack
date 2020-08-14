<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Get started with octavia-ingress-controller for Kubernetes](#get-started-with-octavia-ingress-controller-for-kubernetes)
  - [What is an Ingress Controller?](#what-is-an-ingress-controller)
  - [Why octavia-ingress-controller](#why-octavia-ingress-controller)
  - [Requirements](#requirements)
  - [Deploy octavia-ingress-controller in the Kubernetes cluster](#deploy-octavia-ingress-controller-in-the-kubernetes-cluster)
    - [Create service account and grant permissions](#create-service-account-and-grant-permissions)
    - [Prepare octavia-ingress-controller configuration](#prepare-octavia-ingress-controller-configuration)
    - [Deploy octavia-ingress-controller](#deploy-octavia-ingress-controller)
  - [Setting up HTTP Load Balancing with Ingress](#setting-up-http-load-balancing-with-ingress)
    - [Create a backend service](#create-a-backend-service)
    - [Create an Ingress resource](#create-an-ingress-resource)
  - [Enable TLS encryption](#enable-tls-encryption)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Get started with octavia-ingress-controller for Kubernetes

This guide explains how to deploy and config the octavia-ingress-controller in Kubernetes cluster on top of OpenStack cloud.

> NOTE: octavia-ingress-controller is still in Beta, support for the overall feature will not be dropped, though details may change.

## What is an Ingress Controller?
In Kubernetes, Ingress allows external users and client applications access to HTTP services. Ingress consists of two components.

- [Ingress Resource](https://kubernetes.io/docs/concepts/services-networking/ingress/#the-ingress-resource) is a collection of rules for the inbound traffic to reach Services. These are Layer 7 (L7) rules that allow hostnames (and optionally paths) to be directed to specific Services in Kubernetes.
- [Ingress Controller](https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-controllers) which acts upon the rules set by the Ingress Resource, typically via an HTTP or L7 load balancer.

It is vital that both pieces are properly configured to route traffic from an outside client to a Kubernetes Service.

## Why octavia-ingress-controller
After creating a Kubernetes cluster, the most common way to expose the application to the outside world is to use [LoadBalancer](https://kubernetes.io/docs/concepts/services-networking/service/#type-loadbalancer) type service. In the OpenStack cloud, Octavia(LBaaS v2) is the default implementation of LoadBalancer type service, as a result, for each LoadBalancer type service, there is a load balancer created in the cloud tenant account. We could see some drawbacks of this way:

- The cost of Kubernetes Service is a little bit high if it's one-to-one mapping from the service to Octavia load balancer, the customers have to pay for a load balancer per exposed service, which can get expensive.
- There is no filtering, no routing, etc. for the service. This means you can send almost any kind of traffic to it, like HTTP, TCP, UDP, Websockets, gRPC, or whatever.
- The traditional ingress controllers(such as NGINX ingress controller,  HAProxy ingress controller, Traefik ingress controller, etc.) don't make much sense in the cloud environment because they still rely on the cloud load balancing service to expose themselves behind a Service of LoadBalancer type, not to mention the overhead of managing the extra softwares.

The octavia-ingress-controller could solve all the above problems in the OpenStack environment by creating a single load balancer for multiple [NodePort](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport) type services in an Ingress. In order to use the octavia-ingress-controller in Kubernetes cluster, set the annotation `kubernetes.io/ingress.class` in the `metadata` section of the Ingress resource as shown below:

```yaml
annotations:
 kubernetes.io/ingress.class: "openstack"
```

## Requirements

octavia-ingress-controller implementation relies on load balancer management by OpenStack Octavia service, so:

- Communication between octavia-ingress-controller and Octavia is needed.
- Octavia stable/queens or higher version is required because of some needed features such as bulk pool members operation.
- OpenStack Key Manager(Barbican) service is required for TLS Ingress, otherwise Ingress creation will fail.

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
      domain-name: ${domain-name}
      username: ${user_id}
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
  serviceName: octavia-ingress-controller
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
Create a simple web service that is listening on a HTTP server on port 8080.

```bash
$ cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
name: webserver
namespace: default
labels:
  app: webserver
spec:
replicas: 1
selector:
  matchLabels:
    app: webserver
template:
  metadata:
    labels:
      app: webserver
  spec:
    containers:
    - name: webserver
      image: lingxiankong/alpine-test
      imagePullPolicy: IfNotPresent
      ports:
        - containerPort: 8080
EOF
$ kubectl expose deployment webserver --type=NodePort --target-port=8080
$ kubectl get svc
NAME         TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)          AGE
webserver    NodePort    10.105.129.150   <none>        8080:32461/TCP   9h
```

When you create a Service of type NodePort, Kubernetes makes your Service available on a randomly selected high port number (e.g. 32461) on all the nodes in your cluster. Generally the Kubernetes nodes are not externally accessible by default, creating this Service does not make your application accessible from the Internet. However, we could verify the service using its `CLUSTER-IP` on Kubernetes master node:

```bash
$ curl http://10.105.129.150:8080
webserver-58fcfb75fb-dz5kn
```

Next, we create an Ingress resource to make your HTTP web server application publicly accessible.

### Create an Ingress resource

The following command defines an Ingress resource that forwards traffic that requests `http://foo.bar.com/ping` to the webserver:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: test-octavia-ingress
  annotations:
    kubernetes.io/ingress.class: "openstack"
    octavia.ingress.kubernetes.io/internal: "false"
spec:
  rules:
  - host: foo.bar.com
    http:
      paths:
      - path: /ping
        backend:
          serviceName: webserver
          servicePort: 8080
EOF
```

Kubernetes creates an Ingress resource on your cluster. The octavia-ingress-controller service running inside the cluster is responsible for creating/maintaining the corresponding resources in Octavia to route all external HTTP traffic (on port 80) to the `webserver` NodePort Service you exposed.

> If you don't want your Ingress to be accessible from the public internet, you could set the annotation `octavia.ingress.kubernetes.io/internal` to true.

Verify that Ingress Resource has been created. Please note that the IP address for the Ingress Resource will not be defined right away (wait for the ADDRESS field to get populated):

```bash
$ kubectl get ing
NAME                   CLASS    HOSTS         ADDRESS   PORTS   AGE
test-octavia-ingress   <none>   foo.bar.com             80      12s
$ # Wait until the ingress gets an IP address
$ kubectl get ing
NAME                   CLASS    HOSTS         ADDRESS          PORTS   AGE
test-octavia-ingress   <none>   foo.bar.com   103.197.62.239   80      25s
```

For testing purpose, you can log into a host that could connect to the floating IP, you should be able to access the backend service by sending HTTP request to the domain name specified in the Ingress resource:

```shell
$ IPADDRESS=103.197.62.239
$ curl -H "Host: foo.bar.com" http://$IPADDRESS/ping
webserver-58fcfb75fb-dz5kn
```

## Enable TLS encryption

In the example below, we are going generate TLS certificates and keys for the
Ingress and enable the more secure HTTPS protocol.

1. Generate server TLS certificate and key for foo.bar.com using a
    self-signed CA. When generating certificates using the script, just use
    simple password e.g. 1234, the passphrase is removed from the private key
    in the end. In production, it's recommended to use [Let's
    Encrypt](https://letsencrypt.org/) or other certificate authorities to
    generate real server certificates and keys. 

    ```shell
    $ ll
    total 0
    $ curl -SLO https://gist.github.com/lingxiankong/47aa743de380a1f122a900d39cff02b3/raw/f7886bedeb615bef2964775b7ca67a38552180c3/gen_certs.sh
      % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                    Dload  Upload   Total   Spent    Left  Speed
      0     0    0     0    0     0      0      0 --:--:-- --:--:-- --:--:--     0
    100   966  100   966    0     0   1588      0 --:--:-- --:--:-- --:--:--  1588
    $ bash gen_certs.sh
    Enter your server domain [foo.bar.com]:
    Create CA cert(self-signed) and key...
    Create server key...
    Enter pass phrase for .key:
    Verifying - Enter pass phrase for .key:
    Remove password...
    Enter pass phrase for .key:
    Create server certificate signing request...
    Sign SSL certificate...
    Succeed!
    $ ll
    total 24
    -rw-rw-r-- 1 stack stack 1346 Feb 14 15:38 ca.crt
    -rw------- 1 stack stack 1704 Feb 14 15:38 ca.key
    -rw-rw-r-- 1 stack stack  966 Feb 14 15:38 gen_certs.sh
    -rw-rw-r-- 1 stack stack 1038 Feb 14 15:38 foo.bar.com.crt
    -rw-rw-r-- 1 stack stack  672 Feb 14 15:38 foo.bar.com.csr
    -rw------- 1 stack stack  887 Feb 14 15:38 foo.bar.com.key
    ```

1. Create Kubernetes secret using the certificates created.

    ```shell script
    kubectl create secret tls tls-secret --cert foo.bar.com.crt --key foo.bar.com.key
    ```

1. Create a TLS Ingress and wait for it's allocated the IP address.

    ```shell script
    cat <<EOF | kubectl apply -f -
    ---
    apiVersion: networking.k8s.io/v1beta1
    kind: Ingress
    metadata:
      name: test-octavia-ingress
      annotations:
        kubernetes.io/ingress.class: "openstack"
        octavia.ingress.kubernetes.io/internal: "false"
    spec:
      backend:
        serviceName: default-http-backend
        servicePort: 80
      tls:
        - secretName: tls-secret
      rules:
        - host: foo.bar.com
          http:
            paths:
            - path: /ping
              backend:
                serviceName: webserver
                servicePort: 8080
    EOF
    $ kubectl get ing
    NAME                   HOSTS             ADDRESS        PORTS     AGE
    test-octavia-ingress   foo.bar.com       172.24.5.178   80, 443   2m55s
    ```

1. Verify we could send HTTPS request to the Ingress address.

    ```shell script
    $ ip=172.24.5.178
    $ curl --cacert ca.crt --resolve foo.bar.com:443:$ip https://foo.bar.com/ping
    webserver-58fcfb75fb-dz5kn
    ```

> NOTE: octavia-ingress-controller currently doesn't support to integrate with
`cert-manager` to create the non-existing secret dynamically. Could be improved
in the future.
