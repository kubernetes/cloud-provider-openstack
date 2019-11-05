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

function install_prereqs {
   # Install pre-reqs
    $BASE_DIR/tools/install-distro-packages.sh
    $BASE_DIR/tools/test-setup.sh

    install_package nfs-common
}


function install_docker {
    # Install docker if needed
    if ! is_package_installed docker-engine; then
        sudo apt-key adv --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 58118E89F3A912897C070ADBF76221572C52609D || true
        sudo apt-key adv --keyserver hkp://pgp.mit.edu:80 --recv-keys 58118E89F3A912897C070ADBF76221572C52609D || true
        sudo apt-add-repository 'deb http://apt.dockerproject.org/repo ubuntu-xenial main'
        sudo apt-get update
        sudo apt-cache policy docker-engine
        sudo apt-get install -y docker-engine=1.12.6-0~ubuntu-xenial
        sudo cat /lib/systemd/system/docker.service
        sudo sed -r -i "s|(ExecStart)=(.+)|\1=\2 --iptables=false|" /lib/systemd/system/docker.service
        sudo cat /lib/systemd/system/docker.service
        sudo systemctl daemon-reload
        sudo systemctl restart docker
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

    # golang env details
    go env
    go version

    GO111MODULE=off go get -u github.com/jteeuwen/go-bindata/go-bindata || true
    GO111MODULE=off go get -u github.com/cloudflare/cfssl/cmd/... || true

    # Get Kubernetes from source
    mkdir -p ${GOPATH}/src/k8s.io/
    if [ ! -d "${K8S_SRC}" ]; then
        git clone https://${CONFORMANCE_REPO} ${K8S_SRC}
        pushd ${K8S_SRC} >/dev/null
        git remote update
        git fetch --all --tags --prune

        popd >/dev/null
    fi

    # Run the script that builds kubernetes from source and starts the processes
    pushd ${K8S_SRC} >/dev/null

    # local-up-cluster needs the etcd path and the GOPATH bin as cf-ssl
    # and go-bindata are located there
    export PATH=$DEST/bin:$GOPATH/bin:${PATH}

    # Seed the log files so devstack-gate can capture the logs
    export LOG_DIR=${SCREEN_LOGDIR:-/opt/stack/logs}
    sudo mkdir -p $LOG_DIR
    sudo touch $LOG_DIR/kube-apiserver.log;sudo ln -sf $LOG_DIR/kube-apiserver.log $LOG_DIR/screen-kube-apiserver.log
    sudo touch $LOG_DIR/kube-controller-manager.log;sudo ln -sf $LOG_DIR/kube-controller-manager.log $LOG_DIR/screen-kube-controller-manager.log
    sudo touch $LOG_DIR/kube-proxy.log;sudo ln -sf $LOG_DIR/kube-proxy.log $LOG_DIR/screen-kube-proxy.log
    sudo touch $LOG_DIR/kube-scheduler.log;sudo ln -sf $LOG_DIR/kube-scheduler.log $LOG_DIR/screen-kube-scheduler.log
    sudo touch $LOG_DIR/kubelet.log;sudo ln -sf $LOG_DIR/kubelet.log $LOG_DIR/screen-kubelet.log

    echo "Stopping firewall and allow all traffic..."
    sudo iptables -F
    sudo iptables -X
    sudo iptables -t nat -F
    sudo iptables -t nat -X
    sudo iptables -t mangle -F
    sudo iptables -t mangle -X
    sudo iptables -P INPUT ACCEPT
    sudo iptables -P FORWARD ACCEPT
    sudo iptables -P OUTPUT ACCEPT

    # Turn on/off a few things in local-up-cluster.sh
    export ALLOW_PRIVILEGED=true
    export ALLOW_SECURITY_CONTEXT=true
    export CLOUD_PROVIDER=external
    export ENABLE_CRI=false
    export ENABLE_DAEMON=true
    export ENABLE_HOSTPATH_PROVISIONER=true
    export ENABLE_SINGLE_CA_SIGNER=true
    export HOSTNAME_OVERRIDE=$(ip route get 1.1.1.1 | awk '{print $7}')
    export KUBE_ENABLE_CLUSTER_DASHBOARD=true
    export KUBE_ENABLE_CLUSTER_DNS=true
    export LOG_LEVEL=10

    pushd ${BASE_DIR}
    make build
    popd

    export EXTERNAL_CLOUD_PROVIDER_BINARY=${BASE_DIR}/openstack-cloud-controller-manager
    export EXTERNAL_CLOUD_PROVIDER=true
    export CLOUD_PROVIDER=openstack
    export CLOUD_CONFIG=${CLOUD_CONFIG_FILE}

    sudo pwd
    if [[ ! -d "${CLOUD_CONFIG_DIR}" ]]; then
        sudo mkdir -p ${CLOUD_CONFIG_DIR}
    fi
    sudo chown $STACK_USER /etc/kubernetes/
    iniset ${CLOUD_CONFIG} Global region RegionOne
    iniset ${CLOUD_CONFIG} Global username admin
    iniset ${CLOUD_CONFIG} Global password secretadmin
    iniset ${CLOUD_CONFIG} Global auth-url "http://localhost/identity"
    iniset ${CLOUD_CONFIG} Global tenant-id $(openstack project show admin -f value -c id)
    iniset ${CLOUD_CONFIG} Global domain-id default
    iniset ${CLOUD_CONFIG} LoadBalancer subnet-id $(openstack subnet show lb-mgmt-subnet -f value -c id)
    iniset ${CLOUD_CONFIG} LoadBalancer floating-network-id $(openstack network show public -f value -c id)
    iniset ${CLOUD_CONFIG} BlockStorage bs-version v2

    # Listen on all interfaces
    export KUBELET_HOST="0.0.0.0"

    # Use the docker0's ip address for kubedns to work
    export API_HOST_IP="172.17.0.1"

    # kill etcd and let local-up-cluster start it up
    $SYSTEMCTL stop $ETCD_SYSTEMD_SERVICE

    # local-up-cluster.sh compiles everything from source and starts the services.
    sudo -E PATH=$PATH SHELLOPTS=$SHELLOPTS ./hack/local-up-cluster.sh

    popd >/dev/null
}

# cleanup_k8s_cloud_provider() - Remove residual data files, anything left over from previous
# runs that a clean run would need to clean up
function cleanup_k8s_cloud_provider {
    echo_summary "Cleaning up Devstack Plugin for k8s-cloud-provider"
    # Kill the k8s processes
    ps -ef | grep -e hyperkube | grep -v grep | awk '{print $2}' | xargs sudo kill -9

    # Cleanup docker images and containers
    sudo docker rm -f $(docker ps -a -q) || true
    sudo docker rmi -f $(docker images -q -a) || true

    # Stop docker
    sudo systemctl stop docker.service
    sudo rm -rf "$K8S_SRC"
}

function stop_k8s_cloud_provider {
    echo_summary "Stop Devstack Plugin for k8s-cloud-provider"
    stop_process kubernetes
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
        # no-op
        :
    elif [[ "$1" == "stack" && "$2" == "extra"  ]]; then
        install_k8s_cloud_provider
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
