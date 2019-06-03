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

package manila

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

var (
	fwdEndpointGRPCCallCounter uint64
)

func grpcConnect(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
	var (
		dialOptions = []grpc.DialOption{
			grpc.WithInsecure(),
			grpc.WithBackoffMaxDelay(time.Second),
			grpc.WithBlock(),
			grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				callID := atomic.AddUint64(&fwdEndpointGRPCCallCounter, 1)

				klog.V(3).Infof("[ID:%d] FWD GRPC call: %s", callID, method)
				klog.V(5).Infof("[ID:%d] FWD GRPC request: %s", callID, protosanitizer.StripSecrets(req))

				err := invoker(ctx, method, req, reply, cc, opts...)
				if err != nil {
					klog.Infof("[ID:%d] FWD GRPC error: %v", callID, err)
				} else {
					klog.V(5).Infof("[ID:%d] FWD GRPC response: %s", callID, protosanitizer.StripSecrets(reply))
				}

				return err
			}),
		}

		conn *grpc.ClientConn
		err  error
	)

	if ctx != nil {
		conn, err = grpc.DialContext(ctx, endpoint, dialOptions...)
		if err == context.DeadlineExceeded {
			return nil, fmt.Errorf("connecting to %s timed out: %v", endpoint, err)
		}

		return conn, err
	} else {
		dialFinished := make(chan bool)
		go func() {
			conn, err = grpc.Dial(endpoint, dialOptions...)
			close(dialFinished)
		}()

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				klog.Warningf("still connecting to %s", endpoint)
			case <-dialFinished:
				return conn, err
			}
		}
	}
}

type csiNodeCapabilitySet map[csi.NodeServiceCapability_RPC_Type]bool

func csiNodeGetCapabilities(ctx context.Context, conn *grpc.ClientConn) (csiNodeCapabilitySet, error) {
	client := csi.NewNodeClient(conn)
	req := csi.NodeGetCapabilitiesRequest{}
	rsp, err := client.NodeGetCapabilities(ctx, &req)
	if err != nil {
		return nil, err
	}

	caps := csiNodeCapabilitySet{}
	for _, cap := range rsp.GetCapabilities() {
		if cap == nil {
			continue
		}
		rpc := cap.GetRpc()
		if rpc == nil {
			continue
		}
		t := rpc.GetType()
		caps[t] = true
	}
	return caps, nil
}

func csiNodeGetInfo(ctx context.Context, conn *grpc.ClientConn) (*csi.NodeGetInfoResponse, error) {
	client := csi.NewNodeClient(conn)
	return client.NodeGetInfo(ctx, &csi.NodeGetInfoRequest{})
}

func csiNodePublishVolume(ctx context.Context, conn *grpc.ClientConn, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	client := csi.NewNodeClient(conn)
	return client.NodePublishVolume(ctx, req)
}

func csiNodeUnpublishVolume(ctx context.Context, conn *grpc.ClientConn, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	client := csi.NewNodeClient(conn)
	return client.NodeUnpublishVolume(ctx, req)
}

func csiNodeStageVolume(ctx context.Context, conn *grpc.ClientConn, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	client := csi.NewNodeClient(conn)
	return client.NodeStageVolume(ctx, req)
}

func csiNodeUnstageVolume(ctx context.Context, conn *grpc.ClientConn, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	client := csi.NewNodeClient(conn)
	return client.NodeUnstageVolume(ctx, req)
}

func csiGetPluginInfo(ctx context.Context, conn *grpc.ClientConn) (*csi.GetPluginInfoResponse, error) {
	client := csi.NewIdentityClient(conn)
	return client.GetPluginInfo(ctx, &csi.GetPluginInfoRequest{})
}
