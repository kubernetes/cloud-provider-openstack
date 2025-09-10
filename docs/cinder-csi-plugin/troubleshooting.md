# Troubleshooting

## When trying to resize a Cinder Volume, PV/PVC is updated but file system not resized.

Check the volume status by using `openstack volume show` command about the volume (it should have `pvc-` prefix) and see whether backend openstack need update, contact with your openstack administrator for help on the error reported.

`openstack volume message list --os-volume-api-version 3.3` might also show some information about a failed resize, e.g. extend volume:Compute service failed to extend volume.

## When trying to resize a Cinder Volume based on Ceph, the VM doesn't see the new size.

Chances are, the underlying OpenStack is not configured properly.
This could be tested by manually creating a VM and Volume, attaching and resizing the Volume.
If you see the same behaviour, no change of the Volume in the VM, try to get in contact with the Admin-Team of the OpenStack you are using.

Chances are, Cinder is not allowed to send the `volume-extended` event to [Nova API](https://docs.openstack.org/api-ref/compute/?expanded=run-events-detail#run-events).
The error likely can be spotted as a 403 in the logs.

In that case. The section `nova` in [`cinder.conf`](https://docs.openstack.org/cinder/latest/configuration/block-storage/samples/cinder.conf.html)
must be configured properly, and with a user with sufficient privileges.

## When trying to use the topology feature, pods are not able to schedule

The Controller plugin reports the AZ of the host VM - a Compute (Nova) AZ retrieved from either the [config drive or the metadata service](https://docs.openstack.org/nova/latest/user/metadata.html) - to provide the accessible topology of the node. This AZ is then used when generating Volume create requests for the Block Storage (Cinder) service. For this to work as expected, the set of Compute and Block Storage AZs must match. If they do not - and you do not have the ability to re-configure the OpenStack deployment to change this - then you must do one of the following.

* Disable the topology feature by passing the `--with-topology=false` option to both the node driver (to prevent it reporting topology information) and controller (to prevent it requesting AZs from Cinder) services.
* Set the `availability` parameter on the Storage Class(es) and configure the `[BlockStorage] ignore-volume-az` config option for the controller plugin. The former overrides the topology value reported from the node driver and will be used instead when creating the Cinder Volume. The latter ensure the topology value reported from the node driver *is* used for the CSI Volume topology value. Failure to configure the latter will result in the Cinder Volume's AZ being used for the CSI Volume, which will cause any pods using the volume (via a PV) to be unschedulable due to the AZ mismatch.
