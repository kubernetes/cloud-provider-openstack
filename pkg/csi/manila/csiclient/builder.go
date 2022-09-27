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
	"sync/atomic"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"
)

var (
	grpcCallCounter uint64

	dialOptions = []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1.0 * time.Second,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   time.Second,
			},
		}),
		grpc.WithBlock(),
		// CSI connections use unix:// so should ignore proxy settings
		// WithNoProxy can be removed when we update to gRPC >= v1.34,
		// which contains https://github.com/grpc/grpc-go/pull/3890.
		// This automatically ignores proxy settings for unix
		// connections.
		grpc.WithNoProxy(),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			callID := atomic.AddUint64(&grpcCallCounter, 1)

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

	_ Builder = &ClientBuilder{}
)

func NewNodeSvcClient(conn *grpc.ClientConn) *NodeSvcClient {
	return &NodeSvcClient{cl: csi.NewNodeClient(conn)}
}

func NewIdentitySvcClient(conn *grpc.ClientConn) *IdentitySvcClient {
	return &IdentitySvcClient{cl: csi.NewIdentityClient(conn)}
}

func NewConnection(endpoint string) (*grpc.ClientConn, error) {
	var (
		conn *grpc.ClientConn
		err  error
	)

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

func NewConnectionWithContext(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, endpoint, dialOptions...)
}

type ClientBuilder struct{}

func (b ClientBuilder) NewConnection(endpoint string) (*grpc.ClientConn, error) {
	return NewConnection(endpoint)
}

func (b ClientBuilder) NewConnectionWithContext(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
	return NewConnectionWithContext(ctx, endpoint)
}

func (b ClientBuilder) NewNodeServiceClient(conn *grpc.ClientConn) Node {
	return NewNodeSvcClient(conn)
}

func (b ClientBuilder) NewIdentityServiceClient(conn *grpc.ClientConn) Identity {
	return NewIdentitySvcClient(conn)
}
