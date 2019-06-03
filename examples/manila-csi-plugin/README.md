## Example CSI Manila CephFS usage

1. Deploy CSI CephFS driver
2. Deploy CSI Manila driver with `--share-protocol-selector=CEPHFS` and `--fwdendpoint=unix:///csi/csi-cephfsplugin/csi.sock` (or similar, based on your environment)
3. Modify `secrets.yaml` to suite your OpenStack cloud environment. Refer to the _"Secrets, authentication"_ section of CSI Manila docs. You may also use helper scripts from `examples/manila-provisioner` to generate the Secrets manifest.
4. Deploy OpenStack secrets
5. Create a persistent volume:
  5.1 **If you want to provision a new share:**
    5.1.1 Modify `storageclass.yaml` to reflect your environment. Refer to the _"Controller Service volume parameters"_ section of CSI Manila docs.
    5.1.2 Deploy the `csi-manila-cephfs` storage class `storageclass.yaml`
    5.1.3 Deploy the persistent volume claim `pvc.yaml`
  5.2 **OR you want to use an existing share:**
    5.2.1 Modify `preprovisioned-pvc.yaml` to reflect your environment. Refer to the _"Node Service volume context"_ section of CSI Manila docs.
    5.2.2 Deploy the PV+PVC
6. Deploy `pod.yaml` which creates a Pod that mounts the share you've prepared in the steps above
7. You should see `pod/csicephfs-demo-pod` with status _Running_ soon
