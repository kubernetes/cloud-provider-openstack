# k8s-keystone-auth

Proof-Of-Concept : Kubernetes webhook authentication and authorization for OpenStack Keystone

Steps to use this webook with Kubernetes

- Save the following into webhook.kubeconfig.
```
apiVersion: v1
clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: https://localhost:8443/webhook
  name: webhook
contexts:
- context:
    cluster: webhook
    user: webhook
  name: webhook
current-context: webhook
kind: Config
preferences: {}
users:
- name: webhook
```

## Configuration on K8s master for authentication

- Add the following flags to your Kubernetes api server.
  * `--authentication-token-webhook-config-file=/path/to/your/webhook.kubeconfig`
  * `--authorization-mode=Node,RBAC`
- Start webhook process with the following flags
  * `--tls-cert-file /var/run/kubernetes/serving-kube-apiserver.crt`
  * `--tls-private-key-file /var/run/kubernetes/serving-kube-apiserver.key`
  * `--keystone-policy-file examples/webhook/policy.json`
  * `--keystone-url https://my.keystone:5000/v3`

## Configuration on K8s master for authorization

- Copy the examples/webhook/policy.json and edit it to your needs.
- Add the following flags to your Kubernetes api server.
  * `--authorization-mode=Node,Webhook,RBAC --authorization-webhook-config-file=/path/to/your/webhook.kubeconfig`
- When you start the webhook process make sure you also have the following flags (in addition to the flags in the case of authentication)
  * `--keystone-policy-file examples/webhook/policy.json`

## K8s kubectl Client configuration

### Old kubectl clients

- Run `openstack token issue` to generate a token
- Run `kubectl --token $TOKEN get po` or `curl -k -v -XGET  -H "Accept: application/json" -H "Authorization: Bearer $TOKEN" https://localhost:6443/api/v1/namespaces/default/pods`

### kubectl clients from v1.8.0 to v1.10.x

The client is able to read the `OS_` env variables used also by the openstackclient. You don't have to pass a token with `--token`, but the client will contact Keystone directly, will get a token and will use it. To configure the client do the following:

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

Source your env vars. Make sure you include `OS_DOMAIN_NAME` or the client will fallback to Keystone V2 that is not supported by the webhook.This env should be ok:

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

Client auth providers are deprecated in v1.11.0 and to be removed in the next version. The recommended way of client authentication is to use ``exec`` mode with the ``client-keystone-auth`` binary.

To configure the client do the following:

- Run `kubectl config set-credentials openstackuser`

This command creates the following entry in your ~/.kube/config

```
- name: openstackuser
  user: {}
```

To enable ``exec`` mode you have to manually edit the file and add the following lines to the entry:

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

In above kubeconfig, the cluster name is `mycluster`, the kube API address is `https://172.24.4.6:6443`. And in this kubeconfig file, there are two contexts.
One for normal certs auth, and one for Keystone auth. Please note, the current context is `openstackuser@kubernetes`.

Next you have several ways to specify additional auth parameters:

1. Source your env vars. Make sure you include `OS_DOMAIN_NAME` or the client will fallback to Keystone V2 that is not supported by the webhook. This env should be ok:

  ```
  OS_AUTH_URL="https://keystone.example.com:5000/v3"
  OS_DOMAIN_NAME="default"
  OS_PASSWORD="mysecret"
  OS_USERNAME="username"
  ```

2. Specify auth parameters in the ~/.kube/config file. For more information read [client keystone auth configuaration doc](./using-client-keystone-auth.md) and
[credential plugins documentation](https://kubernetes.io/docs/admin/authentication/#client-go-credential-plugins)

3. Use the interactive mode. If auth parameters are not specified initially, neither as env variables, nor the ~/.kube/config file, the user will be prompted to enter them from keyboard at the time of the interactive session.

To test that everything works as expected try: `kubectl get pods`

In case you are using this Webhook just for the authentication, you should get an authorization error:

```
Error from server (Forbidden): pods is forbidden: User "username" cannot list pods in the namespace "default"
```

You need to configure the RBAC with roles to be authorized to do something, for example:

``` kubectl create rolebinding username-view --clusterrole view --user username --namespace default```

Try now again to see the pods with `kubectl get pods`

## References

More details about Kubernetes Authentication Webhook using Bearer Tokens is at :
https://kubernetes.io/docs/admin/authentication/#webhook-token-authentication

and the Authorization Webhook is at:
https://kubernetes.io/docs/admin/authorization/webhook/

## Tips

- You can directly test the webhook with
```
cat << EOF | curl -kvs -XPOST -d @- https://localhost:8443/webhook | python -mjson.tool
{
	"apiVersion": "authentication.k8s.io/v1beta1",
	"kind": "TokenReview",
	"metadata": {
		"creationTimestamp": null
	},
	"spec": {
		"token": "$TOKEN"
	}
}
EOF

cat << EOF | curl -kvs -XPOST -d @- https://localhost:8443/webhook | python -mjson.tool
{
	"apiVersion": "authorization.k8s.io/v1beta1",
	"kind": "SubjectAccessReview",
	"spec": {
		"resourceAttributes": {
			"namespace": "kittensandponies",
			"verb": "get",
			"group": "unicorn.example.org",
			"resource": "pods"
		},
		"user": "jane",
		"group": [
			"group1",
			"group2"
		]
	}
}
EOF
```
