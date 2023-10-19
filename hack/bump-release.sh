#!/bin/sh

# Copyright 2023 The Kubernetes Authors.
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

FROM_MAJOR="${1:?FROM_MAJOR (1st arg) not set or empty}"
TO_MAJOR="${2:?TO_MAJOR (2nd arg) not set or empty}"
TO_MINOR="${3:?TO_MINOR (3rd arg) not set or empty}"

# example usage: hack/bump_release.sh 28 28 1
# should replace 1.28.x with 1.28.1 / 2.28.x with 2.28.1

find charts docs manifests tests examples -type f -exec sed -i -re 's/((ersion)?: ?v?)?([1-2]\.)'${FROM_MAJOR}'\.([0-9][0-9a-zA-Z.-]*)/\1\3'${TO_MAJOR}'.'${TO_MINOR}'/g' "{}" \;
