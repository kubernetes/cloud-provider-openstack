# Troubleshooting

## When trying to resize a Cinder Volume based on Ceph, the VM doesn't see the new size.

Chances are, the underlying OpenStack is not configured properly.
This could be tested by manually creating a VM and Volume, attaching and resizing the Volume.
If you see the same behaviour, no change of the Volume in the VM, try to get in contact with the Admin-Team of the OpenStack you are using.

Chances are, Cinder is not allowed to send the `volume-extended` event to [Nova API](https://docs.openstack.org/api-ref/compute/?expanded=run-events-detail#run-events).
The error likely can be spotted as a 403 in the logs.

In that case. The section `nova` in [`cinder.conf`](https://docs.openstack.org/cinder/latest/configuration/block-storage/samples/cinder.conf.html)
must be configured properly, and with a user with sufficient privileges.
