<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Authentication synchronization between Keystone and Kubernetes](#authentication-synchronization-between-keystone-and-kubernetes)
  - [Overview](#overview)
  - [Configuration](#configuration)
  - [Example of sync config file](#example-of-sync-config-file)
  - [Full example using Keystone for Authentication and Kubernetes RBAC for Authorization](#full-example-using-keystone-for-authentication-and-kubernetes-rbac-for-authorization)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Authentication synchronization between Keystone and Kubernetes

## Overview

Authentication synchronization is an experimental feature allowing to create Kubernetes auth related entities during user authentication.

As an example, you can take the synchronization of Keystone projects with Kubernetes namespaces. If the user belongs to the project in Keystone, then, when attempting to authenticate in Kubernetes using [k8s-keystone-auth](./using-keystone-webhook-authenticator-and-authorizer.md) webhook for the first time, a Kubernetes namespace will automatically be created for the user.

Another typical use case is, the Kubernetes cluster admin wants to config RBAC based on Keystone roles, e.g. all users with *member* role should have access to resources of *default* namespace, see the configuration example below.

## Configuration

To enable the feature two new arguments were added to the k8s-keystone-auth binary:

- `--sync-config-file` - points to a local config file.
- `--sync-configmap-name` - defines a ConfigMap object containing the configuration. The ConfigMap must have `syncConfig` key containing config data.

Sync config file takes precedence over the ConfigMap, but the sync config definition will be refreshed based on the ConfigMap change on-the-fly. It is possible that both are not provided, in this case, the keystone webhook will not synchronize data.

The config options:

* **role-mappings**

  A list of role mappings that apply to the user identity after authentication, works with Keystone authentication webhook. This option could be used alone without all others. This allows the cluster admin to config RBAC based on Keystone roles, which is more Kubernetes-native than using the policy definition in the Keystone authorization webhook. The supported keys are: keystone-role, username, groups. See a full example below.

* **data-types-to-sync**

  Defines a list of available data types, that the webhook will synchronize. Default: []

  Available options are *projects* and *role_assignments*. In the first case the webhook will convert Keystone projects into Kubernetes namespaces. In the second, the webhook creates Kubernetes RBAC *rolebindings* based on the Keystone role names.

  To correctly create the *rolebindings*, the cluster admin should create the *clusterrole* (same with Keystone role name) first. For example, if the user *alice* has a role assignment *member* in Keystone, when *alice* accesses the cluster, before Kubernetes actually does authorization, the webhook should create a new *rolebinding* in the new namespace with the *clusterrole* name *member* and the user *alice*. As a result, the user *alice* should have some pre-defined resource permissions even it's the first time to access the cluster.

* **projects-blacklist**

  Contains a list of Keystone project ids, that should be excluded from synchronization. Default: []

* **projects-name-blacklist**

  Contains a list of Keystone project names, that should be excluded from synchronization. Default: []

* **namespace-format**

  Defines a string with a format of namespace name after synchronization allowing to create more mnemonic names. The string may contain wildcards ``%d``, ``%n`` and ``%i`` representing keystone domain id, project name and project id respectively. Default: "%i"

  For example if the format string was set to ``prefix-%d-%n-%i-suffix``, then syncer will create a namespace as ``prefix-default-my_proj-2f240589c9e44a59836892bfa5abd698-suffix``.

  By convention the namespace name must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character.

  Due to the restriction of Kubernetes to the length of the name in 63 characters, if the generated string is longer than the specified limit, then only the keystone id will be used as the namespace name.

  The string must contain ``%i`` wildcard. If this is absent the webhook won't start.

## Example of sync config file

Here is an example of sync configuration *configmap*:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: keystone-sync-policy
  namespace: kube-system
data:
  syncConfig: |
    data_types_to_sync:
      - projects
      - role_assignments
    namespace_format: "%i"
    role-mappings:
      - keystone-role: member
        username: myuser
        groups: ["mytest"]
```

## Full example using Keystone for Authentication and Kubernetes RBAC for Authorization

* Make sure you have deployed k8s-keystone-auth webhook server by following [k8s-keystone-auth installation guide](./using-keystone-webhook-authenticator-and-authorizer.md). However, we are going to use Kubernetes RBAC for authorization, so remove the `--authorization-webhook-config-file` option for *kube-apiserver* service and make sure `--authorization-mode=Node,RBAC`. Restart *kube-apiserver* as needed.

* Assuming your k8s-keystone-auth webhook server is running in *kube-system* namespace. As the cluster admin, create a *configmap* that assign the group *project-1-group* to the user who has been assigned *member* role in Keystone:

  ```
  cat <<EOF | kubectl apply -f -
  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: keystone-sync-policy
    namespace: kube-system
  data:
    syncConfig: |
      role-mappings:
        - keystone-role: member
          groups: ["project-1-group"]
  EOF
  ```

* Change k8s-keystone-auth configuration by adding `--sync-configmap-name`, specify the *configmap* name created above, e.g.

  ```
  k8s-keystone-auth \
  --tls-cert-file /etc/kubernetes/pki/apiserver.crt \
  --tls-private-key-file /etc/kubernetes/pki/apiserver.key \
  --keystone-url https://keystone:5000/v3 \
  --sync-configmap-name keystone-sync-policy
  ```

  Restart the service as needed.

  > NOTE: After --sync-configmap-name configured, the configmap keystone-sync-policy could be changed on-the-fly, k8s-keystone-auth is able to pick up the change automatically.

* As the cluster admin, create RBAC *roles* and *rolebindings*, e.g. we want to allow users in the group *project-1-group* to access *pods* in the namespace *project-1*. 

  ```
  kubectl create ns project-1
  kubectl -n project-1 create role pod-reader --verb=get,list --resource=pods
  kubectl -n project-1 create rolebinding pod-reader --role=pod-reader --group=project-1-group
  ```

* Let's summarize what we have configured so far, according to the configuration, when the Keystone user *alice* with role *member* trying to access the Kubernetes cluster, she could list the *pods* in the *project-1* namespace, but nothing else.

  ```shell
  $ # Switch to OpenStack user.
  $ kubectl get po
  Error from server (Forbidden): pods is forbidden: User "alice" cannot list resource "pods" in API group "" in the namespace "default"
  $ kubectl get ns
  Error from server (Forbidden): namespaces is forbidden: User "alice" cannot list resource "namespaces" in API group "" at the cluster scope
  $ kubectl -n project-1 get pod
  No resources found.
  $ kubectl -n project-1 get deployment
  Error from server (Forbidden): deployments.extensions is forbidden: User "alice" cannot list resource "deployments" in API group "extensions" in the namespace "project-1"
  ```
