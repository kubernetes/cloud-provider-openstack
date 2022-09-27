/*
Copyright 2019 The Kubernetes Authors.

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

package sanity

import (
	"context"
	"os"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/csiclient"
)

var (
	_ csiclient.Builder  = &fakeCSIClientBuilder{}
	_ csiclient.Identity = &fakeIdentitySvcClient{}
	_ csiclient.Node     = &fakeNodeSvcClient{}
)

type fakeIdentitySvcClient struct{}

func (c fakeIdentitySvcClient) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

func (c fakeIdentitySvcClient) GetPluginInfo(context.Context) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          "fake-fwd-driver",
		VendorVersion: "1.0.0",
	}, nil
}

func (c fakeIdentitySvcClient) ProbeForever(*grpc.ClientConn, time.Duration) error { return nil }

type fakeNodeSvcClient struct{}

func (c fakeNodeSvcClient) GetCapabilities(context.Context) (*csi.NodeGetCapabilitiesResponse, error) {
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

func (c fakeNodeSvcClient) GetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (c fakeNodeSvcClient) StageVolume(context.Context, *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return &csi.NodeStageVolumeResponse{}, nil
}

func (c fakeNodeSvcClient) UnstageVolume(context.Context, *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (c fakeNodeSvcClient) PublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	// sanity-csi test checks for existence of target_path directory.
	if err := os.MkdirAll(req.GetTargetPath(), 0700); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (c fakeNodeSvcClient) UnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	// sanity-csi test checks that target_path directory no longer exists.
	if err := os.RemoveAll(req.GetTargetPath()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

type fakeCSIClientBuilder struct{}

func (b fakeCSIClientBuilder) NewConnection(string) (*grpc.ClientConn, error) {
	return grpc.Dial("", grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func (b fakeCSIClientBuilder) NewConnectionWithContext(context.Context, string) (*grpc.ClientConn, error) {
	return grpc.Dial("", grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func (b fakeCSIClientBuilder) NewNodeServiceClient(conn *grpc.ClientConn) csiclient.Node {
	return &fakeNodeSvcClient{}
}

func (b fakeCSIClientBuilder) NewIdentityServiceClient(conn *grpc.ClientConn) csiclient.Identity {
	return &fakeIdentitySvcClient{}
}
