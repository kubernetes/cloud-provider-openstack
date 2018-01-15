#!/bin/bash -xe
# test-setup.sh - Install required stuffs
# Used in both CI jobs and locally
#
# Install the following tools:
# * glide

# Get OS
case $(uname -s) in
    Darwin)
        OS=darwin
        ;;
    Linux)
        if LSB_RELEASE=$(which lsb_release); then
            OS=$($LSB_RELEASE -s -c)
        else
            # No lsb-release, trya hack or two
            if which dpkg 1>/dev/null; then
                OS=debian
            elif which yum 1>/dev/null || which dnf 1>/dev/null; then
                OS=redhat
            else
                echo "Linux distro not yet supported"
                exit 1
            fi
        fi
        ;;
    *)
        echo "Unsupported OS"
        exit 1
        ;;
esac

case $OS in
    darwin)
        if which brew 1>/dev/null; then
            if ! which glide 1>/dev/null; then
                brew install glide
            fi
        else
            echo "Homebrew not found, install Glide from source?"
        fi
        ;;
    xenial|zesty)
        APT_GET="DEBIAN_FRONTEND=noninteractive \
            apt-get -q --option "Dpkg::Options::=--force-confold" \
            --assume-yes"
        if ! which add-apt-repository 1>/dev/null; then
            sudo $APT_GET install software-properties-common
        fi
        sudo add-apt-repository --yes ppa:gophers/archive
        sudo apt-get update && sudo $APT_GET install golang-1.9-go
        sudo ln -s /usr/lib/go-1.9/bin/go /usr/local/bin
        sudo ln -s /usr/lib/go-1.9/bin/gofmt /usr/local/bin
        wget -O install-glide.sh https://glide.sh/get
        export GOPATH=$HOME/go
        export PATH=$PATH:$HOME/go/bin
        mkdir -p $HOME/go/bin
        chmod +x install-glide.sh
        sudo -E PATH=$PATH ./install-glide.sh
        ;;
esac
