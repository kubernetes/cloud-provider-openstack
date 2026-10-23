---
name: debug-ci
description: "CPO CI: Debug E2E test failures in Prow. Browse artifacts, analyze Ginkgo/shell test output, trace through CPO controller and OpenStack DevStack logs to find root cause."
---
# CPO CI E2E Failure Debugging

Debug failures in the Cloud Provider OpenStack (CPO) E2E test suite run via Prow at `prow.k8s.io` against the `kubernetes/cloud-provider-openstack` repository.

## Prerequisites

- HTTP access to `storage.googleapis.com` (public, no authentication needed)
- Working directory: the CPO repository root
- Recommended: download artifacts to `/tmp/ci-artifacts/` for local analysis

## Overview

CPO E2E tests run via Prow on `prow.k8s.io`. Each E2E job provisions a GCE VM (acquired from Boskos), deploys DevStack and K3s on it via Ansible, builds and deploys CPO components (OCCM, Cinder CSI, or Manila CSI), then runs E2E tests. When a test fails, the CI collects DevStack service logs, K3s logs, CSI/OCCM pod logs, and kernel messages as artifacts uploaded to GCS.

## Step 1: Identify the Failure

### Prow Job Names

| Prow Job | Entry Script | What It Tests |
|---|---|---|
| `openstack-cloud-controller-manager-e2e-test` | `tests/ci-occm-e2e.sh` | OCCM load balancer E2E (Amphora provider) |
| `openstack-cloud-controller-manager-ovn-e2e-test` | `tests/ci-occm-e2e.sh` | OCCM load balancer E2E (OVN provider) |
| `openstack-cloud-csi-cinder-e2e-test` | `tests/ci-csi-cinder-e2e.sh` | Cinder CSI E2E (Kubernetes storage conformance) |
| `openstack-cloud-csi-manila-e2e-test` | `tests/ci-csi-manila-e2e.sh` | Manila CSI E2E (custom Ginkgo suite) |
| `openstack-cloud-csi-cinder-sanity-test` | `make test-cinder-csi-sanity` | Cinder CSI sanity (no VM, unit-level) |
| `openstack-cloud-csi-manila-sanity-test` | `make test-manila-csi-sanity` | Manila CSI sanity (no VM, unit-level) |
| `openstack-cloud-keystone-authentication-authorization-test` | `tests/ci-keystone-e2e.sh` | Currently a **no-op placeholder** |
| `openstack-cloud-helm-chart-lint` | `make lint-charts` | Helm chart linting (no VM) |

### TestGrid Dashboards

| Dashboard | URL |
|---|---|
| OCCM | `https://testgrid.k8s.io/provider-openstack-openstack-cloud-controller-manager` |
| Cinder CSI | `https://testgrid.k8s.io/provider-openstack-openstack-csi-cinder` |
| Manila CSI | `https://testgrid.k8s.io/provider-openstack-manila-csi-plugin` |
| Keystone | `https://testgrid.k8s.io/provider-openstack-keystone-authentication-authorization` |

### Navigating a Prow Failure

1. From a Prow URL (e.g. `https://prow.k8s.io/view/gs/kubernetes-ci-logs/...`), click on the failing job to see the build log.
2. The **build-log.txt** contains the full Ansible-orchestrated run output including all role and task names.
3. Click **Artifacts** in the Prow UI to browse the GCS artifact directory.

## Step 2: Access Artifacts

The GCS bucket `kubernetes-ci-logs` is **publicly readable**. No `gsutil` or authentication is required. All artifacts can be fetched over HTTPS.

### GCS Path Convention

**Presubmits:**
```
https://storage.googleapis.com/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/<PR#>/<job-name>/<build-id>/artifacts/
```

**Periodics / postsubmits:**
```
https://storage.googleapis.com/kubernetes-ci-logs/logs/<job-name>/<build-id>/artifacts/
```

### Browsing and Downloading

**Browse** the artifact directory tree in a web browser:
```
https://gcsweb.k8s.io/gcs/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/<PR#>/<job-name>/<build-id>/artifacts/
```

