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

"""gRPC servicer that wraps os-brick volume operations.

This module implements the OsBrickConnectorServicer interface defined
in the protobuf API (proto/osbrick/v1/connector.proto). Each RPC
delegates to the corresponding os-brick function.
"""

import json
import logging
import os
import socket
import subprocess

import grpc
from os_brick.initiator import connector as brick_connector

from osbrick.gen import connector_pb2
from osbrick.gen import connector_pb2_grpc

LOG = logging.getLogger(__name__)

# Root helper for os-brick operations. In a privileged container this
# is typically 'sudo' or '' (empty string if already root).
# Configurable via OSBRICK_ROOT_HELPER environment variable.
ROOT_HELPER = os.environ.get("OSBRICK_ROOT_HELPER", "sudo")

# Whether to report multipath capability in connector properties.
# Set OSBRICK_MULTIPATH=false on hosts without multipath-tools.
MULTIPATH = os.environ.get("OSBRICK_MULTIPATH", "true").lower() in (
    "true", "1", "yes",
)


def _get_my_ip():
    """Return the IP address of the host.

    Uses the OSBRICK_MY_IP environment variable if set, otherwise
    resolves the hostname. os-brick needs this for iSCSI initiator
    registration and connection tracking.
    """
    ip = os.environ.get("OSBRICK_MY_IP")
    if ip:
        return ip
    try:
        return socket.gethostbyname(socket.gethostname())
    except socket.gaierror:
        LOG.warning("Could not resolve hostname, falling back to 127.0.0.1")
        return "127.0.0.1"


