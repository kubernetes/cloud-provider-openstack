package server

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/net/context"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	gcfg "gopkg.in/gcfg.v1"
	pb "k8s.io/apiserver/pkg/storage/value/encrypt/envelope/v1beta1"
	"k8s.io/cloud-provider-openstack/pkg/kms/barbican"
	"k8s.io/cloud-provider-openstack/pkg/kms/encryption/aescbc"
	"k8s.io/klog/v2"
)

const (
	netProtocol    = "unix"
	version        = "v1beta1"
	runtimename    = "barbican"
	runtimeversion = "0.0.1"
)

// KMSserver struct
type KMSserver struct {
	cfg      barbican.Config
	barbican barbican.BarbicanService
}

func initConfig(configFilePath string, cfg *barbican.Config) error {
	config, err := os.Open(configFilePath)
	defer config.Close()
	if err != nil {
		return err
	}
	err = gcfg.FatalOnly(gcfg.ReadInto(cfg, config))
	if err != nil {
		return err
	}
	return nil
}

// Run Grpc server for barbican KMS
func Run(configFilePath string, socketpath string, sigchan <-chan os.Signal) (err error) {
	klog.Infof("Barbican KMS Plugin Starting Version: %s, RunTimeVersion: %s", version, runtimeversion)
	s := new(KMSserver)
	err = initConfig(configFilePath, &s.cfg)
	if err != nil {
		klog.V(4).Infof("Error in Getting Config File: %v", err)
		return err
	}

	client, err := barbican.NewBarbicanClient(s.cfg)
	if err != nil {
		klog.V(4).Infof("Failed to get Barbican client: %v", err)
		return err
	}
	s.barbican = &barbican.Barbican{Client: client}

	// unlink the unix socket
	if err = unix.Unlink(socketpath); err != nil {
		klog.V(4).Infof("Error to unlink unix socket: %v", err)
	}

	listener, err := net.Listen(netProtocol, socketpath)
	if err != nil {
		klog.Fatalf("Failed to Listen: %v", err)
		return err
	}

	gServer := grpc.NewServer()
	pb.RegisterKeyManagementServiceServer(gServer, s)

	go gServer.Serve(listener)

	for {
		sig := <-sigchan
		if sig == unix.SIGINT || sig == unix.SIGTERM {
			fmt.Println("force stop, shutting down grpc server")
			gServer.GracefulStop()
			return nil
		}
	}
}

// Version returns KMS service version
func (s *KMSserver) Version(ctx context.Context, req *pb.VersionRequest) (*pb.VersionResponse, error) {
	klog.V(4).Infof("Version Information Requested by Kubernetes api server")

	res := &pb.VersionResponse{
		Version:        version,
		RuntimeName:    runtimename,
		RuntimeVersion: runtimeversion,
	}

	return res, nil
}

// Decrypt decrypts the cipher
func (s *KMSserver) Decrypt(ctx context.Context, req *pb.DecryptRequest) (*pb.DecryptResponse, error) {
	klog.V(4).Infof("Decrypt Request by Kubernetes api server")

	key, err := s.barbican.GetSecret(s.cfg.KeyManager.KeyID)
	if err != nil {
		klog.V(4).Infof("Failed to get key %v: ", err)
		return nil, err
	}

	plain, err := aescbc.Decrypt(req.Cipher, key)
	if err != nil {
		klog.V(4).Infof("Failed to decrypt data %v: ", err)
		return nil, err
	}

	return &pb.DecryptResponse{Plain: plain}, nil
}

// Encrypt encrypts DEK
func (s *KMSserver) Encrypt(ctx context.Context, req *pb.EncryptRequest) (*pb.EncryptResponse, error) {
	klog.V(4).Infof("Encrypt Request by Kubernetes api server")

	key, err := s.barbican.GetSecret(s.cfg.KeyManager.KeyID)

	if err != nil {
		klog.V(4).Infof("Failed to get key %v: ", err)
		return nil, err
	}

	cipher, err := aescbc.Encrypt(req.Plain, key)

	if err != nil {
		klog.V(4).Infof("Failed to encrypt data %v: ", err)
		return nil, err
	}
	return &pb.EncryptResponse{Cipher: cipher}, nil
}
