#!/usr/bin/env bash
# plugin.sh - DevStack plugin.sh dispatch script zun-ui

BASE_DIR=$(cd $(dirname $BASH_SOURCE)/.. && pwd)

function install_k8s_cloud_provider {
    echo_summary "Installing Devstack Plugin"
}

function init_k8s_cloud_provider {
    echo_summary "Initialize Devstack Plugin"
}

function configure_k8s_cloud_provider {
    echo_summary "Configuring Devstack Plugin"
}

# check for service enabled
if is_service_enabled zun-ui; then

    if [[ "$1" == "stack" && "$2" == "pre-install"  ]]; then
        # Set up system services
        # no-op
        :

    elif [[ "$1" == "stack" && "$2" == "install"  ]]; then
        # Perform installation of service source
        # no-op
        :

    elif [[ "$1" == "stack" && "$2" == "post-config"  ]]; then
        # Configure after the other layer 1 and 2 services have been configured
        # no-op
        :

    elif [[ "$1" == "stack" && "$2" == "extra"  ]]; then
        # no-op
        :
    fi

    if [[ "$1" == "unstack"  ]]; then
        # no-op
        :
    fi

    if [[ "$1" == "clean"  ]]; then
        # Remove state and transient data
        # Remember clean.sh first calls unstack.sh
        # no-op
        :
    fi
fi
