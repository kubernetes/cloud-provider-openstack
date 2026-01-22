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
# ./release-image-digests.sh registry.k8s.io/images/k8s-staging-provider-os/images.yaml [v1.35.0] [v1.34.1]

YAML_FILE=${1:?Usage: $0 <yaml_file> [<tag>...]}
TAGS="${@:2}"

# fail if file does not exist
if [ ! -f "${YAML_FILE}" ]; then
  echo "ERROR: File ${YAML_FILE} does not exist" >&2
  exit 1
fi

if [ -z "$TAGS" ]; then
  echo "Processing existing tags in ${YAML_FILE}..."
  TAGS=$(yq '[.[] | .dmap | to_entries[] | .value[]] | unique | sort | .[]' "${YAML_FILE}")
fi

IMAGES=$(yq '.[] | .name' "${YAML_FILE}")
for TAG in $TAGS; do
for IMAGE in $IMAGES; do
  #echo "Processing image: $IMAGE:$TAG)"
  digest="$(curl -sI "https://gcr.io/v2/k8s-staging-provider-os/${IMAGE}/manifests/${TAG}" | awk '/(i?)docker-content-digest/{ gsub(/\r/, ""); print tolower($NF)}')"
  if [ -z "$digest" ]; then
    echo "ERROR: gcr.io/k8s-staging-provider-os/$IMAGE:$TAG digest is empty" >&2
    continue
  fi

  # add new digest -> tag mapping
  yq -i '(.[] | select(.name == "'"${IMAGE}"'") | .dmap["'"${digest}"'" | . style="double"]) = (["'"${TAG}"'" | . style="double"] | . style="flow")' "${YAML_FILE}"
  # add/replace existing digest -> tag mapping
  # yq -i '(.[] | select(.name == "'"${IMAGE}"'") | .dmap) |= with_entries(select(.value[] | contains("'"${TAG}"'") | not)) | (.[] | select(.name == "'"${IMAGE}"'") | .dmap["'"${digest}"'" | . style="double"]) = (["'"${TAG}"'" | . style="double"] | . style="flow") ' "${YAML_FILE}"
done
done

echo "YAML file updated: $YAML_FILE"
