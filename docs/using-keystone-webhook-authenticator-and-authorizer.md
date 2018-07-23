# k8s-keystone-auth

Kubernetes webhook authentication and authorization for OpenStack Keystone.

The k8s-keystone-auth can be running either as a static pod(controlled by
kubelet) or a normal kubernetes service.

## Prerequisites

- You already have an available kubernetes cluster(version >= 1.9.3) and you
  have the admin permission for the cluster.
- You have an OpenStack environment and admin credentials.

> If you run k8s-keystone-auth service as a static pod, the pod creation could
  be a part of kubernetes cluster initialization process.

## Running k8s-keystone-auth as a Kubernetes service

### Prepare the authorization policy

The authorization policy can be specified using an existing configmap name in
the cluster, by doing this, the policy could be changed dynamically without the
k8s-keystone-auth service restart. We need to create the configmap before
running the k8s-keystone-auth service.

Currently, k8s-keystone-auth service supports four types of policies:

- user. The Keystone user ID or name.
- project. The Keystone project ID or name.
- role. The user role defined in Keystone.
- group. The group is not a Keystone concept actually, it's supported for
  backward compatibility, you can use group as project ID.

For testing purpose, in the following configmap, we only allow the users in
project `demo` with `k8s-viewer` role in OpenStack to query the pod information
from all the namespaces. We create the configmap in `kube-system` namespace
because we will also run k8s-keystone-auth service there.

```shell
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: k8s-auth-policy
  namespace: kube-system
data:
  policies: |
    [
      {
        "resource": {
          "verbs": ["get", "list", "watch"],
          "resources": ["pods"],
          "version": "*",
          "namespace": "default"
        },
        "match": [
          {
            "type": "role",
            "values": ["k8s-viewer"]
          },
          {
            "type": "project",
            "values": ["demo"]
          }
        ]
      }
    ]
EOF
```

### Prepare the service certificates

For security reason, the k8s-keystone-auth service is running as an HTTPS
service, so the TLS certificates need to be configured. For testing purpose, we
are about to reuse the API server certificates, it's recommended to create new
ones in production environment though.

```shell
kubectl create secret generic keystone-auth-certs \
  --from-file=cert-file=/etc/kubernetes/pki/apiserver.crt \
  --from-file=key-file=/etc/kubernetes/pki/apiserver.key \
  -n kube-system
```

### Create service account for k8s-keystone-auth

In order to support dynamic policy configuration, the k8s-keystone-auth service
needs to talk to the API server to query configmap resources. You can either
specify a kubeconfig file or relies on the in-cluster configuration capability
to instantiate the kubernetes client, the latter approach is commended.

For testing purpose, we reuse `kube-system:default` service account and grant
the cluster admin role to the service account.

```shell
kubectl create clusterrolebinding default-cluster-admin \
  --clusterrole=cluster-admin \
  --serviceaccount=kube-system:default
```

### Create k8s-keystone-auth service

Now we are ready to create the k8s-keystone-auth deployment and expose it as a
service. There are several things we need to notice in the deployment manifest:

- We are using the official nightly-built image
  `k8scloudprovider/k8s-keystone-auth`
- We use `k8s-auth-policy` configmap created above.
- The pod will use `kube-system:default` by default, you need to specify
  `serviceAccount` explicitly in the pod definition if you have created a new
  one.
- We use `keystone-auth-certs` secret created above to pass the certificates to
  the container.
- The value of `--keystone-url` needs to be changed according to your
  environment.

```shell
cat <<EOF | kubectl create -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: k8s-keystone-auth
  namespace: kube-system
  labels:
    app: k8s-keystone-auth
spec:
  replicas: 2
  selector:
    matchLabels:
      app: k8s-keystone-auth
  template:
    metadata:
      labels:
        app: k8s-keystone-auth
    spec:
      containers:
        - name: k8s-keystone-auth
          image: k8scloudprovider/k8s-keystone-auth
          imagePullPolicy: Always
          args:
            - ./bin/k8s-keystone-auth
            - --tls-cert-file
            - /etc/kubernetes/pki/cert-file
            - --tls-private-key-file
            - /etc/kubernetes/pki/key-file
            - --policy-configmap-name
            - k8s-auth-policy
            - --keystone-url
            - http://10.140.81.86/identity/v3
          volumeMounts:
            - mountPath: /etc/kubernetes/pki
              name: k8s-certs
              readOnly: true
          ports:
            - containerPort: 8443
      volumes:
      - name: k8s-certs
        secret:
          secretName: keystone-auth-certs
---
kind: Service
apiVersion: v1
metadata:
  name: k8s-keystone-auth-service
  namespace: kube-system
spec:
  selector:
    app: k8s-keystone-auth
  ports:
    - protocol: TCP
      port: 8443
      targetPort: 8443
EOF
```

