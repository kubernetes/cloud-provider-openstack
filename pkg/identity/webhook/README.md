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
  * `--keystone-policy-file examples/policy.json`
  * `--keystone-url https://my.keystone:5000/v3`

## Configuration on K8s master for authorization

- Copy the examples/policy.json and edit it to your needs.
- Add the following flags to your Kubernetes api server.
  * `--authorization-mode=Webhook,Node --authorization-webhook-config-file=/path/to/your/webhook.kubeconfig`
- When you start the webhook process make sure you also have the following flags (in addition to the flags in the case of authentication)
  * `--keystone-policy-file examples/policy.json`

## K8s kubectl Client configuration

### Old kubectl clients

- Run `openstack token issue` to generate a token
- Run `kubectl --token $TOKEN get po` or `curl -k -v -XGET  -H "Accept: application/json" -H "Authorization: Bearer $TOKEN" https://localhost:6443/api/v1/namespaces/default/pods`

### New kubectl clients v1.8.0 and later

The client is able to read the `OS_` env variables used also by the openstackclient. You dont have to pass a token with `--token`, but the client will contact Keystone directly, will get a token and will use it. To configure the client to the following:

- Run `kubectl config set-credentials openstackuser --auth-provider=openstack`

This command creates the following entry in your ~/.kube/config
```
- name: openstackuser
  user:
    as-user-extra: {}
    auth-provider:
      name: openstack
```
- Run `kubectl config set-context --cluster=kubernetes --user=openstackuser openstackuser@kubernetes`
- Run `kubectl config use-context openstackuser@kubernetes` to activate the context

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
