/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package brick

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "k8s.io/cloud-provider-openstack/pkg/util/brick/gen/osbrickv1"
)

const bufSize = 1024 * 1024

// fakeServer implements the generated OsBrickConnectorServer interface
// for in-process testing.
type fakeServer struct {
	pb.UnimplementedOsBrickConnectorServer

	// Configurable return values
	connProps          *pb.ConnectorProperties
	connectResp        *pb.ConnectVolumeResponse
	disconnectErr      error
	extendErr          error
	lastConnectionInfo string
}

func (s *fakeServer) GetConnectorProperties(_ context.Context, _ *pb.GetConnectorPropertiesRequest) (*pb.ConnectorProperties, error) {
	return s.connProps, nil
}

func (s *fakeServer) ConnectVolume(_ context.Context, req *pb.ConnectVolumeRequest) (*pb.ConnectVolumeResponse, error) {
	s.lastConnectionInfo = req.GetConnectionInfo()
	return s.connectResp, nil
}

func (s *fakeServer) DisconnectVolume(_ context.Context, req *pb.DisconnectVolumeRequest) (*pb.DisconnectVolumeResponse, error) {
	s.lastConnectionInfo = req.GetConnectionInfo()
	return &pb.DisconnectVolumeResponse{}, s.disconnectErr
}

func (s *fakeServer) ExtendVolume(_ context.Context, req *pb.ExtendVolumeRequest) (*pb.ExtendVolumeResponse, error) {
	s.lastConnectionInfo = req.GetConnectionInfo()
	return &pb.ExtendVolumeResponse{}, s.extendErr
}

// newTestClient creates a GRPCConnector backed by an in-process gRPC
// server via bufconn. Returns the connector, the fake server (for
// configuring responses), and a cleanup function.
func newTestClient(t *testing.T, srv *fakeServer) *GRPCConnector {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	grpcServer := grpc.NewServer()
	pb.RegisterOsBrickConnectorServer(grpcServer, srv)

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufconn: %v", err)
	}

	t.Cleanup(func() {
		conn.Close()
		grpcServer.Stop()
	})

	return &GRPCConnector{
		conn:   conn,
		client: pb.NewOsBrickConnectorClient(conn),
	}
}

func TestGetConnectorProperties(t *testing.T) {
	rawJSON := `{"initiator":"iqn.2025-01.com.example:node1","host":"node1.example.com","multipath":true,"sdc_guid":"ABC123"}`
	srv := &fakeServer{
		connProps: &pb.ConnectorProperties{
			Initiator: "iqn.2025-01.com.example:node1",
			Wwpns:     []string{"50060b0000c26040", "50060b0000c26041"},
			Host:      "node1.example.com",
			Multipath: true,
			Extras:    map[string]string{"sdc_guid": "ABC123"},
			RawJson:   rawJSON,
		},
	}
	client := newTestClient(t, srv)
	ctx := context.Background()

	props, err := client.GetConnectorProperties(ctx)
	if err != nil {
		t.Fatalf("GetConnectorProperties failed: %v", err)
	}

	if props.Initiator != "iqn.2025-01.com.example:node1" {
		t.Errorf("Initiator = %q, want %q", props.Initiator, "iqn.2025-01.com.example:node1")
	}
	if len(props.Wwpns) != 2 || props.Wwpns[0] != "50060b0000c26040" {
		t.Errorf("Wwpns = %v, want [50060b0000c26040 50060b0000c26041]", props.Wwpns)
	}
	if props.Host != "node1.example.com" {
		t.Errorf("Host = %q, want %q", props.Host, "node1.example.com")
	}
	if !props.Multipath {
		t.Error("Multipath = false, want true")
	}
	if props.Extras["sdc_guid"] != "ABC123" {
		t.Errorf("Extras[sdc_guid] = %q, want %q", props.Extras["sdc_guid"], "ABC123")
	}
	if props.RawJSON != rawJSON {
		t.Errorf("RawJSON = %q, want %q", props.RawJSON, rawJSON)
	}
}

func TestConnectVolume(t *testing.T) {
	srv := &fakeServer{
		connectResp: &pb.ConnectVolumeResponse{
			DevicePath: "/dev/sdb",
		},
	}
	client := newTestClient(t, srv)
	ctx := context.Background()

	devicePath, err := client.ConnectVolume(ctx, `{"driver_volume_type":"iscsi"}`)
	if err != nil {
		t.Fatalf("ConnectVolume failed: %v", err)
	}
	if devicePath != "/dev/sdb" {
		t.Errorf("devicePath = %q, want %q", devicePath, "/dev/sdb")
	}
	if srv.lastConnectionInfo != `{"driver_volume_type":"iscsi"}` {
		t.Errorf("server received connectionInfo = %q, want %q", srv.lastConnectionInfo, `{"driver_volume_type":"iscsi"}`)
	}
}

func TestConnectVolumeMultipath(t *testing.T) {
	srv := &fakeServer{
		connectResp: &pb.ConnectVolumeResponse{
			DevicePath:      "/dev/sdb",
			MultipathDevice: "/dev/dm-3",
		},
	}
	client := newTestClient(t, srv)
	ctx := context.Background()

	devicePath, err := client.ConnectVolume(ctx, `{"driver_volume_type":"iscsi"}`)
	if err != nil {
		t.Fatalf("ConnectVolume failed: %v", err)
	}
	// Should prefer multipath device when available.
	if devicePath != "/dev/dm-3" {
		t.Errorf("devicePath = %q, want multipath device %q", devicePath, "/dev/dm-3")
	}
}

func TestDisconnectVolume(t *testing.T) {
	srv := &fakeServer{}
	client := newTestClient(t, srv)
	ctx := context.Background()

	err := client.DisconnectVolume(ctx, `{"driver_volume_type":"iscsi"}`)
	if err != nil {
		t.Fatalf("DisconnectVolume failed: %v", err)
	}
	if srv.lastConnectionInfo != `{"driver_volume_type":"iscsi"}` {
		t.Errorf("server received connectionInfo = %q, want %q", srv.lastConnectionInfo, `{"driver_volume_type":"iscsi"}`)
	}
}

func TestExtendVolume(t *testing.T) {
	srv := &fakeServer{}
	client := newTestClient(t, srv)
	ctx := context.Background()

	err := client.ExtendVolume(ctx, `{"driver_volume_type":"iscsi"}`)
	if err != nil {
		t.Fatalf("ExtendVolume failed: %v", err)
	}
	if srv.lastConnectionInfo != `{"driver_volume_type":"iscsi"}` {
		t.Errorf("server received connectionInfo = %q, want %q", srv.lastConnectionInfo, `{"driver_volume_type":"iscsi"}`)
	}
}