### Test k8s-keystone-auth service

Before we continue to config k8s API server, we could test the
k8s-keystone-auth service by sending HTTP request directly on the kubernetes
master node to make sure the service works as expected.

- Authentication

  Fetch a token of any user from OpenStack, send request to the
  k8s-keystone-auth service, `10.109.16.219` is the cluster IP of
  k8s-keystone-auth service.

  ```shell
  cat <<EOF | curl -ks -XPOST -d @- https://10.109.16.219:8443/webhook | python -mjson.tool
  {
    "apiVersion": "authentication.k8s.io/v1beta1",
    "kind": "TokenReview",
    "metadata": {
      "creationTimestamp": null
    },
    "spec": {
      "token": "$token"
    }
  }
  EOF
  ```

  You can see the detailed information of the Keystone user from the response
  if the service is configured correctly:

  ```shell
  {
      "apiVersion": "authentication.k8s.io/v1beta1",
      "kind": "TokenReview",
      "metadata": {
          "creationTimestamp": null
      },
      "spec": {
          "token": "<truncated>"
      },
      "status": {
          "authenticated": true,
          "user": {
              "extra": {
                  "alpha.kubernetes.io/identity/project/id": [
                      "423d41d3a02f4b77b4a9bbfbc3a1b3c6"
                  ],
                  "alpha.kubernetes.io/identity/project/name": [
                      "demo"
                  ],
                  "alpha.kubernetes.io/identity/roles": [
                      "member",
                      "load-balancer_member",
                      "reader",
                      "anotherrole"
                  ],
                  "alpha.kubernetes.io/identity/user/domain/id": [
                      "default"
                  ],
                  "alpha.kubernetes.io/identity/user/domain/name": [
                      "Default"
                  ]
              },
              "groups": [
                  "423d41d3a02f4b77b4a9bbfbc3a1b3c6"
              ],
              "uid": "ff369be2cbb14ee9bb775c0bcf2a1061",
              "username": "demo"
          }
      }
  }
  ```

- Authorization

  From the above response,  we know the `demo` user in the `demo` project
  doesn't have `k8s-viewer` role associated, so the authorization will fail if
  we construct the authorization request using the information returned above:

  ```shell
  cat <<EOF | curl -ks -XPOST -d @- https://10.109.16.219:8443/webhook | python -mjson.tool
  {
    "apiVersion": "authorization.k8s.io/v1beta1",
    "kind": "SubjectAccessReview",
    "spec": {
      "resourceAttributes": {
        "namespace": "default",
        "verb": "get",
        "group": "",
        "resource": "pods"
      },
      "user": "demo",
      "group": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
      "extra": {
          "alpha.kubernetes.io/identity/project/id": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
          "alpha.kubernetes.io/identity/project/name": ["demo"],
          "alpha.kubernetes.io/identity/roles": ["load-balancer_member","member", "reader", "anotherrole"]
      }
    }
  }
  EOF
  ```

  Response:

  ```shell
  {
      "apiVersion": "authorization.k8s.io/v1beta1",
      "kind": "SubjectAccessReview",
      "status": {
          "allowed": false
      }
  }
  ```

  But if we manually add `k8s-viewer` role to the roles list of the request,
  the authorization should pass:

  ```shell
  cat <<EOF | curl -ks -XPOST -d @- https://10.109.16.219:8443/webhook | python -mjson.tool
  {
    "apiVersion": "authorization.k8s.io/v1beta1",
    "kind": "SubjectAccessReview",
    "spec": {
      "resourceAttributes": {
        "namespace": "default",
        "verb": "get",
        "group": "",
        "resource": "pods"
      },
      "user": "demo",
      "group": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
      "extra": {
          "alpha.kubernetes.io/identity/project/id": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
          "alpha.kubernetes.io/identity/project/name": ["demo"],
          "alpha.kubernetes.io/identity/roles": ["load-balancer_member","member", "reader", "anotherrole", "k8s-viewer"]
      }
    }
  }
  EOF
  ```
  Response:

  ```shell
  {
      "apiVersion": "authorization.k8s.io/v1beta1",
      "kind": "SubjectAccessReview",
      "status": {
          "allowed": true
      }
  }
  ```

