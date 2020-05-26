## General notes before continuing

1. Make sure you've completed all the steps in `docs/using-manila-csi-plugin.md`: e.g. you've deployed CSI NFS and CSI Manila plugins and CSI Manila is running with `--share-protocol-selector=NFS` and `--fwdendpoint=unix:///csi/csi-nfsplugin/csi.sock` (or similar, based on your environment)
2. Modify `secrets.yaml` to suite your OpenStack cloud environment. Refer to the _"Secrets, authentication"_ section of CSI Manila docs. You may also use helper scripts from `examples/manila-provisioner` to generate the Secrets manifest.
3. The same steps apply to all supported Manila share protocols
4. `exec-bash.sh`, `logs.sh` are convenience scripts for debugging CSI Manila

## Example CSI Manila usage with NFS shares

```
nfs/
├── dynamic-provisioning/
│   ├── pod.yaml
│   ├── pvc.yaml
│   └── --> storageclass.yaml <--
├── snapshot/
│   ├── pod.yaml
│   ├── --> snapshotclass.yaml <--
│   ├── snapshotcreate.yaml
│   └── snapshotrestore.yaml
├── static-provisioning/
│   ├── pod.yaml
│   └── --> preprovisioned-pvc.yaml <--
├── topology-aware/
│   ├── pod.yaml
│   ├── pvc.yaml
│   └── --> storageclass.yaml <--
└── --> secrets.yaml <--
```

Files marked with `--> ... <--` may need to be customized.

* `dynamic-provisioning/` : creates a new Manila NFS share and mounts it in a Pod.
* `static-provisioning/` : fetches an existing Manila NFS share and mounts it in a Pod
* `snapshot/` : takes a snapshot from a PVC source, restores it into a new share and mounts it in a Pod. Deploy manifests in `dynamic-provisioning/` first
* `topology-aware/` : topology-aware dynamic provisioning

Make sure the `provisioner` field in `storageclass.yaml` and `snapshotclass.yaml` matches the driver name in your deployment!

After deploying each example you should see the corresponding Pod with status _Running_ soon.
