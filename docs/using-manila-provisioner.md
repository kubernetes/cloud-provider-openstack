# Manila external provisioner
The Manila external provisioner fulfills persistent volume claims by provisioning Manila shares and mapping them to natively supported volume sources represented by Share backends.

Depending on supplied parameters, the Manila external provisioner is able to work in two modes of operation:
- dynamic provisioning: a new share along with a corresponding access rule will be created; the reclaim policy is honored when deleting the claim, i.e. the underlying share will be deleted if `reclaimPolicy: Delete`
- static provisioning: an existing share and its corresponding access rule will be used; deleting the claim leaves the supplied share and its access rule intact

Dynamic provisioning is the default mode. In order to use an existing Manila share, parameters [`osShareID` or `osShareName`] and `osShareAccessID` must be supplied.

## Share backends
A share backend handles share-specific tasks like granting access to the share and building a Persistent Volume source. Each share backend may need its own specific configuration, which is handled by Share options.

## Share options
Share options are parsed from Storage Class parameters.

**Common options**

Key | Required | Default value | Description
:------ | :------- | :------------ | :-----------
`type` | No | `default` |
`zones` | No | `nova` | Comma separated list of zones
`protocol` | Yes | None | Protocol used when provisioning a share, options `CEPHFS`,`NFS`
`backend`  | Yes | None | Share backend used for granting access and creating `PersistentVolumeSource` options `cephfs`,`csi-cephfs`,`nfs`
`osSecretName` | Yes | None | Name of the Secret object containing OpenStack credentials
`osSecretNamespace` | Yes | None | Namespace of the OpenStack credentials Secret object
`shareSecretNamespace` | No | value of `osSecretNamespace` | Namespace of the per-share Secret object (contains backend-specific secrets)
`osShareID` | No | None | The UUID of an existing share. Used during static provisioning
`osShareName` | No | None | The name of an exisiting share. Used during static provisioning
`osShareAccessID` | No | None | The UUID of an existing access rule to a share. Used during static provisioning


**Protocol specific options**  
None.

**Share-backend specific options**

Key | For backend | For protocol  | Required | Default Value | Description
--- | ----------- | ------------- | ------------- | ----------- |---------
`csi-driver` | `csi-cephfs` | `CEPHFS` | Yes | None | Name of the CSI driver

## Authentication with Manila v2 client
The provisioner authenticates to the OpenStack Manila service with the credentials supplied from the Kubernetes Secret object referenced by `osSecretNamespace` : `osSecretName`. One can authenticate either as a user or as a trustee, with each of those having its own set of parameters. Note that if the Secret object is created from a manifest, the Secret's values need to be encoded in base64.

Available Secret parameters: `os-authURL`, `os-region`, `os-certAuthority`, `os-TLSInsecure`, `os-userID`, `os-userName`, `os-password`, `os-projectID`, `os-projectName`, `os-domainID`, `os-domainName`, `os-trustID`, `os-trusteeID` and `os-trusteePassword`.

Parameters `os-authURL` and `os-region` are required for both user and trustee authentication.

Optionally, you can also supply a custom certificate via `os-certAuthority` secret parameter (PEM file contents). By default, the usual TLS verification is performed. To override this behaviour and accept insecure certificates, set `os-TLSInsecure: "true"` (optional, defaults to `false`).

The recommended way to create a Secret manifest with OpenStack credentials is following:
```bash
$ cd examples/manila-provisioner
$ ./generate-secrets.sh -n my-manila-secrets | ./filter-secrets.sh > secrets.yaml
$ kubectl create -f secrets.yaml
```
- `generate-secrets.sh` will read OpenStack variables from the environment and generate a Secrets manifest. It can also parse OpenRC file. Please consult the `-h` option for more details.
- `filter-secrets.sh` will filter duplicate fields: `*ID` fields will take precedence over `*Name` ones (e.g. `os-projectID` will be chosen over `os-projectName`) - otherwise `gophercloud` would report errors.

In order to reference the Secret object from Storage Class parameters `osSecretName` and `osSecretNamespace` need to be filled in.

Available Secret fields: `os-authURL`, `os-userID`, `os-userName`, `os-password`, `os-projectID`, `os-projectName`, `os-domainID`, `os-domainName`, `os-region`

### Adding a new Share backend
1. (optional) Add struct fields to `.../manila/shareoptions/backend.go` with appropriate field tags
2. Create a separate file in the `sharebackends` package with a struct implementing the `ShareBackend` interface
3. Register the backend in `.../manila/sharebackend.go` in `init()`
