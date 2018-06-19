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