class OsBrickConnectorServicer(connector_pb2_grpc.OsBrickConnectorServicer):
    """Maps gRPC calls to os-brick connector operations."""

    def GetConnectorProperties(self, request, context):
        """Return host initiator information from os-brick."""
        try:
            my_ip = _get_my_ip()
            props = brick_connector.get_connector_properties(
                ROOT_HELPER,
                my_ip,
                multipath=MULTIPATH,
                enforce_multipath=False,
            )
        except Exception as e:
            LOG.exception("get_connector_properties failed")
            context.abort(
                grpc.StatusCode.INTERNAL,
                f"get_connector_properties failed: {e}",
            )

        LOG.info("Connector properties: %s", props)

        # Map the os-brick dict to the proto message.
        #
        # The typed fields (initiator, wwpns, host, multipath) are set
        # for convenience and logging. The raw_json field carries the
        # complete dict as JSON, preserving original Python types
        # (bools, lists, ints) so the Go side can store it directly
        # in the node annotation and pass it to Cinder without type
        # coercion (e.g. Python True → string "True").
        extras = {}
        known_keys = {"initiator", "wwpns", "host", "multipath"}
        for key, value in props.items():
            if key not in known_keys:
                extras[key] = json.dumps(value) if not isinstance(value, str) else value

        return connector_pb2.ConnectorProperties(
            initiator=props.get("initiator", ""),
            wwpns=props.get("wwpns", []),
            host=props.get("host", ""),
            multipath=props.get("multipath", False),
            extras=extras,
            raw_json=json.dumps(props),
        )

    @staticmethod
    def _resolve_rbd_device(connection_info, symlink_path):
        """Look up the actual /dev/rbdN device for an RBD volume.

        os-brick returns a udev symlink path (/dev/rbd/<pool>/<image>)
        that may not exist when udev ceph-renamer rules are not in
        place (e.g. inside a container). Query 'rbd device list' to
        find the numeric device instead.
        """
        data = connection_info.get("data") or connection_info
        name = data.get("name", "")
        if "/" in name:
            pool, image = name.split("/", 1)
        else:
            pool, image = "", name

        if not pool or not image:
            LOG.warning(
                "_resolve_rbd_device: could not parse pool/image from "
                "connection_info name=%s, returning symlink path as-is",
                name,
            )
            return symlink_path

        try:
            out = subprocess.check_output(
                ["rbd", "device", "list", "--format", "json"],
                stderr=subprocess.STDOUT,
            )
            devices = json.loads(out)
            for dev in devices:
                if dev.get("pool") == pool and dev.get("name") == image:
                    real_path = dev.get("device", "")
                    LOG.info(
                        "_resolve_rbd_device: resolved %s -> %s",
                        symlink_path,
                        real_path,
                    )
                    return real_path
        except Exception:
            LOG.exception("_resolve_rbd_device: failed to query rbd device list")

        # Fall through: return the original symlink path and hope
        # for the best.
        LOG.warning(
            "_resolve_rbd_device: could not resolve %s, returning as-is",
            symlink_path,
        )
        return symlink_path

    def ConnectVolume(self, request, context):
        """Attach a volume using os-brick and return the device path."""
        try:
            connection_info = json.loads(request.connection_info)
        except json.JSONDecodeError as e:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                f"Invalid connection_info JSON: {e}",
            )

        driver_volume_type = connection_info.get("driver_volume_type", "")
        if not driver_volume_type:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "connection_info missing driver_volume_type",
            )

        LOG.info(
            "ConnectVolume: driver_volume_type=%s", driver_volume_type
        )

        try:
            # For RBD volumes, do_local_attach=True maps the volume
            # as a kernel RBD device (/dev/rbd<N>) instead of
            # returning a librbd userspace handle. The CSI driver
            # needs a block device path to format and mount.
            extra_kwargs = {}
            if driver_volume_type == "rbd":
                extra_kwargs["do_local_attach"] = True

            conn = brick_connector.InitiatorConnector.factory(
                driver_volume_type,
                ROOT_HELPER,
                use_multipath=connection_info.get("multipath", False),
                **extra_kwargs,
            )
            device_info = conn.connect_volume(connection_info.get("data") or connection_info)
        except Exception as e:
            LOG.exception("connect_volume failed")
            context.abort(
                grpc.StatusCode.INTERNAL,
                f"connect_volume failed: {e}",
            )

        device_path = str(device_info.get("path", ""))
        multipath_device = device_info.get("multipath_device", "")

        # os-brick's RBD connector returns a udev symlink path
        # (/dev/rbd/<pool>/<image>) that requires the ceph-renamer
        # udev rules. In a container, udev typically isn't running
        # so the symlink may not exist. Fall back to the numeric
        # device by querying 'rbd device list'.
        if device_path and not os.path.exists(device_path) and driver_volume_type == "rbd":
            device_path = self._resolve_rbd_device(
                connection_info, device_path
            )

        LOG.info(
            "ConnectVolume: device_path=%s multipath_device=%s",
            device_path,
            multipath_device,
        )

        return connector_pb2.ConnectVolumeResponse(
            device_path=device_path,
            multipath_device=multipath_device,
        )

    def DisconnectVolume(self, request, context):
        """Detach a previously connected volume using os-brick."""
        try:
            connection_info = json.loads(request.connection_info)
        except json.JSONDecodeError as e:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                f"Invalid connection_info JSON: {e}",
            )

        driver_volume_type = connection_info.get("driver_volume_type", "")
        if not driver_volume_type:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "connection_info missing driver_volume_type",
            )

        LOG.info(
            "DisconnectVolume: driver_volume_type=%s", driver_volume_type
        )

        try:
            extra_kwargs = {}
            if driver_volume_type == "rbd":
                extra_kwargs["do_local_attach"] = True

            conn = brick_connector.InitiatorConnector.factory(
                driver_volume_type,
                ROOT_HELPER,
                use_multipath=connection_info.get("multipath", False),
                **extra_kwargs,
            )
            conn.disconnect_volume(
                connection_info.get("data") or connection_info,
                None,  # device_info — os-brick looks it up internally
            )
        except Exception as e:
            LOG.exception("disconnect_volume failed")
            context.abort(
                grpc.StatusCode.INTERNAL,
                f"disconnect_volume failed: {e}",
            )

        LOG.info("DisconnectVolume: success")
        return connector_pb2.DisconnectVolumeResponse()

    def ExtendVolume(self, request, context):
        """Rescan/extend a connected volume using os-brick."""
        try:
            connection_info = json.loads(request.connection_info)
        except json.JSONDecodeError as e:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                f"Invalid connection_info JSON: {e}",
            )

        driver_volume_type = connection_info.get("driver_volume_type", "")
        if not driver_volume_type:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "connection_info missing driver_volume_type",
            )

        LOG.info(
            "ExtendVolume: driver_volume_type=%s", driver_volume_type
        )

        try:
            extra_kwargs = {}
            if driver_volume_type == "rbd":
                extra_kwargs["do_local_attach"] = True

            conn = brick_connector.InitiatorConnector.factory(
                driver_volume_type,
                ROOT_HELPER,
                use_multipath=connection_info.get("multipath", False),
                **extra_kwargs,
            )
            conn.extend_volume(connection_info.get("data") or connection_info)
        except Exception as e:
            LOG.exception("extend_volume failed")
            context.abort(
                grpc.StatusCode.INTERNAL,
                f"extend_volume failed: {e}",
            )

        LOG.info("ExtendVolume: success")
        return connector_pb2.ExtendVolumeResponse()
