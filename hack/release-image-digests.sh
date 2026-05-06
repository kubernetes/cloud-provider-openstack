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

usage() {
  cat <<EOF
Usage: $0 <yaml_file> [<tag>...]

Populate an image promoter manifest with content-digest-to-tag mappings for
staged container images.

The Kubernetes image promoter promotes images from staging to production using
a manifest that maps content digests (sha256:...) to tags. This script
automates the step of looking up those digests from the GCR staging registry
(gcr.io/k8s-staging-provider-os) after images have been pushed, and writing
the dmap entries into the manifest YAML.

Run this after cutting a release to update the promoter manifest before opening
the promotion PR.

Arguments:
  yaml_file   Path to the image promoter manifest, e.g.
              registry.k8s.io/images/k8s-staging-provider-os/images.yaml
  tag         One or more release tags to process, e.g. v1.35.0 v1.34.1.
              If omitted, all tags already present in the manifest are used.

Examples:
  $0 registry.k8s.io/images/k8s-staging-provider-os/images.yaml v1.35.0
  $0 registry.k8s.io/images/k8s-staging-provider-os/images.yaml v1.35.0 v1.34.1
  $0 registry.k8s.io/images/k8s-staging-provider-os/images.yaml
EOF
}

ARGS=$(getopt -o h --long help -n "$0" -- "$@") || { usage >&2; exit 1; }
eval set -- "$ARGS"
while true; do
  case "$1" in
    -h|--help) usage; exit 0;;
    --) shift; break;;
  esac
done

YAML_FILE=${1:?Usage: $0 <yaml_file> [<tag>...]}
shift
TAGS="$*"

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
