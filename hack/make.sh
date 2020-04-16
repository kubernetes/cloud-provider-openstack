#!/bin/bash

# Copyright 2018 The Kubernetes Authors.
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

# Simple script to offer the ability to run the make directives in a Docker Container.
# In other words you don't need a Go env on your system to run Make (build, test etc)
# This script will just bind-mount the source directory into a container under the correct
# GOPATH and handle all of the Go ENV stuff for you.  All you need is Docker
docker run -it -v "$PWD":/go/src/k8s.io/cloud-provider-openstack:z \
	-w /go/src/k8s.io/cloud-provider-openstack \
	golang:1.13 make $1
