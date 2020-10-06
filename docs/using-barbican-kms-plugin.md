<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [OpenStack Barbican KMS Plugin](#openstack-barbican-kms-plugin)
  - [Installation Steps:](#installation-steps)
    - [Verify](#verify)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# OpenStack Barbican KMS Plugin
Kubernetes supports to encrypt etcd data with various providers listed [here](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#providers), one of which is *kms*. The Kubernetes *kms provider* uses envelope encryption scheme. The data is encrypted using *DEK's* by kubernetes *kms provider*, *DEK's* are encrypted by *kms plugin* (e.g. barbican) using *KEK*. *Barbican-kms-plugin* uses *key* from barbican to encrypt/decrypt the *DEK's* as requested by kubernetes api server. 
The *KMS provider* uses gRPC to communicate with a specific *KMS plugin*.

It is recommended to read following kubernetes documents  

* [Encrypting Secret Data at Rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#verifying-that-data-is-encrypted)  
* [Using a KMS provider for data encryption](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)

## Installation Steps:
The following installation steps assumes that you have a Kubernetes cluster(v1.10+) running on OpenStack Cloud.

1. Create 256bit(32 byte) cbc key and store in barbican
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

2. Get the Key Id, It is the uuid in *Secret href*
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

3. Add the key-id in your cloud-config file
```
[Global]
username = <username>
password = <password>
domain-name = <domain-name>
auth-url =  <keystone-url>
tenant-id = <project-id>
region = <region>

[KeyManager]
key-id = <key-id>
```

4. Clone the cloud-provider-openstack repo and build the docker image for barbican-kms-plugin in architecture amd64
```
$ git clone https://github.com/kubernetes/cloud-provider-openstack.git $GOPATH/k8s.io/src/
$ cd $GOPATH/k8s.io/src/cloud-provider-openstack/
$ export ARCH=amd64
$ export VERSION=latest
$ make image-barbican-kms-plugin
```

5. Run the KMS Plugin in docker container
```
$ docker run -d --volume=/var/lib/kms:/var/lib/kms \
--volume=/etc/kubernetes:/etc/kubernetes \
-e socketpath=/var/lib/kms/kms.sock \
-e cloudconfig=/etc/kubernetes/cloud-config \
docker.io/k8scloudprovider/barbican-kms-plugin-amd64:latest
```
6. Create /etc/kubernetes/encryption-config.yaml
```
kind: EncryptionConfig
apiVersion: v1
resources:
  - resources:
    - secrets
    providers:
    - kms:
        name : barbican
        endpoint: unix:///var/lib/kms/kms.sock
        cachesize: 100
    - identity: {}
 ```
7. Enable --experimental-encryption-provider-config flag in kube-apiserver and restart it.
```
--experimental-encryption-provider-config=/etc/kubernetes/encryption-config.yaml
```

### Verify
[Verify the secret data is encrypted](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/#verifying-that-data-is-encrypted
)
