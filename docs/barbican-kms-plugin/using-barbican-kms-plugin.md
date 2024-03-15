<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [OpenStack Barbican KMS Plugin](#openstack-barbican-kms-plugin)
  - [Installation Steps](#installation-steps)
    - [Verify](#verify)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# OpenStack Barbican KMS Plugin
Kubernetes supports encrypting etcd data with various providers listed [here](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#providers), one of which is *kms*. The Kubernetes *kms provider* uses an envelope encryption scheme. The data is encrypted using *DEK's* by kubernetes *kms provider*, *DEK's* are encrypted by *kms plugin* (e.g. barbican) using *KEK*. *Barbican-kms-plugin* uses *key* from barbican to encrypt/decrypt the *DEK's* as requested by kubernetes api server. 
The *KMS provider* uses gRPC to communicate with a specific *KMS plugin*.

It is recommended to read the following kubernetes documents  

* [Encrypting Secret Data at Rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#verifying-that-data-is-encrypted)  
* [Using a KMS provider for data encryption](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)


## Installation Steps

The following installation steps assumes that you have a Kubernetes cluster(v1.10+) running on OpenStack Cloud.


### Create 256-bit (32 bytes) CBC key and store in barbican

```
$ openstack secret order create --name k8s_key --algorithm aes --mode cbc --bit-length 256 --payload-content-type=application/octet-stream key
+----------------+-----------------------------------------------------------------------+
| Field          | Value                                                                 |
+----------------+-----------------------------------------------------------------------+
| Order href     | http://hostname:9311/v1/orders/e477a578-4a46-4c3f-b071-79e220207b0e  |
| Type           | Key                                                                  |
| Container href | N/A                                                                  |
| Secret href    | None                                                                 |
| Created        | None                                                                 |
| Status         | None                                                                 |
| Error code     | None                                                                 |
| Error message  | None                                                                 |
+----------------+----------------------------------------------------------------------+
```

### Get the key ID, it is the **uuid** in *Secret href*

```
$ openstack secret order get http://hostname:9311/v1/orders/e477a578-4a46-4c3f-b071-79e220207b0e
 +----------------+-----------------------------------------------------------------------+
| Field          | Value                                                                 |
+----------------+-----------------------------------------------------------------------+
| Order href     | http://hostname:9311/v1/orders/e477a578-4a46-4c3f-b071-79e220207b0e   |
| Type           | Key                                                                   |
| Container href | N/A                                                                   |
| Secret href    | http://hostname:9311/v1/secrets/b5309dfb-b326-4148-b0ad-e9cd1ec223a8  |
| Created        | 2018-10-10T06:29:56+00:00                                             |
| Status         | ACTIVE                                                                |
| Error code     | None                                                                  |
| Error message  | None                                                                  |
+----------------+-----------------------------------------------------------------------+
```


### Add the key ID in your cloud-config file

```toml
[Global]
username = "<username>"
password = "<password>"
domain-name = "<domain-name>"
auth-url = "<keystone-url>"
tenant-id = "<project-id>"
region = "<region>"

[KeyManager]
key-id = "<key-id>"
```


### Run the KMS Plugin in your cluster

This will provide a socket at `/var/lib/kms/kms.sock` on each of the control
plane nodes.
```
kubectl apply -f https://raw.githubusercontent.com/kubernetes/cloud-provider-openstack/master/manifests/barbican-kms/ds.yaml
```
*recommendation:* Use the tag corresponding to your Kubernetes release, for
example `release-1.29` for kubernetes version 1.29.


### Create encryption configuration

Create `/etc/kubernetes/encryption-config.yaml` on each of your control plane
nodes.
```yaml
kind: EncryptionConfig
apiVersion: v1
resources:
  - resources:
    - secrets
    providers:
    - kms:
        apiVersion: v2
        name: barbican
        endpoint: unix:///var/lib/kms/kms.sock
    - identity: {}
```


### Update the API server

On each of your control plane nodes, you need to edit the kube-apiserver, the
configuration is usually found at
`/etc/kubernetes/manifests/kube-apiserver.yaml`. You can just edit it and
kubernetes will eventually restart the pod with the new configuration.

Add the following volumes and volume mounts to the `kube-apiserver.yaml`
```yaml
spec:
  containers:
  - command:
    - kube-apiserver
    - --encryption-provider-config=/etc/kubernetes/encryption-config.yaml
    ...
    volumeMounts:
    - mountPath: /var/lib/kms/kms.sock
      name: kms-sock
    - mountPath: /etc/kubernetes/encryption.yaml
      name: encryption-config
      readOnly: true
  ...
  volumes:
  - hostPath:
      path: /var/lib/kms/kms.sock
      type: Socket
    name: kms-sock
  - hostPath:
      path: /etc/kubernetes/encryption.yaml
      type: File
    name: encryption-config
  ...
```


### Verify
[Verify that the secret data is encrypted](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#verifying-that-data-is-encrypted
)
