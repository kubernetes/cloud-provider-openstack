# OCCM bootstrap DNS test

This Go test checks that OCCM can initialize a node using an external OpenStack authentication hostname before CoreDNS can be scheduled.
The existing Ansible workflow prepares DevStack, DNS, a single-node k3s VM and the OCCM image, then runs the test before the load balancer tests.

## Test sequence

1. The fixture starts DNS on the DevStack host and configures the k3s subnet to use it.
   It resolves `keystone.occm.test` to Keystone's HTTP endpoint without changing OpenStack credentials or the service catalog.
2. The test checks node DNS resolution and confirms that CoreDNS cannot be scheduled because of the uninitialized taint.
3. It applies the checked-in OCCM manifests with only `dnsPolicy` changed to `ClusterFirstWithHostNet`.
   OCCM must report an authentication lookup failure for the test hostname against cluster DNS while the node remains uninitialized.
   A timeout or an unrelated container error does not pass this check.
4. It removes the negative-case DaemonSet and applies the original manifest without a DNS policy override.
   The same node must receive an OpenStack provider ID and lose its uninitialized taint, and OCCM and CoreDNS must become Ready.

The test checks the actual OCCM resolver files and rejects hosts-file shortcuts.
It uses the standalone manifests.
Helm rendering is covered separately.
Load balancer and wider network checks remain in the existing e2e tests.

## Running the test

Follow the [DevStack environment setup](../../../docs/developers-guide.md#devstack-based-testing-environment).
The checkout on the DevStack host at `~/src/k8s.io/cloud-provider-openstack` must match the code being tested.
Use a fresh, disposable environment.
The playbook creates the k3s VM and its OpenStack resources.

```sh
ansible-playbook \
  --user ubuntu \
  --inventory <DEVSTACK_VM_IP>, \
  tests/playbooks/test-occm-e2e.yaml \
  -e test_occm_bootstrap=true \
  -e run_e2e=false
```

Set `-e run_e2e=true` to also run the load balancer tests.

Bootstrap testing is enabled by default in this playbook and `tests/ci-occm-e2e.sh`.
Use `-e test_occm_bootstrap=false`, or `TEST_OCCM_BOOTSTRAP=false` for the CI entry point, to use the previous installation flow.
A successful test initializes the node, so repeating it requires a fresh environment.

The playbook builds a static Go test binary, copies it and the manifests to the k3s VM, and runs it as root with the local kubeconfig and containerd socket.
The hostname, expected IP and file paths are passed through `OCCM_BOOTSTRAP_*` variables in [bootstrap.yaml](../../playbooks/roles/install-cpo-occm/tasks/bootstrap.yaml).
Ordinary `go test ./tests/e2e/cloudprovider` runs skip cluster access unless `OCCM_BOOTSTRAP_E2E=true` is set.

## Results

Node and Pod state, events, DaemonSets, OCCM logs and resolver files are saved for diagnosis, including after a failed test.
Logs come from the local runtime, so collection works before the API server can reach the kubelet.
Only containers belonging to current OCCM Pods are used, keeping old negative-case logs out of the recovery check.
Secret resources are not included in the diagnostic snapshots.

Ansible copies the `artifacts` directory to `~/logs/occm-bootstrap/artifacts/` on the DevStack host.
The test output and DNS server log are saved alongside it as `test-output.txt` and `dns.log`.
The CI entry point downloads this directory and those files under `$ARTIFACTS/logs/devstack/occm-bootstrap/`.
