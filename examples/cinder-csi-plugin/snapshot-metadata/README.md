# Snapshot Metadata (Changed Block Tracking)

Manifests that add the CSI SnapshotMetadata service to the Cinder CSI
controller plugin deployed from `manifests/cinder-csi-plugin`. The requirements
and the mapping to Cinder are described in
[Snapshot Metadata (Changed Block Tracking)](../../../docs/cinder-csi-plugin/features.md#snapshot-metadata-changed-block-tracking).

1. Install the `SnapshotMetadataService` CRD and the sidecar ClusterRole from
   the `deploy/` directory of
   [external-snapshot-metadata](https://github.com/kubernetes-csi/external-snapshot-metadata)
   and bind the ClusterRole to the `csi-cinder-controller-sa` service account in
   `kube-system`.

2. Create a TLS secret for the sidecar. The certificate must be valid for the
   Service name `csi-cinder-snapshot-metadata.kube-system`:

   ```
   kubectl -n kube-system create secret tls snapshot-metadata-tls \
     --cert=tls.crt --key=tls.key
   ```

3. Replace `spec.caCert` in `snapshot-metadata-service.yaml` with the
   base64-encoded PEM of the CA that signed that certificate, then create the
   Service and the `SnapshotMetadataService` resource:

   ```
   kubectl apply -f snapshot-metadata-service.yaml
   ```

4. Add the sidecar and the `--with-snapshot-metadata` flag to the controller
   plugin:

   ```
   kubectl -n kube-system patch deployment csi-cinder-controllerplugin \
     --patch-file controllerplugin-sidecar-patch.yaml
   ```

The sidecar exits at startup unless the driver advertises the
`SNAPSHOT_METADATA_SERVICE` plugin capability, which the driver does only when
`--with-snapshot-metadata` is set and at least one configured cloud may support
Block Storage API microversion 3.72.
