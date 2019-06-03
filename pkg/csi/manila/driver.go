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
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

type Driver struct {
	nodeID     string
	name       string
	version    string
	shareProto string

	serverEndpoint string
	fwdEndpoint    string

	ids *identityServer
	cs  *controllerServer
	ns  *nodeServer

	vcaps  []*csi.VolumeCapability_AccessMode
	cscaps []*csi.ControllerServiceCapability
	nscaps []*csi.NodeServiceCapability
}

type nonBlockingGRPCServer struct {
	wg     sync.WaitGroup
	server *grpc.Server
}

const (
	driverVersion = "0.9.0"
)

var (
	serverGRPCEndpointCallCounter uint64
)

func NewDriver(nodeID, driverName, endpoint, fwdEndpoint, shareProto string) (*Driver, error) {
	klog.Infof("Driver: %v version: %v CSI spec version: 1.1.0", driverName, driverVersion)

	d := &Driver{
		nodeID:         nodeID,
		name:           driverName,
		serverEndpoint: endpoint,
		fwdEndpoint:    fwdEndpoint,
		shareProto:     shareProto,
	}

	serverProto, serverAddr, err := parseGRPCEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server endpoint address %s: %v", endpoint, err)
	}

	fwdProto, fwdAddr, err := parseGRPCEndpoint(fwdEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proxy client address %s: %v", fwdEndpoint, err)
	}

	d.serverEndpoint = endpointAddress(serverProto, serverAddr)
	d.fwdEndpoint = endpointAddress(fwdProto, fwdAddr)

	d.addControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
	})

	d.addVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
	})

	var supportsNodeStage bool

	if nodeCapsMap, err := d.initProxiedDriver(); err != nil {
		return nil, fmt.Errorf("failed to initialize proxied CSI driver: %v", err)
	} else {
		var nscaps []csi.NodeServiceCapability_RPC_Type
		for c := range nodeCapsMap {
			nscaps = append(nscaps, c)

			if c == csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME {
				supportsNodeStage = true
			}
		}

		d.addNodeServiceCapabilities(nscaps)
	}

	d.ids = &identityServer{d: d}
	d.cs = &controllerServer{d: d}
	d.ns = &nodeServer{d: d, supportsNodeStage: supportsNodeStage, nodeStageCache: make(map[volumeID]stageCacheEntry)}

	return d, nil
}

func (d *Driver) Run() {
	s := nonBlockingGRPCServer{}
	s.start(d.serverEndpoint, d.ids, d.cs, d.ns)
	s.wait()
}

func (d *Driver) addControllerServiceCapabilities(cs []csi.ControllerServiceCapability_RPC_Type) {
	var caps []*csi.ControllerServiceCapability

	for _, c := range cs {
		klog.Infof("Enabling controller service capability: %v", c.String())
		csc := &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: c,
				},
			},
		}

		caps = append(caps, csc)
	}

	d.cscaps = caps
}

func (d *Driver) addVolumeCapabilityAccessModes(vs []csi.VolumeCapability_AccessMode_Mode) {
	var caps []*csi.VolumeCapability_AccessMode

	for _, c := range vs {
		klog.Infof("Enabling volume access mode: %v", c.String())
		caps = append(caps, &csi.VolumeCapability_AccessMode{Mode: c})
	}

	d.vcaps = caps
}

func (d *Driver) addNodeServiceCapabilities(ns []csi.NodeServiceCapability_RPC_Type) {
	var caps []*csi.NodeServiceCapability

	for _, c := range ns {
		klog.Infof("Enabling node service capability: %v", c.String())
		nsc := &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: c,
				},
			},
		}

		caps = append(caps, nsc)
	}

	d.nscaps = caps
}

func (d *Driver) initProxiedDriver() (csiNodeCapabilitySet, error) {
	conn, err := grpcConnect(nil, d.fwdEndpoint)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s endpoint failed: %v", d.fwdEndpoint, err)
	}

	if err = connection.ProbeForever(conn, time.Second*5); err != nil {
		return nil, fmt.Errorf("probe failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	pluginInfo, err := csiGetPluginInfo(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin info of the proxied driver: %v", err)
	}

	klog.Infof("proxying CSI driver %s version %s", pluginInfo.GetName(), pluginInfo.GetVendorVersion())

	nodeCaps, err := csiNodeGetCapabilities(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to get node capabilities: %v", err)
	}

	return nodeCaps, nil
}

func (s *nonBlockingGRPCServer) start(endpoint string, ids *identityServer, cs *controllerServer, ns *nodeServer) {
	s.wg.Add(1)
	go s.serve(endpoint, ids, cs, ns)
}

func (s *nonBlockingGRPCServer) wait() {
	s.wg.Wait()
}

func (s *nonBlockingGRPCServer) stop() {
	s.server.GracefulStop()
}

func (s *nonBlockingGRPCServer) forceStop() {
	s.server.Stop()
}

func (s *nonBlockingGRPCServer) serve(endpoint string, ids *identityServer, cs *controllerServer, ns *nodeServer) {
	proto, addr, err := parseGRPCEndpoint(endpoint)
	if err != nil {
		klog.Fatalf("couldn't parse GRPC server endpoint address %s: %v", endpoint, err)
	}

	if proto == "unix" {
		if err = os.Remove(addr); err != nil && !os.IsNotExist(err) {
			klog.Fatalf("failed to remove an existing socket file %s: %v", addr, err)
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		klog.Fatalf("listen failed for GRPC server: %v", err)
	}

	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		callID := atomic.AddUint64(&serverGRPCEndpointCallCounter, 1)

		klog.V(3).Infof("[ID:%d] GRPC call: %s", callID, info.FullMethod)
		klog.V(5).Infof("[ID:%d] GRPC request: %s", callID, protosanitizer.StripSecrets(req))
		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("[ID:%d] GRPC error: %v", callID, err)
		} else {
			klog.V(5).Infof("[ID:%d] GRPC response: %s", callID, protosanitizer.StripSecrets(resp))
		}
		return resp, err
	}))

	s.server = server

	csi.RegisterIdentityServer(server, ids)
	csi.RegisterControllerServer(server, cs)
	csi.RegisterNodeServer(server, ns)

	klog.Infof("listening for connections on %#v", listener.Addr())

	if err := server.Serve(listener); err != nil {
		klog.Fatalf("GRPC server failure: %v", err)
	}
}
