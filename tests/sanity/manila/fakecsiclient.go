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
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
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

func (c fakeNodeSvcClient) StageVolume(context.Context, *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return &csi.NodeStageVolumeResponse{}, nil
}

func (c fakeNodeSvcClient) UnstageVolume(context.Context, *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (c fakeNodeSvcClient) PublishVolume(context.Context, *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	return &csi.NodePublishVolumeResponse{}, nil
}

func (c fakeNodeSvcClient) UnpublishVolume(context.Context, *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

type fakeCSIClientBuilder struct{}

func (b fakeCSIClientBuilder) NewConnection(string) (*grpc.ClientConn, error) { return nil, nil }

func (b fakeCSIClientBuilder) NewConnectionWithContext(context.Context, string) (*grpc.ClientConn, error) {
	return nil, nil
}

func (b fakeCSIClientBuilder) NewNodeServiceClient(conn *grpc.ClientConn) csiclient.Node {
	return &fakeNodeSvcClient{}
}

func (b fakeCSIClientBuilder) NewIdentityServiceClient(conn *grpc.ClientConn) csiclient.Identity {
	return &fakeIdentitySvcClient{}
}
