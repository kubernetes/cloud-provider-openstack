# CSI Manila developer's guide

## Running CSI Sanity tests

Sanity tests create a real instance of driver with fake Manila client and CSI forwarding node plugin.
See [Sanity check](https://github.com/kubernetes-csi/csi-test/tree/master/pkg/sanity) for more info.

Run the test suite with `make test-manila-csi-sanity`.

## Share adapters

A share adapter is an interface found here `pkg/csi/manila/shareadapters/shareadapter.go` that forms an adapter between a Manila share and a CSI plugin.

### Adding support for more share protocols

As of writing this document, CSI Manila supports only NFS and CephFS shares. If you'd like to expand on this set and contribute with a new adapter for a share protocol, keep reading!

1. Create a new file `some-protocol.go` under `pkg/csi/manila/shareadapters`
2. Create a new struct that implements the `ShareAdapter` interface
3. Add a case block in `getShareAdapter()` function in `pkg/csi/manila/adapters.go`. The condition string must match one of Manila's supported share protocols.
4. Add the protocol name to the `matches` expression (regexp syntax) inside `ControllerVolumeContext.Protocol` field tags in `pkg/csi/manila/options/shareoptions.go`. Again, it must match one of Manila's supported share protocols.
5. Update the docs in `docs/using-manila-csi-plugin.md`, namely any parameters that the protocol or node plugin may use. There's also a dedicated section "Share protocols support matrix" at the bottom of the document which needs to be updated: name of the share protocol, link to the proxy'd CSI driver and its supported version(s).

### Passing volume options to share adapters

Usually, shares / share adapters offer a set of options which users may want to configure. Those options can be specified in `pkg/csi/manila/options/shareoptions.go`, in either `ControllerVolumeContext` or `NodeVolumeContext` structs, or both. Each struct field must contain `name` field tag which is then used for parsing input values. It's highly recommended that, if necessary, you use validator tags instead of hard-coding validation checks in share adapters. See `pkg/share/manila/shareoptions/validator/validator.go` for info on supported validator tags.

## Service capabilities

**Controller Service:**
* `CREATE_DELETE_VOLUME`
* `CREATE_DELETE_SNAPSHOT` (snapshotting CephFS shares is not supported yet - planned as a part of GSoC 2019)

Availability Zones are not supported yet.

**Node Service:**

Node Service capabilities of the proxy'd Node Plugin

## Notes on design...

**Problem 1:**

OpenStack Manila supports NFS, CIFS, GLUSTERFS, HDFS, CEPHFS and MAPRFS share protocols. Implementing support for all of those backends within a single CSI driver doesn't scale very well because:
* a dedicated CSI driver for each of those file-systems already exists - or eventually will
* not reusing those existing drivers means more maintenance for devs, possible fragmented/missing features between drivers
* mounting the file-systems mentioned above requires tools that are not usually present on the host system and they'd need to be built into the container image

**Solution:**

CSI Manila's Controller Plugin deals with Manila and nothing else. All node-related operations (attachments, mounts) are carried out by *another CSI driver* dedicated for that particular filesystem, effectively offloading all FS-specific code out of CSI Manila. This is achieved by Node Plugin acting only as a proxy and forwarding CSI RPCs to the other CSI driver.

For example, creating and mounting a CephFS Manila share would go like this:

(1) CO requests a volume provisioning with `CreateVolume`:

```
                        +--------------------+
                        |       MASTER       |
+------+  CreateVolume  |  +--------------+  |
|  CO  +------------------>+  csi-manila  |  |
+------+                |  +--------------+  |
                        +--------------------+

1. Authenticate with Manila v2 client
2. Create a CephFS share
3. Create a cephx access rule
```

(2) And when CO publishes the volume onto a node with `NodePublishVolume`:
```
                             +--------------------------------------+
                             |                 NODE                 |
+------+  NodePublishVolume  |  +--------------+                    |
|  CO  +----------------------->+  csi-manila  |                    |
+------+                     |  +------+-------+                    |
                             |         |                            |
                             |         | FORWARD NodePublishVolume  |
                             |         V                            |
                             |  +------+-------+                    |
                             |  |  csi-cephfs  |                    |
                             |  +--------------+                    |
                             +--------------------------------------+

1. Authenticate with Manila v2 client
2. Retrieve the CephFS share
3. Retrieve the cephx access rule
4. Connect to csi-cephfs socket
5. Call csi-cephfs's NodePublishVolume, return its response
```

The initial idea was to encompass all Manila share protocols within a single instance of csi-manila. Due to limitations of CSI, this cannot be achieved and there have to be multiple instances of csi-manila running in order to serve multiple share protocols, one for each. There are couple of RPCs in Node Service that don't bring enough context to decide which proxy'd driver that particular RPC should be forwarded to (e.g. `NodeGetCapabilities` would be ambiguous and we can't know which proxy'd driver should answer) => therefore with current design, there may be only a single proxy'd driver per csi-manila instance, i.e. one share protocol per csi-manila instance.

**Problem 2:**

The CSI spec allows COs to retry RPCs for any reason, e.g. as a part of timeout-handling mechanism. Such situation may occur even during the execution of operations that would otherwise return with success, but are simply taking too long to complete. This results in multiple operations being executed for the same resource in parallel, placing unnecessary load on Manila.

**Solution:**

Using a simple synchronization with a mutex for each resource would result in this:
```
  R req1             req2             req3
T -----------------+----------------+----------------+
01| LOCK           |                |                |
02| op1            |                |                |
03| op2            | LOCK...wait    |                |
04| op3            | wait...        |                |
05| UNLOCK         | wait...        | LOCK...wait    |
06| RETURN success + LOCK           | wait...        |
07|                  op1            | wait...        |
08|                  op2            | wait...        |
09|                  op3            | wait...        |
21|                  UNLOCK         | wait...        |
22|                  RETURN success + LOCK           |
23|                                   op1            |
24|                                   op2            |
25|                                   op3            |
26|                                   UNLOCK         |
27|                                   RETURN SUCCESS +

T - time
R - request (e.g. CreateVolume); requests may be running in parallel
LOCK/UNLOCK - locking/unlocking a mutex for this resource (e.g. volume ID)
opN - some long running operation
```
The first request  `req1`  locks the mutex and launches a long running operation  `op`  (taking up 3 time units). The call times out, so the CO decides to retry with  `req2`  but needs to wait till  `req1`  finishes so that  `req2`  may acquire the lock. This pattern may repeat until the call doesn't time-out. There are two problems here: (a) it may take a long time till CO is satisfied with the answer, (b) the long running operation  `op`  is being run in all of those retries sequentially, wasting resources.

`pkg/csi/manila/responsebroker`  tries to tackle the problem (b), and hopes to mitigate (a):
```
  R req1                                             req2                          req3
T -------------------------------------------------+-----------------------------+-----------------------------+
01| LOCK(acq-or-create)                            |                             |                             |
02| op1                                            |                             |                             |
03| op2                                            | LOCK(acq-or-create)...wait  |                             |
04| op3                                            | wait...                     |                             |
05| UNLOCK, write success, wait till all Rs finish | wait...                     | LOCK(acq-or-create)...wait  |
06| wait...                                        | LOCK, read response:success | wait...                     |
07| wait...                                        | UNLOCK                      | wait...                     |
08| wait...                                        | RETURN SUCCESS              + LOCK, read response:success |
09| RETURN SUCCESS                                 +                               UNLOCK                      |
10|                                                                                RETURN SUCCESS              +
```

The same scenario as above:  `req1`  locks the mutex and runs the long running operation  `op`  and times-out.  `req2`  will block till it can acquire the lock, but instead of running  `op`  again, it reads the response from  `req1`  and if it's successful, it immediately exits with success. The  `responsebroker`  basically caches the result of an operation and propagates it to multiple readers.

Notes on using `responsebroker`:
* locking should be done at a per-resource granularity (e.g. a single volume) using a unique identifier of the resource as a key (e.g. volume ID)
* acquire the lock before launching the long-running operation using `ResponseBroker.Acquire(key)`
* if this is a new request (`Acquire` returned `true`) or the previous request failed (`ResponseHandle.Read()` returned a non-nil error), continue normally. Once the operation is done, you must call `ResponseHandle.Write(responseData, operationError)` which publishes the result of the operation and releases the lock. If the operation finished successfully, call `ResponseBroker.Done(key)` which waits till everyone has `Read()` the response
* otherwise (i.e. this is either an existing request or `Read()` returned a nil error) call `ResponseHandle.Release()` which releases the lock and return the response data you've read

