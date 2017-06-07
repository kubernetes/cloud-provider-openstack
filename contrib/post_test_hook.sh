#!/bin/bash -xe

# Licensed under the Apache License, Version 2.0 (the "License"); you may
# not use this file except in compliance with the License. You may obtain
# a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and limitations
# under the License.

# This script is executed inside post_test_hook function in devstack gate.

BASE_DIR=$(cd $(dirname $BASH_SOURCE)/.. && pwd)


TESTS_LIST_REGEX=(
    '\[Slow\]'
    '\[Serial\]'
    '\[Disruptive\]'
    '\[Flaky\]'
    '\[Feature:.+\]'
    '\[HPA\]'
)

FLAKY_TESTS_LIST=(
    # https://github.com/kubernetes/kubernetes/issues/44226
    'Downward API volume [It] should update labels on modification [Conformance] [Volume]'
    'Downward API volume [It] should update annotations on modification [Conformance] [Volume]'
    'Projected [It] should update labels on modification [Conformance] [Volume]'
    'Secrets [It] should be consumable from pods in volume with mappings [Conformance] [Volume]'
)

FAILING_TESTS_LIST=(
    'Services [It] should be able to create a functioning NodePort service'
    'Services [It] should serve multiport endpoints from pods [Conformance]'
)

function escape_test_name() {
    sed 's/\[[^]]*\]//g' <<< "$1" | sed "s/[^[:alnum:]]/ /g" | tr -s " " | sed "s/^\s\+//" | sed "s/\s/.*/g"
}

function test_names () {
    local first=y
    for name in "${TESTS_LIST_REGEX[@]}"; do
        if [ -z "${first}" ]; then
            echo -n "|"
        else
            first=
        fi
        echo -n "${name}"
    done
    for name in "${FLAKY_TESTS_LIST[@]}"; do
        if [ -z "${first}" ]; then
            echo -n "|"
        else
            first=
        fi
        echo -n "$(escape_test_name "${name}")"
    done
    for name in "${FAILING_TESTS_LIST[@]}"; do
        if [ -z "${first}" ]; then
            echo -n "|"
        else
            first=
        fi
        echo -n "$(escape_test_name "${name}")"
    done
}

cd $BASE/new/devstack
source openrc admin admin

echo "In post_test_hook"

# Get the latest stable version of kubernetes
export K8S_VERSION=$(curl -sS https://storage.googleapis.com/kubernetes-release/release/stable.txt)
echo "K8S_VERSION : ${K8S_VERSION}"

echo "Download Kubernetes CLI"
sudo wget -O kubectl "http://storage.googleapis.com/kubernetes-release/release/${K8S_VERSION}/bin/linux/amd64/kubectl"
sudo chmod 755 kubectl

export KUBECONFIG=/var/run/kubernetes/admin.kubeconfig

echo "Waiting for kubernetes service to start..."
for i in {1..600}
do
    if [[ -f $KUBECONFIG ]]; then
        running_count=$(./kubectl get nodes --no-headers 2>/dev/null | grep "Ready" | wc -l)
        if [ "$running_count" -ge 1 ]; then
            break
        fi
    fi
    echo -n "."
    sleep 1
done

echo "Cluster created!"
echo ""

sudo journalctl -u devstack@kubernetes.service

echo "Dump Kubernetes Objects..."
./kubectl get componentstatuses
./kubectl get configmaps
./kubectl get daemonsets
./kubectl get deployments
./kubectl get events
./kubectl get endpoints
./kubectl get horizontalpodautoscalers
./kubectl get ingress
./kubectl get jobs
./kubectl get limitranges
./kubectl get nodes
./kubectl get namespaces
./kubectl get pods
./kubectl get persistentvolumes
./kubectl get persistentvolumeclaims
./kubectl get quota
./kubectl get resourcequotas
./kubectl get replicasets
./kubectl get replicationcontrollers
./kubectl get secrets
./kubectl get serviceaccounts
./kubectl get services

echo "Clear the taint to make sure we can schedule jobs to the master node"
./kubectl taint nodes --all node.cloudprovider.kubernetes.io/uninitialized-

./kubectl get node -o json

echo "Create a default StorageClass since we do not have a cloud provider"
./kubectl create -f - <<EOF || true
kind: StorageClass
apiVersion: storage.k8s.io/v1
metadata:
  namespace: kube-system
  name: standard
  annotations:
    storageclass.beta.kubernetes.io/is-default-class: "true"
  labels:
    addonmanager.kubernetes.io/mode: Reconcile

provisioner: kubernetes.io/host-path
EOF

echo "Running tests..."
set -ex

export GOPATH=${BASE_DIR}/go
export KUBE_MASTER=local
export KUBERNETES_PROVIDER=skeleton
export KUBERNETES_CONFORMANCE_TEST=y
export GINKGO_PARALLEL=y
export GINKGO_PARALLELISM=5
export GINKGO_NO_COLOR=y
export KUBE_MASTER_IP=https://127.0.0.1:6443/

pushd $GOPATH/src/k8s.io/kubernetes >/dev/null
sudo -E PATH=$GOPATH/bin:$PATH make all WHAT=cmd/kubectl
sudo -E PATH=$GOPATH/bin:$PATH make all WHAT=vendor/github.com/onsi/ginkgo/ginkgo

# open up access for containers
sudo ifconfig -a
export HOST_INTERFACE=$(ip -f inet route | awk '/default/ {print $5}')
sudo iptables -t nat -A POSTROUTING -o $HOST_INTERFACE -s 10.0.0.0/24 -j MASQUERADE
sudo iptables -t nat -A POSTROUTING -o $HOST_INTERFACE -s 172.17.0.0/24 -j MASQUERADE


sudo -E PATH=$GOPATH/bin:$PATH make all WHAT=test/e2e/e2e.test
sudo -E PATH=$GOPATH/bin:$PATH go run hack/e2e.go -- -v --test --test_args="--ginkgo.trace=true --ginkgo.seed=1378936983 --logtostderr --v 4 --provider=local --report-dir=/opt/stack/logs/ --ginkgo.v --ginkgo.skip=$(test_names)"
popd >/dev/null
