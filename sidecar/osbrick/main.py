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

"""Entry point for the os-brick gRPC sidecar.

Starts a gRPC server on a Unix domain socket and serves the
OsBrickConnector service. The Cinder CSI driver communicates with
this sidecar to perform volume attach/detach operations for nodes
using direct attachment mode (typically bare-metal nodes).
"""

import logging
import os
import signal
import sys
from concurrent import futures

import grpc
from oslo_config import cfg
from grpc_health.v1 import health
from grpc_health.v1 import health_pb2
from grpc_health.v1 import health_pb2_grpc

from osbrick.gen import connector_pb2_grpc
from osbrick.server import OsBrickConnectorServicer

LOG = logging.getLogger(__name__)

DEFAULT_SOCKET_PATH = "/var/run/osbrick/osbrick.sock"
SERVICE_NAME = "osbrick.v1.OsBrickConnector"


def _configure_logging():
    """Set up structured logging to stderr."""
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        stream=sys.stderr,
    )


def _create_server(socket_path: str) -> grpc.Server:
    """Create and configure the gRPC server."""
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))

    # Register the os-brick servicer.
    connector_pb2_grpc.add_OsBrickConnectorServicer_to_server(
        OsBrickConnectorServicer(), server
    )

    # Register the gRPC health check service.
    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set(
        SERVICE_NAME,
        health_pb2.HealthCheckResponse.SERVING,
    )
    # Also set the overall server health (empty service name).
    health_servicer.set(
        "",
        health_pb2.HealthCheckResponse.SERVING,
    )

    # Ensure the socket directory exists and remove any stale socket.
    socket_dir = os.path.dirname(socket_path)
    os.makedirs(socket_dir, exist_ok=True)
    try:
        os.remove(socket_path)
    except FileNotFoundError:
        pass

    server.add_insecure_port(f"unix://{socket_path}")
    return server


def _configure_oslo():
    """Set required oslo.concurrency lock_path for os-brick."""
    lock_dir = "/var/run/osbrick/locks"
    os.makedirs(lock_dir, exist_ok=True)
    cfg.CONF.set_override("lock_path", lock_dir, group="oslo_concurrency")


def main():
    _configure_logging()
    _configure_oslo()

    socket_path = os.environ.get("OSBRICK_SOCKET_PATH", DEFAULT_SOCKET_PATH)

    LOG.info("Starting os-brick sidecar on unix://%s", socket_path)
    server = _create_server(socket_path)
    server.start()
    LOG.info("os-brick sidecar is ready")

    def _handle_signal(signum, frame):
        LOG.info("Received signal %d, shutting down...", signum)
        server.stop(grace=5)

    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    server.wait_for_termination()
    LOG.info("os-brick sidecar stopped")


if __name__ == "__main__":
    main()
