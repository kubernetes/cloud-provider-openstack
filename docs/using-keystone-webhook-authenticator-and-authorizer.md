<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [k8s-keystone-auth](#k8s-keystone-auth)
  - [Prerequisites](#prerequisites)
  - [Deploy k8s-keystone-auth webhook server](#deploy-k8s-keystone-auth-webhook-server)
    - [Prepare the authorization policy (optional)](#prepare-the-authorization-policy-optional)
      - [Non-resource permission](#non-resource-permission)
      - [Sub-resource permission](#sub-resource-permission)
    - [Prepare the service certificates](#prepare-the-service-certificates)
    - [Create service account for k8s-keystone-auth](#create-service-account-for-k8s-keystone-auth)
    - [Deploy k8s-keystone-auth](#deploy-k8s-keystone-auth)
    - [Test k8s-keystone-auth service](#test-k8s-keystone-auth-service)
    - [Configuration on K8S master for authentication and/or authorization](#configuration-on-k8s-master-for-authentication-andor-authorization)
  - [Authorization policy definition(version 2)](#authorization-policy-definitionversion-2)
  - [Client(kubectl) configuration](#clientkubectl-configuration)
    - [Old kubectl clients](#old-kubectl-clients)
    - [kubectl clients from v1.8.0 to v1.10.x](#kubectl-clients-from-v180-to-v110x)
    - [New kubectl clients from v1.11.0 and later](#new-kubectl-clients-from-v1110-and-later)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# k8s-keystone-auth

[Kubernetes webhook authentication and authorization](https://kubernetes.io/docs/reference/access-authn-authz/webhook/)
for OpenStack Keystone. With k8s-keystone-auth, the Kubernetes cluster
administrator only need to know the OpenStack project names or roles,
it's up to the OpenStack project admin for user management, as a result,
the OpenStack users could have access to the Kubernetes cluster.

The k8s-keystone-auth can be running either as a static pod(controlled
by kubelet) or a normal kubernetes service.

## Prerequisites

- You already have a Kubernetes cluster(version >= 1.9.3) up and running and you
  have the admin permission for the cluster.
- You have an OpenStack environment and admin credentials.

> If you run k8s-keystone-auth service as a static pod, the pod creation could
  be a part of kubernetes cluster initialization process.

## Deploy k8s-keystone-auth webhook server

### Prepare the authorization policy (optional)

> The authorization feature is optional, you can choose to deploy k8s-keystone-auth webhook server for authentication only and rely on Kubernetes RBAC for authorization. See more details [here](./using-auth-data-synchronization.md). However, k8s-keystone-auth authorization provides more flexible configurations than Kubernetes native RBAC.

The authorization policy can be specified using an existing ConfigMap name in
the cluster, by doing this, the policy could be changed dynamically without the
k8s-keystone-auth service restart. The ConfigMap needs to be created before
running the k8s-keystone-auth service.

k8s-keystone-auth service supports two versions of policy definition.
Version 2 is recommended because of its better flexibility. However,
both versions are described in this guide. You can see more information
of version 2 in [Authorization policy definition(version
2)](#authorization-policy-definitionversion-2).

For testing purpose, in the following ConfigMap, we only allow the users in
project `demo` with `member` role in OpenStack to query the Pods information
from all the namespaces. We create the ConfigMap in `kube-system` namespace
because we will also run k8s-keystone-auth service there.

Version 1:

```shell
$ cat <<EOF > /etc/kubernetes/keystone-auth/policy-config.yaml
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
            "values": ["member"]
          },
          {
            "type": "project",
            "values": ["demo"]
          }
        ]
      }
    ]
EOF
$ kubectl apply -f /etc/kubernetes/keystone-auth/policy-config.yaml
```
Version 2:

please refer to [configmap](../examples/webhook/keystone-policy-configmap.yaml) for the definition of 
policy config map.
```shell
$ kubectl apply -f examples/webhook/keystone-policy-configmap.yaml
```

As you can see, the version 2 policy definition is much simpler and
more succinct.

####  Non-resource permission

For many scenarios clients require access to `nonresourse` paths.
`nonresource` paths include: `/api`, `/apis`, `/metrics`, `/resetMetrics`,
`/logs`, `/debug`, `/healthz`, `/swagger-ui/`, `/swaggerapi/`, `/ui`, and
`/version`. Clients require access to `/api`, `/api/*`, `/apis`, `/apis/*`,
and `/version` to discover what resources and versions are present on the
server. Access to other `nonresource` paths can be disallowed without
restricting access to the REST api.

#### Sub-resource permission

In order to describe subresource (e.g `logs` or `exec`) of a certain resource
(e.g. `pod`)it is possible to use `/` in order to combine resource and
subresource. This is similar to the way resources described in `rules` list
of k8s `Role` object.

For an example of using of subresources as well as `nonresourse` paths please
see policy below. With this policy we only want to allow client to be able to
`kubectl exec` into pod and only in `utility` namespace. For this purpose we
define resource as `"resources": ["pods/exec"]`. But in order for client to be
able to discover pods and versions as mentioned above we also need to allow
read access to `nonresource` paths `/api`, `/api/*`, `/apis`, `/apis/*`.
At this moment only one path (type string) is supported per `nonresource` json
object, this is why we have entry for each of them.

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
        "nonresource": {
          "verbs": ["get"],
          "path": "/api"
        },
        "match": [
          {
            "type": "role",
            "values": ["utility_exec"]
          }
        ]
      },
      {
        "nonresource": {
          "verbs": ["get"],
          "path": "/api/*"
        },
        "match": [
          {
            "type": "role",
            "values": ["utility_exec"]
          }
        ]
      },
      {
        "nonresource": {
          "verbs": ["get"],
          "path": "/apis"
        },
        "match": [
          {
            "type": "role",
            "values": ["utility_exec"]
          }
        ]
      },
      {
        "nonresource": {
          "verbs": ["get"],
          "path": "/apis/*"
        },
        "match": [
          {
            "type": "role",
            "values": ["utility_exec"]
          }
        ]
      },
      {
        "resource": {
          "verbs": ["create"],
          "resources": ["pods/exec"],
          "version": "*",
          "namespace": "utility"
        },
        "match": [
          {
            "type": "role",
            "values": ["utility_exec"]
          }
        ]
      }
    ]
EOF
```

### Prepare the service certificates

For security reasons, the k8s-keystone-auth service is running as an HTTPS
service, so the TLS certificates need to be configured. This example uses
a self-signed certificate, but for a production cluster it is important to use
certificates signed by a trusted issuer.


```shell
$ openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes -subj /CN=k8s-keystone-auth.kube-system/
$ kubectl --namespace kube-system create secret tls keystone-auth-certs --cert=cert.pem --key=key.pem
```

### Create service account for k8s-keystone-auth

In order to support dynamic policy configuration, the k8s-keystone-auth service
needs to talk to the API server to query ConfigMap resources. You can either
specify a kubeconfig file or relies on the in-cluster configuration capability
to instantiate the kubernetes client, the latter approach is recommended.

Next, we create a new service account `keystone-auth` and grant the
cluster admin role to it. please refer to [rbac](../examples/webhook/keystone-rbac.yaml)
for definition of the rbac such as clusterroles and rolebinding.

```shell
$ kubectl apply -f examples/webhook/keystone-rbac.yaml
```

### Deploy k8s-keystone-auth

Now we are ready to create the k8s-keystone-auth deployment and expose
it as a service. There are several things we need to notice in the
deployment manifest:

- We are using the official nightly-built image
  `k8scloudprovider/k8s-keystone-auth:latest`
- We use `k8s-auth-policy` configmap created above.
- The pod uses service account `keystone-auth` created above.
- We use `keystone-auth-certs` secret created above to inject the
  certificates into the pod.
- The value of `keystone_auth_url` needs to be changed according to your
  environment.

```shell
$ kubectl apply -f examples/webhook/keystone-deployment.yaml
$ kubectl apply -f examples/webhook/keystone-service.yaml
```

### Test k8s-keystone-auth service

- Check k8s-keystone-auth webhook pod.

  First we need check if the k8s-keystone-auth pod is up and running:

  ```shell
  $ kubectl get pods --all-namespaces
  NAMESPACE     NAME                        READY   STATUS    RESTARTS   AGE
  kube-system   k8s-keystone-auth           1/1     Running   0          2m27s
  kube-system   kube-dns-547db76c8f-6wf49   3/3     Running   0          7m42s
  ```

  Before we continue to config kube-apiserver, we could test the
  k8s-keystone-auth service by sending HTTP request directly to make sure
  the service works as expected.

- Authentication

  Get a token of an OpenStack user from the `demo` project, send
  request to the k8s-keystone-auth service. Since this service is only exposed
  within the cluster, run a temporary pod within the kube-system namespace to
  access the webhook endpoint.

  ```shell
  $ token=...
  $ kubectl --namespace kube-system run --rm --restart=Never --attach=true \
    --image curlimages/curl curl -- \
    -ks -XPOST https://k8s-keystone-auth.kube-system:8443/webhook -d '
  {
    "apiVersion": "authentication.k8s.io/v1beta1",
    "kind": "TokenReview",
    "metadata": {
      "creationTimestamp": null
    },
    "spec": {
      "token": "'$token'"
    }
  }'
  ```

  You should see the detailed information of the Keystone user from the
  response if the service is configured correctly. You may notice that besides the user's Keystone group, the user's
  project ID is also included in the *group* field, so the cluster admin could config RBAC *rolebindings* based on the
  groups without involving the webhook authorization.

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
                      "load-balancer_member"
                  ],
                  "alpha.kubernetes.io/identity/user/domain/id": [
                      "default"
                  ],
                  "alpha.kubernetes.io/identity/user/domain/name": [
                      "Default"
                  ]
              },
              "groups": [
                  "mygroup",
                  "423d41d3a02f4b77b4a9bbfbc3a1b3c6"
              ],
              "uid": "ff369be2cbb14ee9bb775c0bcf2a1061",
              "username": "demo"
          }
      }
  }
  ```

- Authorization (optional)

  > Please skip this validation if you are using Kubernetes RBAC for authorization.

  From the above response,  we know the `demo` user in the `demo` project
  does have `member` role associated:

  ```shell
  $ kubectl --namespace kube-system run --rm --restart=Never --attach=true \
    --image curlimages/curl curl -- \
    -ks -XPOST https://k8s-keystone-auth.kube-system:8443/webhook -d '
  {
    "apiVersion": "authorization.k8s.io/v1beta1",
    "kind": "SubjectAccessReview",
    "spec": {
      "resourceAttributes": {
        "namespace": "default",
        "verb": "get",
        "group": "",
        "resource": "pods",
        "name": "pod1"
      },
      "user": "demo",
      "group": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
      "extra": {
          "alpha.kubernetes.io/identity/project/id": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
          "alpha.kubernetes.io/identity/project/name": ["demo"],
          "alpha.kubernetes.io/identity/roles": ["load-balancer_member","member"]
      }
    }
  }'
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

  According to the policy definition, pod creation should fail:

  ```shell
  $ kubectl --namespace kube-system run --rm --restart=Never --attach=true \
    --image curlimages/curl curl -- \
    -ks -XPOST https://k8s-keystone-auth.kube-system:8443/webhook -d '
  {
    "apiVersion": "authorization.k8s.io/v1beta1",
    "kind": "SubjectAccessReview",
    "spec": {
      "resourceAttributes": {
        "namespace": "default",
        "verb": "create",
        "group": "",
        "resource": "pods",
        "name": "pod1"
      },
      "user": "demo",
      "group": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
      "extra": {
          "alpha.kubernetes.io/identity/project/id": ["423d41d3a02f4b77b4a9bbfbc3a1b3c6"],
          "alpha.kubernetes.io/identity/project/name": ["demo"],
          "alpha.kubernetes.io/identity/roles": ["load-balancer_member","member"]
      }
    }
  }'
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

Now the k8s-keystone-auth service works as expected, we could go ahead to
config kubernetes API server to use the k8s-keystone-auth service as a webhook
service for both authentication and authorization. In fact, the
k8s-keystone-auth service can be used for authentication or authorization only,
and both as well, depending on your requirement. In this example,
`10.109.16.219` is the cluster IP of k8s-keystone-auth service.

### Configuration on K8S master for authentication and/or authorization

- Create the webhook config file.

    ```shell
    keystone_auth_service_addr=10.109.16.219
    mkdir /etc/kubernetes/webhooks
    cat <<EOF > /etc/kubernetes/webhooks/webhookconfig.yaml
    ---
    apiVersion: v1
    kind: Config
    preferences: {}
    clusters:
      - cluster:
          insecure-skip-tls-verify: true
          server: https://${keystone_auth_service_addr}:8443/webhook
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

- Modify kube-apiserver config file to use the webhook service for
  authentication and/or authorization.

  Authentication:

  ```
  --authentication-token-webhook-config-file=/etc/kubernetes/webhooks/webhookconfig.yaml
  ```

  Authorization (optional):

  ```
  --authorization-webhook-config-file=/etc/kubernetes/webhooks/webhookconfig.yaml
  --authorization-mode=Node,Webhook,RBAC
  ```

  Also mount the new webhooks directory:

  ```
  containers:
  ...
    volumeMounts:
    ...
    - mountPath: /etc/kubernetes/webhooks
        name: webhooks
        readOnly: true
  volumes:
  ...
  - hostPath:
      path: /etc/kubernetes/webhooks
      type: DirectoryOrCreate
    name: webhooks
  ```

- Wait for the API server to restart successfully until you can see all the
  pods are running in `kube-system` namespace.

## Authorization policy definition(version 2)

The version 2 definition could be used together with version 1 but will
take precedence over version 1 if both are defined. The version 1
definition is still supported but may be considered deprecated in the future.

The authorization policy definition is based on whitelist, which means
the operation is allowed if *ANY* rule defined in the permissions is
satisfied.

- "users" defines which projects the OpenStack users belong to and what
  roles they have. You could define multiple projects or roles, if the project
  of the target user is included in the projects, the permission is going to be
  checked.
- "resource_permissions" is a map with the key defines namespaces and
  resources, the value defines the allowed operations. `/` is used as separator
  for namespace and resource. `!` and `*` are supported both for namespaces and
  resources, see examples below.
- "nonresource_permissions" is a map with the key defines the
  non-resource endpoint such as `/healthz`, the value defines the
  allowed operations.

Some examples:

- Any operation is allowed on any resource in any namespace.

    ```json
    "resource_permissions": {
      "*/*": ["*"]
    }
    ```

- Only "get" and "list" are allowed for Pods in the "default" namespace.

    ```json
    "resource_permissions": {
      "default/pods": ["get", "list"]
    }
    ```

- "create" is allowed for any resource except Secrets and ClusterRoles
  in the "default" namespace.

    ```json
    "resource_permissions": {
      "default/!['secrets', 'clusterroles']": ["create"]
    }
    ```

- Any operation is allowed for any resource in any namespace except
  "kube-system".

    ```json
    "resource_permissions": {
      "!kube-system/*": ["*"]
    }
    ```

- Any operation is allowed for any resource except Secrets and
  ClusterRoles in any namespace except "kube-system".

    ```json
    "resource_permissions": {
      "!kube-system/!['secrets', 'clusterroles']": ["*"]
    }
    ```

## Client(kubectl) configuration

If the k8s-keystone-auth service is configured for both authentication and
authorization, make sure your OpenStack user in the following steps has the
`member` role in Keystone as defined above, otherwise listing pod operation
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

- Download the client-keystone-auth binary(maintained by Lingxian Kong), you can also build the binary by yourself.

    ```
    curl https://github.com/kubernetes/cloud-provider-openstack/releases/latest/download/client-keystone-auth -o ~/client-keystone-auth
    sudo chmod u+x ~/client-keystone-auth
    ```

- Run `kubectl config set-credentials openstackuser`, this command creates the
  following entry in the `~/.kube/config` file.

    ```
    - name: openstackuser
      user: {}
    ```

- Config kubectl to use client-keystone-auth binary for the user
  `openstackuser`. We assume `mycluster` is the cluster name defined in
  `~/.kube/config`.

    ```
    sed -i '/user: {}/ d' ~/.kube/config
    cat <<EOF >> ~/.kube/config
      user:
        exec:
          command: "/home/ubuntu/client-keystone-auth"
          apiVersion: "client.authentication.k8s.io/v1beta1"
    EOF
    kubectl config set-context --cluster=mycluster --user=openstackuser openstackuser@mycluster
    ```

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
  name: openstackuser@mycluster
current-context: openstackuser@mycluster
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
      command: "/home/ubuntu/client-keystone-auth"
      apiVersion: "client.authentication.k8s.io/v1alpha1"
```

In above kubeconfig, the cluster name is `mycluster`, the kube API address is
`https://172.24.4.6:6443`. And in this kubeconfig file, there are two contexts.
One for normal certs auth, and one for Keystone auth.

Next you have several ways to specify additional auth parameters:

1. Source your env vars(recommended). Make sure you include `OS_DOMAIN_NAME`
   otherwise the client will fallback to Keystone V2 that is not supported by
   the webhook.

    ```
    export OS_AUTH_URL="https://keystone.example.com:5000/v3"
    export OS_DOMAIN_NAME="default"
    export OS_PASSWORD="mysecret"
    export OS_USERNAME="username"
    export OS_PROJECT_NAME="demo"
    ```

2. Specify auth parameters in the `~/.kube/config` file. For more information
   read
   [client keystone auth configuaration doc](./using-client-keystone-auth.md)
   and
   [credential plugins documentation](https://kubernetes.io/docs/admin/authentication/#client-go-credential-plugins)
3. Use the interactive mode. If auth parameters are not specified initially,
   neither as env variables, nor the `~/.kube/config` file, the user will be
   prompted to enter them from keyboard at the time of the interactive session.

To test that everything works as expected try:
```
kubectl get pods --context openstackuser@mycluster
```

In case you are using this Webhook just for the authentication, you should get
an authorization error:

```
Error from server (Forbidden): pods is forbidden: User "username" cannot list pods in the namespace "default"
```

You need to configure the RBAC with roles to be authorized to do something, for
example:

```
kubectl create rolebinding username-view --clusterrole view --user username --namespace default
```

Try now again and you should see the pods.