**Download** individual files directly (no auth needed):
```bash
# Download the build log
curl -sS -o /tmp/build-log.txt \
  "https://storage.googleapis.com/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/<PR#>/<job-name>/<build-id>/build-log.txt"

# Download a DevStack service log
curl -sS -o /tmp/o-api.log \
  "https://storage.googleapis.com/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/<PR#>/<job-name>/<build-id>/artifacts/logs/devstack/o-api.log"

# Download a CSI pod log
curl -sS -o /tmp/csi-cinder-controllerplugin.log \
  "https://storage.googleapis.com/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/<PR#>/<job-name>/<build-id>/artifacts/logs/csi-cinder-controllerplugin.log"
```

**From a Prow URL**, extract the GCS path: a Prow job URL like
`https://prow.k8s.io/view/gs/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/2345/openstack-cloud-csi-cinder-e2e-test/1234567890`
maps to:
`https://storage.googleapis.com/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/2345/openstack-cloud-csi-cinder-e2e-test/1234567890/`

### Artifact Contents

**DevStack service logs** (in `artifacts/logs/devstack/`):

These are collected by the `fetch-logs` Ansible role from systemd journal (`devstack@<service>.service`).

| File | Service | Relevant CPO Component |
|---|---|---|
| `q-svc.log` | Neutron API | OCCM (LB networking), all CSI |
| `n-api.log` | Nova API | OCCM (node management), Cinder CSI (attach/detach) |
| `n-cpu.log` | Nova Compute | Server build failures |
| `n-sch.log` | Nova Scheduler | Server scheduling failures |
| `g-api.log` | Glance API | Image operations |
| `c-api.log` | Cinder API | Cinder CSI (volume CRUD) |
| `c-vol.log` | Cinder Volume | Cinder CSI (backend errors, attach/detach) |
| `o-api.log` | Octavia API | OCCM (load balancer operations) |
| `keystone.log` | Keystone | Authentication for all components |
| `m-api.log` | Manila API | Manila CSI (share operations) |

**K3s and system logs:**

| File | Description |
|---|---|
| `artifacts/logs/k3s.log` | K3s service journal from the GCE VM |

**CPO pod logs** (in `artifacts/logs/`):

| File | Job(s) | Description |
|---|---|---|
| `csi-cinder-controllerplugin.log` | Cinder CSI | Cinder CSI controller plugin pod log |
| `csi-cinder-nodeplugin.log` | Cinder CSI | Cinder CSI node plugin pod log |
| `csi-manila-controllerplugin.log` | Manila CSI | Manila CSI controller plugin pod log |
| `csi-manila-nodeplugin.log` | Manila CSI | Manila CSI node plugin pod log |
| `occm.log` | Cinder CSI | OCCM pod log (collected on CSI deployment failure) |
| `dmesg.log` | Cinder CSI | Kernel messages from the K3s node |
| `dmesg_local.log` | Cinder CSI | Kernel messages from the GCE VM |
| `octavia-image-build.log` | OCCM | Octavia amphora image build log |

**Test output:**

| File | Job(s) | Description |
|---|---|---|
| `cinder-csi-e2e.log` | Cinder CSI | Full Ginkgo test output |
| `manila-csi-e2e.log` | Manila CSI | Full Ginkgo test output |

