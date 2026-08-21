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

package manila

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/csiclient"
)

type unavailableIdentityClient struct {
	unavailableCount int32
	calls            int32
}

func (c *unavailableIdentityClient) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

func (c *unavailableIdentityClient) GetPluginInfo(context.Context) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          "fake-driver",
		VendorVersion: "1.0.0",
	}, nil
}

func (c *unavailableIdentityClient) ProbeForever(context.Context, *grpc.ClientConn, time.Duration) error {
	n := atomic.AddInt32(&c.calls, 1)
	if n <= c.unavailableCount {
		return status.Error(codes.Unavailable, "not ready yet")
	}
	return nil
}

type stubNodeClient struct{}

func (c *stubNodeClient) GetCapabilities(context.Context) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (c *stubNodeClient) GetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (c *stubNodeClient) StageVolume(context.Context, *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return &csi.NodeStageVolumeResponse{}, nil
}

func (c *stubNodeClient) UnstageVolume(context.Context, *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (c *stubNodeClient) PublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	return &csi.NodePublishVolumeResponse{}, nil
}

func (c *stubNodeClient) UnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

type testCSIClientBuilder struct {
	identityClient csiclient.Identity
	nodeClient     csiclient.Node
}

func (b *testCSIClientBuilder) NewConnection(string) (*grpc.ClientConn, error) {
	return grpc.NewClient("localhost", grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func (b *testCSIClientBuilder) NewConnectionWithContext(context.Context, string) (*grpc.ClientConn, error) {
	return grpc.NewClient("localhost", grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func (b *testCSIClientBuilder) NewNodeServiceClient(conn *grpc.ClientConn) csiclient.Node {
	return b.nodeClient
}

func (b *testCSIClientBuilder) NewIdentityServiceClient(conn *grpc.ClientConn) csiclient.Identity {
	return b.identityClient
}

func TestInitProxiedDriverRetryOnUnavailable(t *testing.T) {
	idClient := &unavailableIdentityClient{unavailableCount: 3}
	builder := &testCSIClientBuilder{
		identityClient: idClient,
		nodeClient:     &stubNodeClient{},
	}

	d := &Driver{
		fwdEndpoint:      "unix:///tmp/fake.sock",
		csiClientBuilder: builder,
	}

	caps, err := d.initProxiedDriver()
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	if !caps[csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME] {
		t.Error("expected STAGE_UNSTAGE_VOLUME capability")
	}
	if atomic.LoadInt32(&idClient.calls) != 4 {
		t.Errorf("expected 4 ProbeForever calls (3 Unavailable + 1 success), got %d", idClient.calls)
	}
}
