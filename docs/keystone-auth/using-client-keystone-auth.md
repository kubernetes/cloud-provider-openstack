<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Keystone client-go credential plugin usage](#keystone-client-go-credential-plugin-usage)
  - [Overview](#overview)
  - [Example use case](#example-use-case)
  - [Configuration](#configuration)
  - [Input and output formats](#input-and-output-formats)
  - [References](#references)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Keystone client-go credential plugin usage

## Overview

`k8s.io/client-go` and tools using it such as `kubectl` and `kubelet` are able to execute an
external command to receive user credentials.

This feature allows client side integrations with authentication using Keystone API, that is
not natively supported by `k8s.io/client-go`. The plugin implements the protocol specific logic,
then returns opaque credentials to use. This credential plugin use cases require a server side
component with support for the [webhook token authenticator](https://kubernetes.io/docs/admin/authentication/#webhook-token-authentication)
to interpret the credential format produced by the client plugin. The webhook authenticator is
provided by [k8s-keystone-auth binary](https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/using-keystone-webhook-authenticator-and-authorizer.md)

## Example use case

In a hypothetical use case, an organization would run an external service that exchanges Openstack Keystone
credentials for user specific, signed tokens. The service would also be capable of responding to [webhook token
authenticator](https://kubernetes.io/docs/admin/authentication/#webhook-token-authentication) requests to
validate the tokens. Users would be required to install a credential plugin on their workstation.

To authenticate against the API:

* The user issues a `kubectl` command.
* Credential plugin prompts the user for Keystone credentials, exchanges credentials with external service for a token.
* Credential plugin returns token to client-go, which uses it as a bearer token against the API server.
* API server uses the [webhook token authenticator](https://kubernetes.io/docs/admin/authentication/#webhook-token-authentication)
  to submit the token to the Keystone service.
* Keystone service verifies the token and returns the user's username and groups.

## Configuration

The credential plugin is configured through [`kubectl` config files](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/)
as part of the user fields.

```yaml
apiVersion: v1
kind: Config
users:
- name: my-user
  user:
    exec:
      # Command to execute. Required.
      command: "client-keystone-auth"

      # API version to use when encoding and decoding the ExecCredentials
      # resource. Required.
      #
      # The API version returned by the plugin MUST match the version encoded.
      apiVersion: "client.authentication.k8s.io/v1beta1"

      # Environment variables to set when executing the plugin. Optional.
      env:
      - name: "OS_USERNAME"
        value: "admin"
      - name: "OS_PASSWORD"
        value: "passw0rd"
      - name: "OS_PROJECT_NAME"
        value: "myproject"

      # Arguments to pass when executing the plugin. Optional.
      args:
      - "--domain-name=default"
      - "--keystone-url=https://127.0.0.1/identity"
clusters:
- name: my-cluster
  cluster:
    server: "https://172.17.4.100:6443"
    certificate-authority: "/etc/kubernetes/ca.pem"
contexts:
- name: my-cluster
  context:
    cluster: my-cluster
    user: my-user
current-context: my-cluster
```

Relative command paths are interpreted as relative to the directory of the config file. If
KUBECONFIG is set to `/home/jane/kubeconfig` and the exec command is `./bin/client-keystone-auth`,
the binary `/home/jane/bin/client-keystone-auth` is executed.

```yaml
- name: my-user
  user:
    exec:
      # Path relative to the directory of the kubeconfig
      command: "./bin/client-keystone-auth"
      apiVersion: "client.authentication.k8s.io/v1beta1"
```

## Input and output formats

The executed command prints an `ExecCredential` object to `stdout`. `k8s.io/client-go`
authenticates against the Kubernetes API using the returned credentials in the `status`.

When run from an interactive session, `stdin` is exposed directly to the plugin. The plugin uses a
[TTY check](https://godoc.org/golang.org/x/crypto/ssh/terminal#IsTerminal) to determine if it's
appropriate to prompt a user interactively.

When the plugin is executed from an interactive session, `stdin` and `stderr` are directly
exposed to the plugin so it can prompt the user for input for interactive logins.

To authenticate in Keystone from an interactive session, the user needs to provide the address
domain name, user name, password, and, optionally, a project name. These values can be specified
using environment variables
(`OS_AUTH_URL`, `OS_DOMAIN_NAME`, `OS_USERNAME`, `OS_PASSWORD` and `OS_PROJECT_NAME`), or through command
arguments
(`--keystone-url`, `--domain-name`, `--user-name`, `--password` and `--project-name`), respectively.
If they are not specified, the user will be prompted to enter them at the time of the interactive
session.
It is also possible to authenticate with [Keystone Application Credentials](https://docs.openstack.org/keystone/latest/user/application_credentials.html).
In this case, the user needs to provide the id or the name of the Application Credential, and the Secret.
The environment variables are `OS_APPLICATION_CREDENTIAL_ID`,`OS_APPLICATION_CREDENTIAL_NAME` and
`OS_APPLICATION_CREDENTIAL_SECRET` and the command line arguments are `--application-credential-name`,
`--application-credential-id` and `--application-credential-secret`.

When responding to a 401 HTTP status code (indicating invalid credentials), this object will
include metadata about the response.

```json
{
  "apiVersion": "client.authentication.k8s.io/v1beta1",
  "kind": "ExecCredential",
  "spec": {
    "response": {
      "code": 401,
      "header": {},
    },
    "interactive": true
  }
}
```

The executed command prints an `ExecCredential` to `stdout`. This objects contains a bearer
token as `token` and the expiry of the token formatted as a RFC3339 timestamp as `expirationTimestamp`.
`k8s.io/client-go` will then use the returned bearer token in the `status` when authenticating against the
Kubernetes API.

```json
{
  "apiVersion": "client.authentication.k8s.io/v1beta1",
  "kind": "ExecCredential",
  "status": {
    "token": "my-bearer-token",
    "expirationTimestamp": "2018-03-05T17:30:20-08:00"
  }
}
```

## References

More details about Kubernetes Authentication Webhook using Bearer Tokens is at :
https://kubernetes.io/docs/admin/authentication/#webhook-token-authentication

Client-go credential plugins documentation is at:
https://kubernetes.io/docs/admin/authentication/#client-go-credential-plugins
