#!/bin/bash
#
# lib/dlm
#
# Functions to control the installation and configuration of kubernetes with the
# external OpenStack cloud provider enabled.

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

    install_package nfs-common
}


function install_docker {
    # Install docker if needed
    if ! is_package_installed docker-engine; then
        sudo apt-key adv --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 58118E89F3A912897C070ADBF76221572C52609D
        sudo apt-add-repository 'deb http://apt.dockerproject.org/repo ubuntu-xenial main'
        sudo apt-get update
        sudo apt-cache policy docker-engine
        sudo apt-get install -y docker-engine=1.12.6-0~ubuntu-xenial
        sudo cat /lib/systemd/system/docker.service
        sudo sed -r -i "s|(ExecStart)=(.+)|\1=\2 --iptables=false|" /lib/systemd/system/docker.service
        sudo cat /lib/systemd/system/docker.service
        sudo systemctl daemon-reload
        sudo systemctl restart docker
        sudo systemctl status docker
        sudo ifconfig -a
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

    go get -u github.com/jteeuwen/go-bindata/go-bindata || true
    go get -u github.com/cloudflare/cfssl/cmd/... || true

    # Get Kubernetes from source
    mkdir -p ${GOPATH}/src/k8s.io/
    if [ ! -d "${K8S_SRC}" ]; then
        git clone https://${CONFORMANCE_REPO} ${K8S_SRC}
        pushd ${K8S_SRC} >/dev/null
        git remote update
        git fetch --all --tags --prune
        #git checkout tags/v1.7.0-alpha.1
        popd >/dev/null
    fi

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

    # Turn on/off a few things in local-up-cluster.sh
    export ALLOW_PRIVILEGED=true
    export KUBE_ENABLE_CLUSTER_DNS=true
    export KUBE_ENABLE_CLUSTER_DASHBOARD=true
    export ALLOW_SECURITY_CONTEXT=true
    export ENABLE_HOSTPATH_PROVISIONER=true
    # Use the docker0's ip address for kubedns to work
    export API_HOST_IP="172.17.0.1"
    export KUBELET_HOST="0.0.0.0"
    export ENABLE_CRI=false

#    echo "Stop Docker iptable rules that interfere with kubedns"
#    sudo iptables -D FORWARD -j DOCKER-ISOLATION
#    sudo iptables -A DOCKER-ISOLATION -j RETURN
#    sudo iptables --flush DOCKER-ISOLATION
#    sudo iptables -X DOCKER-ISOLATION
    echo "Stopping firewall and allowing everything..."
    sudo iptables -F
    sudo iptables -X
    sudo iptables -t nat -F
    sudo iptables -t nat -X
    sudo iptables -t mangle -F
    sudo iptables -t mangle -X
    sudo iptables -P INPUT ACCEPT
    sudo iptables -P FORWARD ACCEPT
    sudo iptables -P OUTPUT ACCEPT

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
        install_docker
        install_prereqs

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