Now the k8s-keystone-auth service works as expected, we could go ahead to
config kubernetes API server to use the k8s-keystone-auth service as a webhook
service for both authentication and authorization. In fact, the
k8s-keystone-auth service can be used for authentication or authorization only,
and both as well, depending on your requirement.

### Configuration on K8S master for authentication and authorization

- Create webhook config file. `10.109.16.219` is the cluster IP of
  k8s-keystone-auth service. We reuse the folder `/etc/kubernetes/pki/` because
  it's already mounted and accessible by API server pod.

  ```shell
  cat <<EOF > /etc/kubernetes/pki/webhookconfig.yaml
  ---
  apiVersion: v1
  kind: Config
  preferences: {}
  clusters:
    - cluster:
        insecure-skip-tls-verify: true
        server: https://10.109.16.219:8443/webhook
      name: webhook
  users:
    - name: webhook
  contexts:
    - context:
        cluster: webhook
        user: webhook
      name: webhook
  current-context: webhook
  EOF
  ```

- Modify API server config file to use the webhook service for authentication.

  ```shell
  sed -i '/image:/ i \ \ \ \ - --authentication-token-webhook-config-file=/etc/kubernetes/pki/webhookconfig.yaml' /etc/kubernetes/manifests/kube-apiserver.yaml
  ```

- Modify API server config file to use the webhook service for authorization.

  ```shell
  sed -i '/image:/ i \ \ \ \ - --authorization-webhook-config-file=/etc/kubernetes/pki/webhookconfig.yaml' /etc/kubernetes/manifests/kube-apiserver.yaml
  sed -i "/authorization-mode/c \ \ \ \ - --authorization-mode=Node,Webhook,RBAC" /etc/kubernetes/manifests/kube-apiserver.yaml
  ```

- Wait for the API server to restart successfully until you can get all the
  pods in `kube-system` namespace by running `kubectl get pod -n kube-system`

## Client(kubectl) configuration

If the k8s-keystone-auth service is configured for both authentication and
authorization, make sure your OpenStack user in the following steps has the
`k8s-viewer` role in Keystone as defined above, otherwise listing pod operation
will fail.

### Old kubectl clients

- Run `openstack token issue` to generate a token
- Run `kubectl --token $TOKEN get po` or `curl -k -v -XGET  -H "Accept: application/json" -H "Authorization: Bearer $TOKEN" https://localhost:6443/api/v1/namespaces/default/pods`

### kubectl clients from v1.8.0 to v1.10.x

The client is able to read the `OS_` env variables used also by the
openstackclient. You don't have to pass a token with `--token`, but the client
will contact Keystone directly, will get a token and will use it. To configure
the client do the following:

- Run `kubectl config set-credentials openstackuser --auth-provider=openstack`

This command creates the following entry in your ~/.kube/config
```
- name: openstackuser
  user:
    as-user-extra: {}
    auth-provider:
      name: openstack
```
- Run `kubectl config set-context --cluster=mycluster --user=openstackuser openstackuser@kubernetes`
- Run `kubectl config use-context openstackuser@kubernetes` to activate the context

After running above commands, your kubeconfig file should be like below:

```
apiVersion: v1
clusters:
- cluster:
    certificate-authority: /tmp/certs/ca.pem
    server: https://172.24.4.6:6443
  name: mycluster
contexts:
- context:
    cluster: mycluster
    user: admin
  name: default
- context:
    cluster: mycluster
    user: openstackuser
  name: openstackuser@kubernetes
current-context: openstackuser@kubernetes
kind: Config
preferences: {}
users:
- name: admin
  user:
    client-certificate: /tmp/certs/cert.pem
    client-key: /tmp/certs/key.pem
- name: openstackuser
  user:
    auth-provider:
      config:
        ttl: 10m0s
      name: openstack

```

In above kubeconfig, the cluster name is `mycluster`, the kube API address is
`https://172.24.4.6:6443`. And in this kubeconfig file, there are two contexts.
One for normal certs auth, and one for Keystone auth. Please note, the current
context is `openstackuser@kubernetes`.

Source your env vars. Make sure you include `OS_DOMAIN_NAME` or the client will
fallback to Keystone V2 that is not supported by the webhook.This env should be
ok:

```
OS_AUTH_URL="https://keystone.example.com:5000/v3"
OS_DOMAIN_NAME="default"
OS_IDENTITY_API_VERSION="3"
OS_PASSWORD="mysecret"
OS_PROJECT_NAME="myproject"
OS_REGION_NAME="myRegion"
OS_USERNAME="username"
```
- Try: `kubectl get pods`

### New kubectl clients from v1.11.0 and later

Client auth providers are deprecated in v1.11.0 and to be removed in the next
version. The recommended way of client authentication is to use ``exec`` mode
with the ``client-keystone-auth`` binary.

To configure the client do the following:

- Run `kubectl config set-credentials openstackuser`

This command creates the following entry in your ~/.kube/config

```
- name: openstackuser
  user: {}
```

To enable ``exec`` mode you have to manually edit the file and add the
following lines to the entry:

```
- name: openstackuser
  user:
    exec:
      command: "/path/to/client-keystone-auth"
      apiVersion: "client.authentication.k8s.io/v1alpha1"
```

And then

- Run `kubectl config set-context --cluster=mycluster --user=openstackuser openstackuser@kubernetes`
- Run `kubectl config use-context openstackuser@kubernetes` to activate the context

After running above commands, your kubeconfig file should be like below:

```
apiVersion: v1
clusters:
- cluster:
    certificate-authority: /tmp/certs/ca.pem
    server: https://172.24.4.6:6443
  name: mycluster
contexts:
- context:
    cluster: mycluster
    user: admin
  name: default
- context:
    cluster: mycluster
    user: openstackuser
  name: openstackuser@kubernetes
current-context: openstackuser@kubernetes
kind: Config
preferences: {}
users:
- name: admin
  user:
    client-certificate: /tmp/certs/cert.pem
    client-key: /tmp/certs/key.pem
- name: openstackuser
  user:
    exec:
      command: "/path/to/client-keystone-auth"
      apiVersion: "client.authentication.k8s.io/v1alpha1"
```

In above kubeconfig, the cluster name is `mycluster`, the kube API address is
`https://172.24.4.6:6443`. And in this kubeconfig file, there are two contexts.
One for normal certs auth, and one for Keystone auth. Please note, the current
context is `openstackuser@kubernetes`.

Next you have several ways to specify additional auth parameters:

1. Source your env vars. Make sure you include `OS_DOMAIN_NAME` or the client
   will fallback to Keystone V2 that is not supported by the webhook. This env
   should be ok:

    ```
    OS_AUTH_URL="https://keystone.example.com:5000/v3"
    OS_DOMAIN_NAME="default"
    OS_PASSWORD="mysecret"
    OS_USERNAME="username"
    ```

2. Specify auth parameters in the ~/.kube/config file. For more information
   read [client keystone auth configuaration doc](./using-client-keystone-auth.md)
   and [credential plugins documentation](https://kubernetes.io/docs/admin/authentication/#client-go-credential-plugins)

3. Use the interactive mode. If auth parameters are not specified initially,
   neither as env variables, nor the ~/.kube/config file, the user will be
   prompted to enter them from keyboard at the time of the interactive session.

To test that everything works as expected try: `kubectl get pods`

In case you are using this Webhook just for the authentication, you should get
an authorization error:

```
Error from server (Forbidden): pods is forbidden: User "username" cannot list pods in the namespace "default"
```

You need to configure the RBAC with roles to be authorized to do something, for
example:

``` kubectl create rolebinding username-view --clusterrole view --user username --namespace default```

Try now again to see the pods with `kubectl get pods`

## References

More details about Kubernetes Authentication Webhook using Bearer Tokens is at :
https://kubernetes.io/docs/admin/authentication/#webhook-token-authentication

and the Authorization Webhook is at:
https://kubernetes.io/docs/admin/authorization/webhook/
