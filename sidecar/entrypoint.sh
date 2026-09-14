#!/bin/sh
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

# Entrypoint wrapper for the os-brick gRPC sidecar.
#
# The host's /run is mounted at /host/run (instead of /run) to avoid
# conflicting with CRI-O's internal overlay storage at
# /run/containers/storage/.  This script symlinks the host runtime
# directories that os-brick tools need into /run before starting the
# gRPC server.

set -eu

HOST_RUN="${HOST_RUN_DIR:-/host/run}"

# Directories used by os-brick initiator connectors (iSCSI, FC,
# multipath, LVM, NVMe).
for dir in udev lvm lock multipath multipathd iscsi nvme; do
    if [ -e "${HOST_RUN}/${dir}" ]; then
        ln -sfn "${HOST_RUN}/${dir}" "/run/${dir}"
    fi
done

# Individual sockets / files that live directly under /run.
for entry in multipathd.sock iscsid.pid; do
    if [ -e "${HOST_RUN}/${entry}" ]; then
        ln -sfn "${HOST_RUN}/${entry}" "/run/${entry}"
    fi
done

exec python -m osbrick.main "$@"
