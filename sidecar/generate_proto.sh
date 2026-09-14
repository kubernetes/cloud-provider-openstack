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

# Generate Python gRPC stubs from the os-brick protobuf definition.
#
# Prerequisites:
#   pip install grpcio-tools
#
# Usage (from the sidecar/ directory):
#   ./generate_proto.sh
#
# Or from the repository root:
#   sidecar/generate_proto.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROTO_FILE="${REPO_ROOT}/proto/osbrick/v1/connector.proto"
OUT_DIR="${SCRIPT_DIR}/osbrick/gen"

mkdir -p "${OUT_DIR}"
touch "${OUT_DIR}/__init__.py"

echo "Generating Python stubs from ${PROTO_FILE}..."

python -m grpc_tools.protoc \
  --proto_path="${REPO_ROOT}/proto/osbrick/v1" \
  --python_out="${OUT_DIR}" \
  --grpc_python_out="${OUT_DIR}" \
  "${PROTO_FILE}"

# Fix the import in the generated gRPC stub: protoc generates a bare
# "import connector_pb2" which doesn't work when the file lives inside
# the osbrick.gen package.
sed -i 's/^import connector_pb2/from osbrick.gen import connector_pb2/' \
  "${OUT_DIR}/connector_pb2_grpc.py"

echo "Generated files:"
ls -la "${OUT_DIR}"/connector_pb2*.py
