#!/bin/bash
#
# lib/dlm
#
# Functions to control the installation and configuration of kubernetes with the
# external OpenStack cloud provider enabled.

# Dependencies:
#
# - ``functions`` file

# ``stack.sh`` calls the entry points in this order:
#
# - install_k8s_cloud_provider
# - configure_k8s_cloud_provider
# - cleanup_dlm

# Save trace setting
_XTRACE_K8S_PROVIDER=$(set +o | grep xtrace)
set -o xtrace


BASE_DIR=$(cd $(dirname $BASH_SOURCE)/.. && pwd)

# Defaults
# --------

export GOPATH=${BASE_DIR}/go

CONFORMANCE_REPO=${CONFORMANCE_REPO:-github.com/kubernetes/kubernetes}
K8S_SRC=${GOPATH}/src/k8s.io/kubernetes
ETCD_VERSION=v3.1.4

function install_prereqs {
   # Install pre-reqs
    $BASE_DIR/tools/install-distro-packages.sh
    $BASE_DIR/tools/test-setup.sh
}


function install_docker {
    # Install docker if needed
    if ! is_package_installed docker-engine; then
        curl -sSL https://get.docker.io | sudo bash
    fi
    docker --version

    # Get the latest stable version of kubernetes
    export K8S_VERSION=$(curl -sS https://storage.googleapis.com/kubernetes-release/release/stable.txt)
    echo "K8S_VERSION : ${K8S_VERSION}"

    echo "Starting docker service"
    sudo systemctl enable docker.service
    sudo systemctl start docker.service --ignore-dependencies
    echo "Checking docker service"
    sudo docker ps
}

function install_k8s_cloud_provider {
    echo_summary "Installing Devstack Plugin for k8s-cloud-provider"

    # upstream kubernetes has not moved to 1.8 yet, so use 1.7.5
    sudo curl -sL -o ./gimme https://raw.githubusercontent.com/travis-ci/gimme/master/gimme
    sudo chmod +x ./gimme
    sudo ./gimme 1.7.5
    env | grep HOME
    source $HOME/.gimme/envs/go1.7.5.env

    # golang env details
    go env
    go version

    # Get Kubernetes from source
    mkdir -p ${GOPATH}/src/k8s.io/
    if [ ! -d "${K8S_SRC}" ]; then
        git clone https://${CONFORMANCE_REPO} ${K8S_SRC}
    fi
    go get -u github.com/jteeuwen/go-bindata/go-bindata || true
    go get -u github.com/cloudflare/cfssl/cmd/... || true

    # Run the script that builds kubernetes from source and starts the processes
    pushd ${K8S_SRC} >/dev/null
    hack/install-etcd.sh
    export PATH=${K8S_SRC}/third_party/etcd:$GOPATH/bin:${PATH}

    # Seed the log files so devstack-gate can capture the logs
    export LOG_DIR=${SCREEN_LOGDIR:-/opt/stack/logs}
    sudo mkdir -p $LOG_DIR
    sudo touch $LOG_DIR/kube-apiserver.log;sudo ln -s $LOG_DIR/kube-apiserver.log $LOG_DIR/screen-kube-apiserver.log
    sudo touch $LOG_DIR/kube-controller-manager.log;sudo ln -s $LOG_DIR/kube-controller-manager.log $LOG_DIR/screen-kube-controller-manager.log
    sudo touch $LOG_DIR/kube-proxy.log;sudo ln -s $LOG_DIR/kube-proxy.log $LOG_DIR/screen-kube-proxy.log
    sudo touch $LOG_DIR/kube-scheduler.log;sudo ln -s $LOG_DIR/kube-scheduler.log $LOG_DIR/screen-kube-scheduler.log
    sudo touch $LOG_DIR/kubelet.log;sudo ln -s $LOG_DIR/kubelet.log $LOG_DIR/screen-kubelet.log

    # Turn on a few things in local-up-cluster.sh
    export ALLOW_PRIVILEGED=true
    export ALLOW_SECURITY_CONTEXT=true
    export ALLOW_ANY_TOKEN=true

    run_process kubernetes "sudo -E PATH=$PATH hack/local-up-cluster.sh"
    popd >/dev/null
}

# cleanup_k8s_cloud_provider() - Remove residual data files, anything left over from previous
# runs that a clean run would need to clean up
function cleanup_k8s_cloud_provider {
    echo_summary "Cleaning up Devstack Plugin for k8s-cloud-provider"
    sudo rm -rf "$K8S_SRC"
    sudo rm -rf "$DEST/etcd"
}

function stop_k8s_cloud_provider {
    echo_summary "Stop Devstack Plugin for k8s-cloud-provider"
    stop_process kubernetes
    stop_process etcd-server
}

# check for service enabled
if is_service_enabled k8s-cloud-provider; then

    if [[ "$1" == "stack" && "$2" == "pre-install"  ]]; then
        # no-op
        :

    elif [[ "$1" == "stack" && "$2" == "install"  ]]; then
        install_prereqs
        install_docker

    elif [[ "$1" == "stack" && "$2" == "post-config"  ]]; then
        install_k8s_cloud_provider

    elif [[ "$1" == "stack" && "$2" == "extra"  ]]; then
        # no-op
        :
    fi

    if [[ "$1" == "unstack"  ]]; then
        stop_k8s_cloud_provider
    fi

    if [[ "$1" == "clean"  ]]; then
        cleanup_k8s_cloud_provider
    fi
fi

# Restore xtrace
$_XTRACE_K8S_PROVIDER

# Tell emacs to use shell-script-mode
## Local variables:
## mode: shell-script
## End:
