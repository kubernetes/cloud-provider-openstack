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

package csiclient

import (
	"context"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"time"
)

type Node interface {
	GetCapabilities(ctx context.Context) (*csi.NodeGetCapabilitiesResponse, error)

	StageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error)
	UnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error)

	PublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error)
	UnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error)
}

type Identity interface {
	GetPluginInfo(ctx context.Context) (*csi.GetPluginInfoResponse, error)
	Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error)
	ProbeForever(conn *grpc.ClientConn, singleProbeTimeout time.Duration) error
}

type Builder interface {
	NewConnection(endpoint string) (*grpc.ClientConn, error)
	NewConnectionWithContext(ctx context.Context, endpoint string) (*grpc.ClientConn, error)

	NewNodeServiceClient(conn *grpc.ClientConn) Node
	NewIdentityServiceClient(conn *grpc.ClientConn) Identity
}
