#!/usr/bin/env bash
# Copyright 2026 The Kubernetes Authors.
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

# Generate Go protobuf/gRPC stubs for the os-brick sidecar API.
#
# Prerequisites:
#   protoc              >= 25.0   (https://github.com/protocolbuffers/protobuf/releases)
#   protoc-gen-go       >= 1.34   (go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)
#   protoc-gen-go-grpc  >= 1.5    (go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest)
#
# Usage (from repository root):
#   hack/generate-osbrick-proto.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROTO_FILE="proto/osbrick/v1/connector.proto"
OUT_DIR="${REPO_ROOT}"

cd "${REPO_ROOT}"

echo "Generating Go stubs from ${PROTO_FILE}..."

protoc \
  --go_out="${OUT_DIR}" \
  --go_opt=module=k8s.io/cloud-provider-openstack \
  --go-grpc_out="${OUT_DIR}" \
  --go-grpc_opt=module=k8s.io/cloud-provider-openstack \
  "${PROTO_FILE}"

echo "Generated files:"
ls -la pkg/util/brick/gen/osbrickv1/
