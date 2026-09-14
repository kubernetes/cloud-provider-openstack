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

"""Unit tests for the os-brick gRPC servicer.

These tests mock os_brick.initiator.connector to avoid requiring
actual storage hardware or root privileges.
"""

import json
import unittest
from unittest import mock

import grpc
from grpc import StatusCode
from grpc_testing import server_from_dictionary, strict_real_time

# The generated stubs must be on sys.path. When running from the
# sidecar directory:  PYTHONPATH=. python -m pytest tests/
from osbrick.gen import connector_pb2
from osbrick.gen import connector_pb2_grpc

# Import after gen so the server module can resolve its imports.
from osbrick import server


def _make_test_server():
    """Create a gRPC test server with the os-brick servicer."""
    servicers = {
        connector_pb2.DESCRIPTOR.services_by_name[
            "OsBrickConnector"
        ]: server.OsBrickConnectorServicer(),
    }
    return server_from_dictionary(servicers, strict_real_time())


class TestGetConnectorProperties(unittest.TestCase):
    @mock.patch("osbrick.server._get_my_ip", return_value="10.0.0.1")
    @mock.patch("osbrick.server.brick_connector.get_connector_properties")
    def test_returns_properties(self, mock_get_props, mock_ip):
        mock_get_props.return_value = {
            "initiator": "iqn.2025-01.com.example:node1",
            "wwpns": ["50060b0000c26040"],
            "host": "node1",
            "multipath": True,
            "sdc_guid": "ABC123",
        }

        test_server = _make_test_server()
        request = connector_pb2.GetConnectorPropertiesRequest()
        method = test_server.invoke_unary_unary(
            connector_pb2.DESCRIPTOR.services_by_name[
                "OsBrickConnector"
            ].methods_by_name["GetConnectorProperties"],
            (),
            request,
            None,
        )
        response, _, code, _ = method.termination()

        self.assertEqual(code, StatusCode.OK)
        self.assertEqual(response.initiator, "iqn.2025-01.com.example:node1")
        self.assertEqual(list(response.wwpns), ["50060b0000c26040"])
        self.assertEqual(response.host, "node1")
        self.assertTrue(response.multipath)
        self.assertEqual(response.extras["sdc_guid"], "ABC123")


class TestConnectVolume(unittest.TestCase):
    @mock.patch("osbrick.server.brick_connector.InitiatorConnector.factory")
    def test_connect_volume(self, mock_factory):
        mock_connector = mock.MagicMock()
        mock_connector.connect_volume.return_value = {
            "path": "/dev/sdb",
            "multipath_device": "/dev/dm-3",
        }
        mock_factory.return_value = mock_connector

        connection_info = json.dumps({
            "driver_volume_type": "iscsi",
            "data": {"target_iqn": "iqn.2025-01.com.example:target"},
        })

        test_server = _make_test_server()
        request = connector_pb2.ConnectVolumeRequest(
            connection_info=connection_info
        )
        method = test_server.invoke_unary_unary(
            connector_pb2.DESCRIPTOR.services_by_name[
                "OsBrickConnector"
            ].methods_by_name["ConnectVolume"],
            (),
            request,
            None,
        )
        response, _, code, _ = method.termination()

        self.assertEqual(code, StatusCode.OK)
        self.assertEqual(response.device_path, "/dev/sdb")
        self.assertEqual(response.multipath_device, "/dev/dm-3")
        mock_factory.assert_called_once_with(
            "iscsi", "sudo", use_multipath=False
        )


class TestDisconnectVolume(unittest.TestCase):
    @mock.patch("osbrick.server.brick_connector.InitiatorConnector.factory")
    def test_disconnect_volume(self, mock_factory):
        mock_connector = mock.MagicMock()
        mock_factory.return_value = mock_connector

        connection_info = json.dumps({
            "driver_volume_type": "iscsi",
            "data": {"target_iqn": "iqn.2025-01.com.example:target"},
        })

        test_server = _make_test_server()
        request = connector_pb2.DisconnectVolumeRequest(
            connection_info=connection_info
        )
        method = test_server.invoke_unary_unary(
            connector_pb2.DESCRIPTOR.services_by_name[
                "OsBrickConnector"
            ].methods_by_name["DisconnectVolume"],
            (),
            request,
            None,
        )
        response, _, code, _ = method.termination()

        self.assertEqual(code, StatusCode.OK)
        mock_connector.disconnect_volume.assert_called_once()


class TestExtendVolume(unittest.TestCase):
    @mock.patch("osbrick.server.brick_connector.InitiatorConnector.factory")
    def test_extend_volume(self, mock_factory):
        mock_connector = mock.MagicMock()
        mock_factory.return_value = mock_connector

        connection_info = json.dumps({
            "driver_volume_type": "iscsi",
            "data": {"target_iqn": "iqn.2025-01.com.example:target"},
        })

        test_server = _make_test_server()
        request = connector_pb2.ExtendVolumeRequest(
            connection_info=connection_info
        )
        method = test_server.invoke_unary_unary(
            connector_pb2.DESCRIPTOR.services_by_name[
                "OsBrickConnector"
            ].methods_by_name["ExtendVolume"],
            (),
            request,
            None,
        )
        response, _, code, _ = method.termination()

        self.assertEqual(code, StatusCode.OK)
        mock_connector.extend_volume.assert_called_once()


if __name__ == "__main__":
    unittest.main()
