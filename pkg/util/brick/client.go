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
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "k8s.io/cloud-provider-openstack/pkg/util/brick/gen/osbrickv1"
)

// GRPCConnector implements IConnector by calling the os-brick sidecar
// over gRPC. It is a thin wrapper around the generated gRPC stubs
// that maps between the proto types and the brick Go types.
type GRPCConnector struct {
	conn   *grpc.ClientConn
	client pb.OsBrickConnectorClient
}

var _ IConnector = (*GRPCConnector)(nil)

// NewGRPCConnector creates a new gRPC client connected to the os-brick
// sidecar at the given endpoint (e.g. "unix:///var/run/osbrick/osbrick.sock").
func NewGRPCConnector(endpoint string) (*GRPCConnector, error) {
	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to os-brick sidecar at %s: %w", endpoint, err)
	}

	return &GRPCConnector{
		conn:   conn,
		client: pb.NewOsBrickConnectorClient(conn),
	}, nil
}

// GetConnectorProperties returns the host initiator information from
// the os-brick sidecar.
func (c *GRPCConnector) GetConnectorProperties(ctx context.Context) (*ConnectorProperties, error) {
	resp, err := c.client.GetConnectorProperties(ctx, &pb.GetConnectorPropertiesRequest{})
	if err != nil {
		return nil, fmt.Errorf("GetConnectorProperties RPC failed: %w", err)
	}

	return &ConnectorProperties{
		Initiator: resp.GetInitiator(),
		Wwpns:     resp.GetWwpns(),
		Host:      resp.GetHost(),
		Multipath: resp.GetMultipath(),
		Extras:    resp.GetExtras(),
		RawJSON:   resp.GetRawJson(),
	}, nil
}

// ConnectVolume asks the sidecar to connect a volume and returns the
// resulting device path.
func (c *GRPCConnector) ConnectVolume(ctx context.Context, connectionInfo string) (string, error) {
	resp, err := c.client.ConnectVolume(ctx, &pb.ConnectVolumeRequest{
		ConnectionInfo: connectionInfo,
	})
	if err != nil {
		return "", fmt.Errorf("ConnectVolume RPC failed: %w", err)
	}

	// Prefer the multipath device when available.
	if mp := resp.GetMultipathDevice(); mp != "" {
		return mp, nil
	}
	return resp.GetDevicePath(), nil
}

// DisconnectVolume asks the sidecar to disconnect a previously
// connected volume.
func (c *GRPCConnector) DisconnectVolume(ctx context.Context, connectionInfo string) error {
	_, err := c.client.DisconnectVolume(ctx, &pb.DisconnectVolumeRequest{
		ConnectionInfo: connectionInfo,
	})
	if err != nil {
		return fmt.Errorf("DisconnectVolume RPC failed: %w", err)
	}
	return nil
}

// ExtendVolume asks the sidecar to rescan/extend a connected volume.
func (c *GRPCConnector) ExtendVolume(ctx context.Context, connectionInfo string) error {
	_, err := c.client.ExtendVolume(ctx, &pb.ExtendVolumeRequest{
		ConnectionInfo: connectionInfo,
	})
	if err != nil {
		return fmt.Errorf("ExtendVolume RPC failed: %w", err)
	}
	return nil
}

// Close releases the underlying gRPC connection.
func (c *GRPCConnector) Close() error {
	return c.conn.Close()
}