**Note:** For the OCCM job, test output is inline in `build-log.txt` (the shell script output is captured by Ansible's `debug` callback).

## Step 3: Understand the CI Pipeline

### Full Flow

1. **Entry script** (e.g. `tests/ci-csi-cinder-e2e.sh`):
   - Acquires a GCE project from Boskos (`tests/scripts/boskos.py --get`)
   - Creates a GCE VM (`tests/scripts/create-gce-vm.sh`)
   - Uploads CPO source code to the VM via SCP
   - Runs an Ansible playbook on the VM
   - Fetches logs back via a `fetch-logs` playbook + SCP
   - Releases the Boskos resource

2. **Ansible playbook** (e.g. `tests/playbooks/test-csi-cinder-e2e.yaml`) runs roles in order:
   - `install-golang` — installs Go on the VM
   - `install-devstack` — deploys DevStack (`stable/2025.2`) with configured services
   - `install-docker` — installs Docker
   - `install-docker-registry` — sets up a local Docker registry
   - `install-k3s` — deploys a single-node K3s cluster
   - `install-cpo-occm` — builds and deploys OCCM (always deployed, even for CSI tests)
   - `install-csi-cinder` / `install-csi-manila` — builds and deploys the CSI plugin, then runs the E2E tests

3. **Log collection** (`tests/playbooks/fetch-logs.yaml`):
   - Runs the `fetch-logs` role which collects DevStack service journals and K3s logs
   - CSI pod logs are collected inline in the `install-csi-*` roles after tests complete
   - Everything is SCP'd back to the Prow pod's `$ARTIFACTS` directory

### Key Detail: `build-log.txt` Structure

The `build-log.txt` is the full Prow build log containing:
- Boskos resource acquisition
- GCE VM creation
- The full Ansible run with the `debug` stdout callback (verbose task-by-task output)
- SCP log fetching

Search for Ansible task names to locate specific phases:
```
TASK [install-devstack : ...]    — DevStack setup
TASK [install-k3s : ...]         — K3s deployment
TASK [install-cpo-occm : ...]    — OCCM build and deployment
TASK [install-csi-cinder : ...]  — Cinder CSI build, deployment, and E2E tests
fatal:                           — Ansible task failure
```

## Step 4: Analyze the Failure

### 4a. OCCM E2E Failures

The OCCM E2E test is a shell script: `tests/e2e/cloudprovider/test-lb-service.sh`.

**Test cases** (run sequentially):
1. `test_basic` — creates a LoadBalancer Service, verifies connectivity via the FIP
2. `test_forwarded` — tests `X-Forwarded-For` header (skipped for OVN provider)
3. `test_update_port` — creates a Service, updates ports, validates LB listeners and members
4. `test_shared_lb` — multi-service shared LB lifecycle (create, update, delete, port collision)
5. `test_shared_user_lb` — creates an Octavia LB externally, attaches a K8s Service to it

**Finding failures:** search `build-log.txt` for:
```
>>>>>>> FAIL:
```

**On failure**, the cleanup trap in `test-lb-service.sh` dumps OCCM pod logs inline in `build-log.txt`.

**Key DevStack logs to check:**
- `o-api.log` — Octavia API (LB creation, listener, pool, member operations)
- `q-svc.log` — Neutron API (networks, subnets, ports, FIPs)

**Common OCCM issues:**
- **LB stuck not ACTIVE**: timeout waiting for `provisioning_status == ACTIVE`. Check `o-api.log` for errors in the LB workflow.
- **FIP creation failure**: check `q-svc.log` for floating IP errors.
- **Listener port conflicts**: test `test_shared_lb` deliberately tests port collisions — verify the conflict was correctly rejected.
- **OVN-specific failures**: jobs with `OCTAVIA_PROVIDER=ovn` use OVN Octavia provider; some tests (e.g. `test_forwarded`) are skipped.

### 4b. Cinder CSI E2E Failures

Uses the upstream Kubernetes `e2e.test` binary with:
```
--ginkgo.focus='External.Storage'
```

Test driver config: `tests/e2e/csi/cinder/test-driver.yaml`.

**Finding failures:** search `cinder-csi-e2e.log` (or `build-log.txt` if the artifact wasn't collected) for:
```
[FAIL]
Failure
FAILED
```

**Key logs to check:**
- `csi-cinder-controllerplugin.log` — CSI controller (CreateVolume, DeleteVolume, ControllerPublish/Unpublish)
- `csi-cinder-nodeplugin.log` — CSI node (NodeStage/Unstage, NodePublish/Unpublish)
- `c-api.log` — Cinder API (volume create, delete, attach, detach, extend)
- `c-vol.log` — Cinder Volume backend (actual storage operations)
- `n-api.log` — Nova API (volume attach/detach via compute)
- `dmesg.log` — kernel messages (filesystem, device mapper issues)

**Common Cinder CSI issues:**
- **Volume stuck in creating/attaching/detaching state**: check `c-vol.log` for backend errors.
- **Attach/detach timeout**: check `n-api.log` for Nova errors and `c-api.log` for Cinder state transitions.
- **Filesystem errors**: check `dmesg.log` for I/O errors or filesystem corruption.
- **Snapshot failures**: check `c-api.log` for snapshot state transitions.

### 4c. Manila CSI E2E Failures

Uses a custom Go test suite: `cmd/tests/manila-csi-e2e-suite/manila_csi_e2e_suite_test.go`.

**Finding failures:** search `manila-csi-e2e.log` for:
```
[FAIL]
Failure
FAILED
```

**Key logs to check:**
- `csi-manila-controllerplugin.log` — Manila CSI controller pod log
- `csi-manila-nodeplugin.log` — Manila CSI node pod log
- DevStack Manila logs (the log file name depends on the DevStack service unit name)

### 4d. Infrastructure / Setup Failures

These failures occur before E2E tests run. Search `build-log.txt` for:

```bash
# Ansible task failures
grep -E "fatal:|FAILED|UNREACHABLE" build-log.txt

# Boskos failures
grep "Failed to get account from boskos" build-log.txt

# GCE VM creation failures
grep -E "ERROR|error" build-log.txt | head -20

# Pod not reaching Running state
grep "check_occm\|check_csi_controller\|check_csi_node" build-log.txt
```

**Common infrastructure issues:**
- **Boskos exhaustion**: "Failed to get account from boskos" — all GCE projects are in use; retry later.
- **GCE VM creation failure**: network, quota, or image issues — check early in `build-log.txt`.
- **DevStack install failure**: search for `fatal:` in the `install-devstack` tasks. DevStack branch is `stable/2025.2`.
- **K3s not starting**: check `k3s.log` artifact and the `install-k3s` Ansible tasks.
- **Pod not Running**: the Ansible roles retry up to 24 times with 5-second delays. On failure, they dump `kubectl describe` output and pod logs inline in `build-log.txt`.
- **Image build/push failure**: search for the `Build and upload` Ansible tasks.

## Step 5: Trace DevStack Service Logs

### Service Name Mapping

DevStack services run as systemd units named `devstack@<service>.service`. The `fetch-logs` role extracts the journal for each into `<service>.log`.

| CPO Component | OpenStack Service | DevStack Unit | Log File |
|---|---|---|---|
| OCCM (LB, Amphora) | Octavia API | `devstack@o-api` | `o-api.log` |
| OCCM (LB, OVN) | Octavia + Neutron | `devstack@o-api`, `devstack@q-svc` | `o-api.log`, `q-svc.log` |
| OCCM (nodes) | Nova API | `devstack@n-api` | `n-api.log` |
| Cinder CSI | Cinder API | `devstack@c-api` | `c-api.log` |
| Cinder CSI | Cinder Volume | `devstack@c-vol` | `c-vol.log` |
| Manila CSI | Manila API | `devstack@m-api` | `m-api.log` |
| All | Neutron | `devstack@q-svc` | `q-svc.log` |
| All | Keystone | `devstack@keystone` | `keystone.log` |
| All | Glance | `devstack@g-api` | `g-api.log` |

### Downloading Artifacts for Analysis

Given a Prow job URL, download the relevant artifacts:
```bash
# Set the base URL (extract from the Prow job URL)
BASE="https://storage.googleapis.com/kubernetes-ci-logs/pr-logs/pull/kubernetes_cloud-provider-openstack/<PR#>/<job-name>/<build-id>"

# Download build log and key artifacts
mkdir -p /tmp/ci-artifacts/devstack
curl -sS -o /tmp/ci-artifacts/build-log.txt "${BASE}/build-log.txt"
curl -sS -o /tmp/ci-artifacts/devstack/o-api.log "${BASE}/artifacts/logs/devstack/o-api.log"
curl -sS -o /tmp/ci-artifacts/devstack/c-api.log "${BASE}/artifacts/logs/devstack/c-api.log"
curl -sS -o /tmp/ci-artifacts/devstack/q-svc.log "${BASE}/artifacts/logs/devstack/q-svc.log"
# ... etc. for whichever logs are relevant
```

### General Techniques

```bash
# Find errors in a DevStack service log
grep "ERROR" /tmp/ci-artifacts/devstack/o-api.log

# Find HTTP requests/responses with status codes
grep "HTTP/1\." /tmp/ci-artifacts/devstack/c-api.log | grep " 4[0-9][0-9] \| 5[0-9][0-9] "

# Find operations for a specific resource by name or ID
grep "<volume-id-or-name>" /tmp/ci-artifacts/devstack/c-api.log

# Find errors in a time window
grep "14:38\|14:39\|14:40" /tmp/ci-artifacts/devstack/o-api.log | grep "ERROR"

# Search all DevStack logs for errors at once
grep "ERROR" /tmp/ci-artifacts/devstack/*.log | grep "14:38"
```

### Correlating CPO Logs with DevStack Logs

CPO components log the OpenStack API calls they make. To trace a failure:

1. Find the error in the CPO pod log (e.g. `csi-cinder-controllerplugin.log`) — note the timestamp and any resource IDs.
2. Search the corresponding DevStack service log for the same resource ID or timestamp.
3. Check for HTTP error responses (4xx/5xx) in the DevStack log around that time.

## Step 6: Classify the Failure

| Category | Description | Action |
|---|---|---|
| **OpenStack bug** | DevStack service returned unexpected behavior (e.g. 500, stuck state) | Document as known DevStack/OpenStack issue |
| **CPO bug** | Controller logic error in OCCM or CSI plugin code | Fix the CPO code |
| **Test bug** | Wrong assertions, missing setup, incorrect test logic | Fix the test script or test driver config |
| **Infrastructure flake** | GCE VM issues, Boskos exhaustion, network timeouts, DevStack install flakes | Document and consider retry or infra fix |

### Classification Guardrails

Before classifying a failure as **Infrastructure flake**, verify:

1. You have **positive evidence** of an infrastructure problem (GCE quota error, Boskos timeout, network unreachable, DevStack install crash, etc.) — not just "I can't find another explanation".
2. You have explicitly ruled out **CPO bug** (reviewed relevant controller code path), **Test bug** (verified test script logic and assertions), and **OpenStack bug** (checked DevStack service logs for API-level errors) with specific reasoning for why each doesn't apply.
3. State the specific evidence for your classification — if you cannot point to a concrete log line or error message, reconsider your conclusion.

"Unknown" is a valid classification. Do not default to "Infrastructure flake" when uncertain.

## Caveats

### DevStack Branch

The CI uses DevStack branch `stable/2025.2` (configured in `tests/playbooks/roles/install-devstack/defaults/main.yaml`). Failures may be specific to this OpenStack release.

### K3s Instead of Full Kubernetes

The CI uses K3s (lightweight Kubernetes) rather than a full kubeadm cluster. Some behaviors (e.g. kubelet configuration, CNI) may differ from production Kubernetes.

### OCCM Is Always Deployed

Even for CSI-only tests, the `install-cpo-occm` role is run first. OCCM manages the cloud provider configuration for the K3s cluster. If OCCM fails to deploy, CSI tests will also fail.

### Keystone E2E Is a Placeholder

The `openstack-cloud-keystone-authentication-authorization-test` job currently runs `tests/ci-keystone-e2e.sh` which is a no-op placeholder that always passes.

### Pod Logs May Be Empty

CSI pod logs are collected after E2E tests complete. If the pod crashed and was not restarted, or if collection runs too early, log files may be empty or missing.

### `build-log.txt` Is Very Large

The `build-log.txt` contains the entire Ansible run with the `debug` stdout callback. It can be hundreds of thousands of lines. Use targeted searches rather than reading it end-to-end.

### Enabled DevStack Services Vary by Job

Each Ansible playbook enables different DevStack services:

| Job | DevStack Services |
|---|---|
| OCCM E2E | nova, glance, cinder, neutron, octavia, ovn-octavia, barbican |
| Cinder CSI E2E | nova, neutron, glance, cinder |
| Manila CSI E2E | nova, neutron, glance, manila |
