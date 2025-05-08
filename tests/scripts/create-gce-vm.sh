#!/usr/bin/env bash

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
set -o errexit -o nounset -o pipefail

CLUSTER_NAME=${CLUSTER_NAME:-"cpo-e2e"}
GOOGLE_APPLICATION_CREDENTIALS=${GOOGLE_APPLICATION_CREDENTIALS:-""}
GCP_PROJECT=${GCP_PROJECT:-""}
GCP_REGION=${GCP_REGION:-"us-east4"}
GCP_ZONE=${GCP_ZONE:-"us-east4-a"}
GCP_MACHINE_MIN_CPU_PLATFORM=${GCP_MACHINE_MIN_CPU_PLATFORM:-"Intel Cascade Lake"}
GCP_MACHINE_TYPE=${GCP_MACHINE_TYPE:-"n2-standard-8"}
GCP_NETWORK_NAME=${GCP_NETWORK_NAME:-"${CLUSTER_NAME}-mynetwork"}
# Flavor options are: default
# * default: installs devstack via cloud-init, OPENSTACK_RELEASE only works on default
FLAVOR=${FLAVOR:="default"}
PRIVATE_IP="10.0.2.15"

echo "Using: GCP_PROJECT: ${GCP_PROJECT} GCP_REGION: ${GCP_REGION} GCP_NETWORK_NAME: ${GCP_NETWORK_NAME}"
export CLOUDSDK_CORE_PROJECT=${GCP_PROJECT}

# retry $1 times with $2 sleep in between
function retry {
  attempt=0
  max_attempts=$1
  interval=$2

  shift; shift
  until [[ ${attempt} -ge "${max_attempts}" ]] ; do
    attempt=$((attempt+1))
    set +e
    eval "$*" && return || echo "failed ${attempt} times: $*"
    set -e
    sleep "${interval}"
  done

  echo "error: reached max attempts at retry($*)"
  return 1
}

function init_networks() {
  if [[ ${GCP_NETWORK_NAME} != "default" ]]; then
    if ! gcloud compute networks describe "${GCP_NETWORK_NAME}" > /dev/null 2>&1;
    then
      gcloud compute networks create "${GCP_NETWORK_NAME}" --subnet-mode custom --quiet
      gcloud compute networks subnets create "${GCP_NETWORK_NAME}" --network="${GCP_NETWORK_NAME}" --range="10.0.0.0/20" --region "${GCP_REGION}" --quiet
      gcloud compute firewall-rules create "${GCP_NETWORK_NAME}"-allow-http --allow tcp:80 --network "${GCP_NETWORK_NAME}" --quiet
      gcloud compute firewall-rules create "${GCP_NETWORK_NAME}"-allow-https --allow tcp:443 --network "${GCP_NETWORK_NAME}" --quiet
      gcloud compute firewall-rules create "${GCP_NETWORK_NAME}"-allow-icmp --allow icmp --network "${GCP_NETWORK_NAME}" --priority 65534 --quiet
      gcloud compute firewall-rules create "${GCP_NETWORK_NAME}"-allow-internal --allow "tcp:0-65535,udp:0-65535,icmp" --network "${GCP_NETWORK_NAME}" --priority 65534 --quiet
      gcloud compute firewall-rules create "${GCP_NETWORK_NAME}"-allow-ssh --allow "tcp:22" --network "${GCP_NETWORK_NAME}" --priority 65534 --quiet
    fi
  fi

  printf "\n### gcloud compute firewall-rules list ###\n"
  gcloud compute firewall-rules list
  printf "\n### gcloud compute networks list ###\n"
  gcloud compute networks list
  printf "\n### gcloud compute networks describe ${GCP_NETWORK_NAME} ###\n"
  gcloud compute networks describe "${GCP_NETWORK_NAME}"

  if ! gcloud compute routers describe "${CLUSTER_NAME}-myrouter" --region="${GCP_REGION}" > /dev/null 2>&1;
  then
    gcloud compute routers create "${CLUSTER_NAME}-myrouter" --region="${GCP_REGION}" --network="${GCP_NETWORK_NAME}"
  fi
  if ! gcloud compute routers nats describe --router="${CLUSTER_NAME}-myrouter" "${CLUSTER_NAME}-mynat" --region="${GCP_REGION}" > /dev/null 2>&1;
  then
  gcloud compute routers nats create "${CLUSTER_NAME}-mynat" --router-region="${GCP_REGION}" --router="${CLUSTER_NAME}-myrouter" --nat-all-subnet-ip-ranges --auto-allocate-nat-external-ips
  fi
}

main() {
  if [[ -n "${SKIP_INIT_NETWORK:-}" ]]; then
    echo "Skipping network initialization..."
  else
    echo "Start initializing networks"
    init_networks
  fi

  case "${FLAVOR}" in
  "default")
    local disk_name="devstack-${FLAVOR}-ubuntu2404"
    if ! gcloud compute disks describe "${disk_name}" --zone "${GCP_ZONE}" > /dev/null 2>&1;
    then
      gcloud compute disks create "${disk_name}" \
        --image-project ubuntu-os-cloud --image-family ubuntu-2404-lts-amd64 \
        --zone "${GCP_ZONE}"
    fi

    if ! gcloud compute images describe "${disk_name}" > /dev/null 2>&1;
    then
      gcloud compute images create "${disk_name}" \
        --source-disk "${disk_name}" --source-disk-zone "${GCP_ZONE}" \
        --licenses "https://www.googleapis.com/compute/v1/projects/vm-options/global/licenses/enable-vmx"
    fi
    ;;
  *)
    echo "Unsupported flavor: ${FLAVOR}"
    exit 1
    ;;
  esac

  if ! gcloud compute instances describe devstack --zone "${GCP_ZONE}" > /dev/null 2>&1;
  then
    gcloud compute instances create devstack \
      --zone "${GCP_ZONE}" \
      --image "${disk_name}" \
      --boot-disk-size 30G \
      --boot-disk-type pd-ssd \
      --can-ip-forward \
      --tags http-server,https-server,novnc,openstack-apis \
      --min-cpu-platform "${GCP_MACHINE_MIN_CPU_PLATFORM}" \
      --machine-type "${GCP_MACHINE_TYPE}" \
      --network-interface="private-network-ip=${PRIVATE_IP},network=${CLUSTER_NAME}-mynetwork,subnet=${CLUSTER_NAME}-mynetwork"
  fi

  printf "\n### Waiting until cloud-init is done... ###\n"
  retry 120 15 "gcloud compute ssh --zone ${GCP_ZONE} devstack -- cat /var/lib/cloud/instance/boot-finished"

  username=$(gcloud compute ssh --zone ${GCP_ZONE} devstack -- echo $(whoami))
  public_ip=$(gcloud compute instances describe devstack --zone "${GCP_ZONE}" --format='get(networkInterfaces[0].accessConfigs[0].natIP)')
  export PUBLIC_IP=${public_ip}
  export USERNAME=$username

  echo "Public IP for instance devstack: ${PUBLIC_IP}, user: ${USERNAME}"
}

main "$@"
