# k8s-keystone-auth

Proof-Of-Concept : Kubernetes webhook authentication and authorization for OpenStack Keystone

Steps to use this webook with Kubernetes

- Save the following into webhook.kubeconfig and Add `--authentication-token-webhook-config-file=/path/to/your/webhook.kubeconfig` to your Kubernetes api server. 
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
- Start webhook process with `--tls-cert-file /var/run/kubernetes/serving-kube-apiserver.crt --tls-private-key-file /var/run/kubernetes/serving-kube-apiserver.key`
- Run `openstack token issue` to generate a token
- Run `kubectl --token $TOKEN get po` or `curl -k -v -XGET  -H "Accept: application/json" -H "Authorization: Bearer $TOKEN" https://localhost:6443/api/v1/namespaces/default/pods`

NOTE: I've tested this with `hack/local-up-cluster.sh` with RBAC disabled. Also, currently the hook is just for Authentication, not for Authorization.

More details about Kubernetes Authentication Webhook using Bearer Tokens is at :
https://kubernetes.io/docs/admin/authentication/#webhook-token-authentication

and the Authorization Webhook is at:
https://kubernetes.io/docs/admin/authorization/webhook/

Tips:

- You can directly test the webhook with
```
cat << EOF | curl -kvs -XPOST -d @- https://localhost:8443/webhook
{
	"apiVersion": "authentication.k8s.io/v1beta1",
	"kind": "TokenReview",
	"spec": {
		"token": $TOKEN"
	}
}
EOF
```
