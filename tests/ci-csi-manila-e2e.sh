#!/bin/bash

# Copyright 2021 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

########################################################################
#
# Desc: Entrypoint of CSI Manila e2e CI job
#
# This script may be invoked for different branches (first added in
# release-1.23). It is getting GCP credentials from boskos and provision
# a GCP VM, then run ansible for the rest tasks.
#
########################################################################

set -x
set -o pipefail

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}" || exit 1
GOPATH=${PWD%/*/*/*}  # /home/prow/go will be 3 directories up from where code is
CONFIG_ANSIBLE="${CONFIG_ANSIBLE:-"true"}"
RESOURCE_TYPE="${RESOURCE_TYPE:-"gce-project"}"
ARTIFACTS="${ARTIFACTS:-${PWD}/_artifacts}"
mkdir -p "${ARTIFACTS}/logs/devstack"

cleanup() {
  # stop boskos heartbeat
  [[ -z ${HEART_BEAT_PID:-} ]] || kill -9 "${HEART_BEAT_PID}"
}
trap cleanup EXIT

# Install Ansible from Trixie (Debian Testing) to allow us to talk to a host
# running Python 3.12 like Ubuntu 24.04
#
# https://woju.eu/blog/2025/03/ansible-bookworm/
cat << EOF > /etc/apt/sources.list.d/trixie.list
deb https://deb.debian.org/debian trixie main
EOF

cat << EOF > /etc/apt/preferences.d/ansible.pref
Package: *
Pin: release n=trixie
Pin-Priority: -10

Package: ansible* python3-netaddr
Pin: release n=trixie
Pin-Priority: 990
EOF

apt-get update
apt-get install -y python3-requests ansible

# If BOSKOS_HOST is set then acquire a resource of type ${RESOURCE_TYPE} from Boskos.
if [ -n "${BOSKOS_HOST:-}" ]; then
  # Check out the account from Boskos and store the produced environment
  # variables in a temporary file.
  account_env_var_file="$(mktemp)"
  python3 tests/scripts/boskos.py --get --resource-type="${RESOURCE_TYPE}" 1>"${account_env_var_file}"
  checkout_account_status="${?}"

  # If the checkout process was a success then load the account's
  # environment variables into this process.
  # shellcheck disable=SC1090
  [ "${checkout_account_status}" = "0" ] && . "${account_env_var_file}"

  # Always remove the account environment variable file. It contains
  # sensitive information.
  rm -f "${account_env_var_file}"

  if [ ! "${checkout_account_status}" = "0" ]; then
    echo "Failed to get account from boskos, type: ${RESOURCE_TYPE}" 1>&2
    exit "${checkout_account_status}"
  fi

  # run the heart beat process to tell boskos that we are still
  # using the checked out account periodically
  python3 -u tests/scripts/boskos.py --heartbeat >> "${ARTIFACTS}/logs/boskos.log" 2>&1 &
  HEART_BEAT_PID=$!
fi

case "${RESOURCE_TYPE}" in
"gce-project")
    . tests/scripts/create-gce-vm.sh
    ;;
*)
    echo "Unsupported resource type: ${RESOURCE_TYPE}"
    exit 1
    ;;
esac

# Config ansible. If Ansible is installed from pip or from source,
# we need to create the config file manually.
if [[ "$CONFIG_ANSIBLE" == "true" ]]; then
  mkdir -p /etc/ansible
  cat <<EOF > /etc/ansible/ansible.cfg
[defaults]
private_key_file = ~/.ssh/google_compute_engine
host_key_checking = False
timeout = 30
stdout_callback = debug
EOF
fi

# Upload CPO code
scp -i ~/.ssh/google_compute_engine \
  -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no \
  -r ${GOPATH}/* ${USERNAME}@${PUBLIC_IP}:/root/

# Run ansible playbook on the CI host, e.g. a VM in GCP
# USERNAME and PUBLIC_IP are global env variables set after creating the CI host.
ansible-playbook -v \
  --user ${USERNAME} \
  --private-key ~/.ssh/google_compute_engine \
  --inventory ${PUBLIC_IP}, \
  --ssh-common-args "-o StrictHostKeyChecking=no" \
  tests/playbooks/test-csi-manila-e2e.yaml
exit_code=$?

# Fetch logs for debugging purpose
ansible-playbook -v \
  --user ${USERNAME} \
  --private-key ~/.ssh/google_compute_engine \
  --inventory ${PUBLIC_IP}, \
  --ssh-common-args "-o StrictHostKeyChecking=no" \
  tests/playbooks/fetch-logs.yaml

 scp -i ~/.ssh/google_compute_engine \
   -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no \
   -r ${USERNAME}@${PUBLIC_IP}:~/logs $ARTIFACTS/logs/devstack || true

# Fetch manila-csi tests results
scp -i ~/.ssh/google_compute_engine \
  -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no \
  -r ${USERNAME}@${PUBLIC_IP}:/var/log/csi-pod/* $ARTIFACTS/logs/ || true

# If Boskos is being used then release the resource back to Boskos.
[ -z "${BOSKOS_HOST:-}" ] || python3 tests/scripts/boskos.py --release >> "$ARTIFACTS/logs/boskos.log" 2>&1

exit ${exit_code}
