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
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	"google.golang.org/grpc"
	"time"
)

var (
	_ Node     = &NodeSvcClient{}
	_ Identity = &IdentitySvcClient{}
)

type NodeSvcClient struct {
	cl csi.NodeClient
}

func (c *NodeSvcClient) GetCapabilities(ctx context.Context) (*csi.NodeGetCapabilitiesResponse, error) {
	return c.cl.NodeGetCapabilities(ctx, &csi.NodeGetCapabilitiesRequest{})
}

func (c *NodeSvcClient) StageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return c.cl.NodeStageVolume(ctx, req)
}

func (c *NodeSvcClient) UnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return c.cl.NodeUnstageVolume(ctx, req)
}

func (c *NodeSvcClient) PublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	return c.cl.NodePublishVolume(ctx, req)
}

func (c *NodeSvcClient) UnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	return c.cl.NodeUnpublishVolume(ctx, req)
}

type IdentitySvcClient struct {
	cl csi.IdentityClient
}

func (c IdentitySvcClient) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return c.cl.Probe(ctx, req)
}

func (c IdentitySvcClient) ProbeForever(conn *grpc.ClientConn, singleProbeTimeout time.Duration) error {
	return connection.ProbeForever(conn, singleProbeTimeout)
}

func (c IdentitySvcClient) GetPluginInfo(ctx context.Context) (*csi.GetPluginInfoResponse, error) {
	return c.cl.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
}
