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

TESTS_LIST=(
    'Certificates API [It] should support building a client with a CSR'
    'Downward API volume [It] should update annotations on modification [Conformance] [Volume]'
    'DNS [It] should provide DNS for the cluster [Conformance]'
    'Kubectl client [k8s.io] Simple pod [It] should support exec through an HTTP proxy'
    'Networking [It] should provide Internet connection for containers [Conformance]'
    'PersistentVolumes [Volume] [k8s.io] PersistentVolumes:NFS with Single PV - PVC pairs [It] create a PV and a pre-bound PVC: test write access'
    'PersistentVolumes [Volume] [k8s.io] PersistentVolumes:NFS with Single PV - PVC pairs [It] should create a non-pre-bound PV and PVC: test write access'
    'PersistentVolumes [Volume] [k8s.io] PersistentVolumes:NFS with multiple PVs and PVCs all in same ns [It] should create 4 PVs and 2 PVCs: test write access'
    'Projected [It] should update labels on modification [Conformance] [Volume]'
    'Services [It] should create endpoints for unready pods'
    'StatefulSet [k8s.io] Basic StatefulSet functionality [StatefulSetBasic] [It] should adopt matching orphans and release non-matching pods'
    'StatefulSet [k8s.io] Basic StatefulSet functionality [StatefulSetBasic] [It] should allow template updates'
    'StatefulSet [k8s.io] Basic StatefulSet functionality [StatefulSetBasic] [It] should not deadlock when a pod predecessor fails'
    'StatefulSet [k8s.io] Basic StatefulSet functionality [StatefulSetBasic] [It] should provide basic identity'
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
    for name in "${TESTS_LIST[@]}"; do
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
        running_count=$(./kubectl get svc --no-headers 2>/dev/null | grep "kubernetes" | wc -l)
        if [ "$running_count" -ge 1 ]; then
            break
        fi
    fi
    echo -n "."
    sleep 1
done

echo "Cluster created!"
echo ""

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


echo "Running tests..."
set -ex

export GOPATH=${BASE_DIR}/go
export KUBE_MASTER=local
export KUBERNETES_PROVIDER=skeleton
export KUBERNETES_CONFORMANCE_TEST=y
export GINKGO_PARALLEL=y
export GINKGO_NO_COLOR=y
export KUBE_MASTER_IP=https://127.0.0.1:6443/

pushd $GOPATH/src/k8s.io/kubernetes >/dev/null
sudo -E PATH=$GOPATH/bin:$PATH make all WHAT=cmd/kubectl
sudo -E PATH=$GOPATH/bin:$PATH make all WHAT=vendor/github.com/onsi/ginkgo/ginkgo

# open up access for containers
sudo iptables -t nat -A POSTROUTING -o ens3 -s 10.0.0.0/24 -j MASQUERADE
sudo iptables -t nat -A POSTROUTING -o ens3 -s 172.17.0.0/24 -j MASQUERADE

sudo -E PATH=$GOPATH/bin:$PATH make all WHAT=test/e2e/e2e.test
sudo -E PATH=$GOPATH/bin:$PATH go run hack/e2e.go -- -v --test --test_args="--ginkgo.trace=true --ginkgo.seed=1378936983 --logtostderr --v 4 --report-dir=/opt/stack/logs/ --ginkgo.v --ginkgo.skip=$(test_names)"
popd >/dev/null
