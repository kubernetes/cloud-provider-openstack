# OpenStack Barbican KMS Plugin

Please Read the following documents to familizarize yourself with Encryption at REST
https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#providers     
https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/    

Kubernetes Supports many providers(https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#providers) for Encrypting secret data at REST. From Kubernetes v1.10 support for KMS encryption provider is also added. KMS encryption provider encrypt using a data encryption key (DEK), The DEKs are encrypted with a key encryption key (KEK) that is stored and managed in a remote KMS. The KMS provider uses gRPC to communicate with a specific KMS plugin. 

Barbican KMS Plugin is the grpc server implemenation for KMS provider.

## Prerequisites:
Kubernetes Cluster v1.10+ on OpenStack Cloud

### Installation:

### Create Key
* create 256bit(32 byte) cbc key and store in barbican
openstack secret order create --name k8s_key2 --algorithm aes --mode cbc --bit-length 256 key  --payload-content-type=application/octet-stream key
+----------------+----------------------------------------------------------------------+
| Field          | Value                                                                |
+----------------+----------------------------------------------------------------------+
| Order href     | http://localhost:9311/v1/orders/e477a578-4a46-4c3f-b071-79e220207b0e |
| Type           | Key                                                                  |
| Container href | N/A                                                                  |
| Secret href    | None                                                                 |
| Created        | None                                                                 |
| Status         | None                                                                 |
| Error code     | None                                                                 |
| Error message  | None                                                                 |
+----------------+----------------------------------------------------------------------+


* get the key-id
openstack secret order get http://localhost:9311/v1/orders/e477a578-4a46-4c3f-b071-79e220207b0e
 +----------------+-----------------------------------------------------------------------+
| Field          | Value                                                                 |
+----------------+-----------------------------------------------------------------------+
| Order href     | http://localhost:9311/v1/orders/e477a578-4a46-4c3f-b071-79e220207b0e  |
| Type           | Key                                                                   |
| Container href | N/A                                                                   |
| Secret href    | http://localhost:9311/v1/secrets/b5309dfb-b326-4148-b0ad-e9cd1ec223a8 |
| Created        | 2018-10-10T06:29:56+00:00                                             |
| Status         | ACTIVE                                                                |
| Error code     | None                                                                  |
| Error message  | None                                                                  |
+----------------+-----------------------------------------------------------------------+

* Add the key-id in your cloud-config file (config file depends on how you created k8s cluster on OpenStack)
/etc/kubernetes/cloud-config
[KeyManager]
key-id=b5309dfb-b326-4148-b0ad-e9cd1ec223a8

* Create encryption-config.yaml
cat encryption-config.yaml

* Enable --experimental-encryption-provider-config flag in kube-apiserver and point to encryption-config.yaml 

* start barbican kms plugin using
TODO: create pod yamls, docker file
kubectl create -f k8s.io/cloud-provider-openstack/manifests/barbican-kms/

### Run Example to verify
* Test encryption
kubectl.sh create secret generic secret1 -n default --from-literal=mykey=mydata
kubectl.sh get secret secret1 -o yaml
echo 'bXlkYXRh' | base64 --decode
