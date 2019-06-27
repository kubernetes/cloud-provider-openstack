package cinder

import (
	"fmt"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/mount"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
	"k8s.io/klog"
)

func NewControllerServiceCapability(cap csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
	return &csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: cap,
			},
		},
	}
}

func NewNodeServiceCapability(cap csi.NodeServiceCapability_RPC_Type) *csi.NodeServiceCapability {
	return &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: cap,
			},
		},
	}
}

func NewVolumeCapabilityAccessMode(mode csi.VolumeCapability_AccessMode_Mode) *csi.VolumeCapability_AccessMode {
	return &csi.VolumeCapability_AccessMode{Mode: mode}
}

func NewControllerServer(d *CinderDriver, cloud openstack.IOpenStack) *controllerServer {
	return &controllerServer{
		Driver: d,
		Cloud:  cloud,
	}
}

func NewIdentityServer(d *CinderDriver) *identityServer {
	return &identityServer{
		Driver: d,
	}
}

func NewNodeServer(d *CinderDriver, mount mount.IMount, metadata openstack.IMetadata, cloud openstack.IOpenStack) *nodeServer {
	return &nodeServer{
		Driver:   d,
		Mount:    mount,
		Metadata: metadata,
		Cloud:    cloud,
	}
}

func RunControllerandNodePublishServer(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) {

	s := NewNonBlockingGRPCServer()
	s.Start(endpoint, ids, cs, ns)
	s.Wait()
}

func ParseEndpoint(ep string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(ep), "unix://") || strings.HasPrefix(strings.ToLower(ep), "tcp://") {
		s := strings.SplitN(ep, "://", 2)
		if s[1] != "" {
			return s[0], s[1], nil
		}
	}
	return "", "", fmt.Errorf("Invalid endpoint: %v", ep)
}

func logGRPC(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	klog.V(3).Infof("GRPC call: %s", info.FullMethod)
	klog.V(5).Infof("GRPC request: %+v", req)
	resp, err := handler(ctx, req)
	if err != nil {
		klog.Errorf("GRPC error: %v", err)
	} else {
		klog.V(5).Infof("GRPC response: %+v", resp)
	}
	return resp, err
}
