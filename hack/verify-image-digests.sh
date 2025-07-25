#!/bin/bash

# Copyright 2025 The Kubernetes Authors.
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

# example:
# ./verify-image-digests.sh 'v1.33.*' registry.k8s.io/images/k8s-staging-provider-os/images.yaml

# default to match all images
MATCH=${1:-'.*'}

# fail if $2 is not set
YAML_FILE=${2:?Usage: $0 '<match>' <yaml_file>}

# fail if file does not exist
if [ ! -f "${YAML_FILE}" ]; then
  echo "ERROR: File ${YAML_FILE} does not exist" >&2
  exit 1
fi

while read -r IMAGE DIGEST TAG; do
  #echo "image=$IMAGE, digest='$DIGEST', tag=$TAG"
  digest="$(curl -sI "https://gcr.io/v2/k8s-staging-provider-os/${IMAGE}/manifests/${TAG}" | awk '/(i?)docker-content-digest/{ gsub(/\r/, ""); print tolower($NF)}')"
  if [ -z "$digest" ]; then
    echo "ERROR: gcr.io/k8s-staging-provider-os/$IMAGE:$TAG digest is empty" >&2
    continue
  fi
  if [ "$digest" != "$DIGEST" ]; then
    echo "ERROR: gcr.io/k8s-staging-provider-os/$IMAGE:$TAG digest mismatch: expected $DIGEST, got $digest" >&2
  fi
  digest1="$(curl -sIL "https://registry.k8s.io/v2/provider-os/${IMAGE}/manifests/${TAG}" | awk '/(i?)docker-content-digest/{ gsub(/\r/, ""); print tolower($NF)}')"
  if [ -z "$digest1" ]; then
    echo "ERROR: registry.k8s.io/provider-os/$IMAGE:$TAG digest is empty" >&2
    continue
  fi
  if [ "$digest1" != "$DIGEST" ]; then
    echo "ERROR: registry.k8s.io/provider-os/$IMAGE:$TAG digest mismatch: expected $DIGEST, got $digest1" >&2
  fi
done <<< `yq '.[] | .name as $name | .dmap | to_entries | sort_by(.value[0]) | reverse | .[] | select(.value[0] | test("'"${MATCH}"'")) | "\($name) \(.key) \(.value[0])"' "${YAML_FILE}"`
