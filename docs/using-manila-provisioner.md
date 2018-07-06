# Manila external provisioner
The Manila external provisioner fulfills persistent volume claims by creating Manila shares and mapping them to natively supported volume sources represented by Share backends.

## Share backends
A share backend handles share-specific tasks like granting access to the share and building a Persistent Volume source. Each share backend may need its own specific configuration, which is handled by Share options.

## Share options
Share options are parsed from Storage Class parameters.

**Common options**

Key | Required | Default value | Description
:------ | :------- | :------------ | :-----------
`type` | No | `default` |
`zones` | No | `nova` | Comma separated list of zones
`osSecretName` | Yes | None | Name of the Secret object containing OpenStack credentials
`osSecretNamespace` | Yes | None | Namespace of the Secret object
`protocol` | Yes | None | Protocol used when provisioning a share, options `CEPHFS`,`NFS`
`backend`  | Yes | None | Share backend used for granting access and creating `PersistentVolumeSource` options `cephfs`,`csi-cephfs`,`nfs`

**Protocol specific options**  
None.

**Share-backend specific options**

Key | For backend | For protocol  | Required | Default Value | Description
--- | ----------- | ------------- | ------------- | ----------- |---------
`csi-driver` | `csi-cephfs` | `CEPHFS` | Yes | None | Name of the CSI driver

## Authentication with Manila v2 client
The provisioner uses `gophercloud` library for talking to the OpenStack Manila service. Authentication credentials are read from Kubernetes Secret object which should contain the same credentials as your OpenRC file.

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
