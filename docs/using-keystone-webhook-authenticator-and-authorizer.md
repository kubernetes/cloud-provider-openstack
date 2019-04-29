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

## Running k8s-keystone-auth as a Kubernetes service

First, create a folder in which we will put all the manifest files and
config files.

```shell
$ mkdir -p /etc/kubernetes/keystone-auth
```

### Prepare the authorization policy

The authorization policy can be specified using an existing ConfigMap name in
the cluster, by doing this, the policy could be changed dynamically without the
k8s-keystone-auth service restart. The ConfigMap needs to be created before
running the k8s-keystone-auth service.

k8s-keystone-auth service supports two versions of policy definition.
Version 2 is recommended because of its better flexibility. However,
both versions are described in this guide. You can see more information
of version 2 in `Authorization policy definition(version 2)` section
below.

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
            "values": ["memberr"]
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

```shell
$ cat <<EOF > /etc/kubernetes/keystone-auth/policy-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: keystone-auth-policy
  namespace: kube-system
data:
  policies: |
    [
      {
        "users": {
          "projects": ["demo"],
          "roles": ["member"]
        },
        "resource_permissions": {
          "*/pods": ["get", "list", "watch"]
        }
      }
    ]
EOF
$ kubectl apply -f /etc/kubernetes/keystone-auth/policy-config.yaml
```

As you can see, the version 2 policy definition is much simpler and
more succinct.

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
needs to talk to the API server to query ConfigMap resources. You can either
specify a kubeconfig file or relies on the in-cluster configuration capability
to instantiate the kubernetes client, the latter approach is recommended.

Next, we create a new service account `keystone-auth` and grant the
cluster admin role to it.

```shell
$ cat <<EOF > /etc/kubernetes/keystone-auth/serviceaccount.yaml
---
kind: ServiceAccount
apiVersion: v1
metadata:
  name: keystone-auth
  namespace: kube-system
---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: keystone-auth
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: keystone-auth
    namespace: kube-system
EOF
$ kubectl apply -f /etc/kubernetes/keystone-auth/serviceaccount.yaml
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
$ keystone_auth_url="http://192.168.206.8/identity/v3"
$ image="k8scloudprovider/k8s-keystone-auth:latest"
$ cat <<EOF > /etc/kubernetes/keystone-auth/keystone-auth.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: keystone-auth
  namespace: kube-system
  labels:
    k8s-app: keystone-auth
spec:
  serviceName: keystone-auth
  replicas: 1
  selector:
    matchLabels:
      k8s-app: keystone-auth
  template:
    metadata:
      labels:
        k8s-app: keystone-auth
    spec:
      serviceAccountName: keystone-auth
      tolerations:
        - effect: NoSchedule # Make sure the pod can be scheduled on master kubelet.
          operator: Exists
        - key: CriticalAddonsOnly # Mark the pod as a critical add-on for rescheduling.
          operator: Exists
        - effect: NoExecute
          operator: Exists
      nodeSelector:
        node-role.kubernetes.io/master: ""
      containers:
        - name: keystone-auth
          image: ${image}
          imagePullPolicy: IfNotPresent
          args:
            - ./bin/k8s-keystone-auth
            - --tls-cert-file
            - /etc/kubernetes/pki/cert-file
            - --tls-private-key-file
            - /etc/kubernetes/pki/key-file
            - --policy-configmap-name
            - keystone-auth-policy
            - --keystone-url
            - ${keystone_auth_url}
            - --v
            - "2"
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
  name: keystone-auth
  namespace: kube-system
spec:
  selector:
    k8s-app: keystone-auth
  ports:
    - protocol: TCP
      port: 8443
      targetPort: 8443
EOF
$ kubectl apply -f /etc/kubernetes/keystone-auth/keystone-auth.yaml
```

### Test k8s-keystone-auth service

Before we continue to config kube-apiserver, we could test the
k8s-keystone-auth service by sending HTTP request directly to make sure
the service works as expected.

- Authentication

    Fetch a token of an OpenStack user from the `demo` project, send
    request to the k8s-keystone-auth service, in this example,
    `10.109.16.219` is the cluster IP of k8s-keystone-auth service.

    ```shell
    $ keystone_auth_service_addr=10.109.16.219
    $ token=...
    $ cat <<EOF | curl -ks -XPOST -d @- https://${keystone_auth_service_addr}:8443/webhook | python -mjson.tool
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

    You should see the detailed information of the Keystone user from the
    response if the service is configured correctly:

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
    does have `member` role associated:

    ```shell
    cat <<EOF | curl -ks -XPOST -d @- https://${keystone_auth_service_addr}:8443/webhook | python -mjson.tool
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

    According to the policy definition, pod creation should fail:

    ```shell
    cat <<EOF | curl -ks -XPOST -d @- https://${keystone_auth_service_addr}:8443/webhook | python -mjson.tool
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

Now the k8s-keystone-auth service works as expected, we could go ahead to
config kubernetes API server to use the k8s-keystone-auth service as a webhook
service for both authentication and authorization. In fact, the
k8s-keystone-auth service can be used for authentication or authorization only,
and both as well, depending on your requirement.

### Configuration on K8S master for authentication and authorization

- Create webhook config file. We reuse the folder `/etc/kubernetes/pki/`
  because it's already mounted and accessible by API server pod.

    ```shell
    cat <<EOF > /etc/kubernetes/pki/webhookconfig.yaml
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
  authentication and authorization.

    ```shell
    sed -i '/image:/ i \ \ \ \ - --authentication-token-webhook-config-file=/etc/kubernetes/pki/webhookconfig.yaml' /etc/kubernetes/manifests/kube-apiserver.yaml
    sed -i '/image:/ i \ \ \ \ - --authorization-webhook-config-file=/etc/kubernetes/pki/webhookconfig.yaml' /etc/kubernetes/manifests/kube-apiserver.yaml
    sed -i "/authorization-mode/c \ \ \ \ - --authorization-mode=Node,Webhook,RBAC" /etc/kubernetes/manifests/kube-apiserver.yaml
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
  roles they have.
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
    curl -SL# https://api.nz-por-1.catalystcloud.io:8443/v1/AUTH_b23a5e41d1af4c20974bf58b4dff8e5a/lingxian-public/client-keystone-auth -o ~/client-keystone-auth
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
  name: openstackuser@kubernetes
current-context: default
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
