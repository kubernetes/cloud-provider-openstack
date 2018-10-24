# Auth data synchronization between Keystone and Kubernetes

## Overview

**Syncer** is an experimental feature allowing to create Kubernetes auth related entities during user authentication.

As an example, you can take the synchronization of Keystone projects with Kubernetes namespaces. If the user belongs to the project in Keystone, then, when attempting to authenticate in Kubernetes using [k8s-keystone-auth](./using-keystone-webhook-authenticator-and-authorizer.md) webhook for the first time, a Kubernetes namespace will automatically be created for him.

## Feature activation

To enable the feature two new arguments were added to the k8s-keystone-auth binary:

- ``--sync-config-file`` - points to a local file with sync configuration.
- ``--sync-configmap-name`` - defines a ConfigMap object containing the configuration. The ConfigMap must have ``syncConfig`` key containing config data.

Sync config file takes precedence over the ConfigMap, but the sync config definition will be refreshed based on the ConfigMap change on-the-fly. It is possible that both are not provided, in this case, the keystone webhook will not synchronize data.

For correct data synchronization the administrator must also provide a kube config file using ``--kubeconfig`` option. If the webhook was started in a pod, then system kube config will be used as a default.

## Available config options

This section describes the options that the administrator can specify in the config.
The configuration must be in yaml format.

### ``data_types_to_sync``

Defines a list of available data types, that the webhook will synchronize.

Available options are *projects* and *role_assignments*. In the first case the syncer will convert Keystone projects into Kubernetes namespaces. In the second - Keystone role assignments into Kubernetes RBAC role bindings.

To correctly create the roles bindings, the corresponding cluster roles, that describe the permissions of the user in the namespace, must be previously created by the administrator.
For example: if the user has a role assignment *member* in Keystone, then the syncer will try to create new role binding with the cluster role with the name *member*. If the role with this doesn't exist, then it will be ignored.

It is not recommended to use synchronization of role assignments with disabled project synchronization, because if the syncer can not find the project for the role, it is simply ignored.

Default: ["projects", "role_assignments"]

### ``project_black_list``

Contains a list of Keystone project ids, that should be excluded from synchronization.

Default: []

### ``namespace_format``

Defines a string with a format of namespace name after synchronization allowing to create more mnemonic names. The string may contain wildcards ``%d``, ``%n`` and ``%i`` representing keystone domain id, project name and project id respectively.

For example if the format string was set to ``prefix-%d-%n-%i-suffix``, then syncer will create a namespace as ``prefix-default-my_proj-2f240589c9e44a59836892bfa5abd698-suffix``.

By convention the namespace name must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character.

Due to the restriction of Kubernetes to the length of the name in 63 characters, if the generated string is longer than the specified limit, then only the keystone id will be used as the namespace name.

The string must contain ``%i`` wildcard. If this is absent the webhook won't start.

Default: "%i"

## Example of sync config file

Here is an example of sync configuration yaml file:

```yaml
# In format %d, %n and %i wildcards represent keystone domain id, project name and id respectively
namespace_format: "%n-%i"

# List of Keystone project ids to omit from syncing
projects_black_list: ["id1", "id2"]

# List of data types to synchronize
"data_types_to_sync": ["projects", "role_assignments"]
```

## Full Example using Keystone for Authentication and RBAC for Authorization and synced namespaces

Run k8s-keystone-auth like this:

```
k8s-keystone-auth \
--tls-cert-file /etc/kubernetes/pki/apiserver.crt \
--tls-private-key-file /etc/kubernetes/pki/apiserver.key \
--keystone-url https://keystone:5000/v3 \
--sync-config-file /etc/kubernetes/syncconfig.yaml
```

This way we use it only for authentication. It will print the following in the log:

```
W0809 15:23:26.718723       1 config.go:70] Argument --keystone-policy-file or --policy-configmap-name missing. Only keystone authentication will work. Use RBAC for authorization.
```

Create a ClusterRole like this:
```
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: _member_
rules:
- apiGroups: [""] # "" indicates the core API group
  resources: ["pods"]
  verbs: ["get", "watch", "list"]
```

Where the important bit of information is `_member_`: the string that represents the role of a user in keystone. If your role is called differently in your keystone setup, like `Member` or `member` you have to change this.

Configure the client to use `client-keystone-auth` as described [here].(https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-client-keystone-auth.md). You need a version higher than v0.2.0 to get keystone project scoped tokens.

Once a user makes the first API request, automatically a namespace is created, and also into that namespace a rolebinding is created per each keystone role of the user in that keystone project.

    kubectl get pods -n keystone-<project_uuid>

Check that also the rolebinding was automatically created. With your admin account use the command:

    kubectl get rolebinding -n keystone-<project_id>

You will find a rolebinding called `<user_uuid>__member_`.

Summarizing: the key idea is that a valid keystone user authenticates with a
project scoped token. Kubernetes will create a namespace and a rolebinding in
that namespace using the uuid value of the keystone project and of the keystone
role of the user in that project. The kubernetes admin should create a
ClusterRole in advance, giving permissions to users matching the name of the
keystone role.

Now depending on the keystone roles that you have, you need to implement your ClusterRole.

