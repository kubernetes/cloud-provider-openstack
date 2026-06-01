package server

import (
	"context"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	gcfg "gopkg.in/gcfg.v1"
	"k8s.io/cloud-provider-openstack/pkg/kms/barbican"
	"k8s.io/cloud-provider-openstack/pkg/kms/encryption/aescbc"
	"k8s.io/klog/v2"
	pb "k8s.io/kms/apis/v2"
)

const (
	netProtocol    = "unix"
	version        = "v2"
	runtimeversion = "0.0.2"
)

type BarbicanService interface {
	GetSecret(ctx context.Context, keyID string) ([]byte, error)
}

// KMSserver struct
type KMSserver struct {
	pb.UnimplementedKeyManagementServiceServer
	cfg      barbican.Config
	barbican BarbicanService
}

func initConfig(configFilePath string, cfg *barbican.Config) error {
	config, err := os.Open(configFilePath)
	defer func() { _ = config.Close() }()
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

	serverCh := make(chan error, 1)
	go func() {
		err := gServer.Serve(listener)
		serverCh <- err
		close(serverCh)
	}()

	for {
		select {
		case sig := <-sigchan:
			if sig == unix.SIGINT || sig == unix.SIGTERM {
				fmt.Println("force stop, shutting down grpc server")
				gServer.GracefulStop()
				return nil
			}
		case err := <-serverCh:
			if err != nil {
				return fmt.Errorf("failed to listen: %w", err)
			}
		}
	}
}

// Version returns KMS service version
func (s *KMSserver) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	klog.V(4).Infof("Version Information Requested by Kubernetes api server")

	res := &pb.StatusResponse{
		Version: version,
		Healthz: "ok",
		KeyId:   s.cfg.KeyManager.KeyID,
	}

	return res, nil
}

// Decrypt decrypts the cipher
func (s *KMSserver) Decrypt(ctx context.Context, req *pb.DecryptRequest) (*pb.DecryptResponse, error) {
	klog.V(4).Infof("Decrypt Request by Kubernetes api server")

	// TODO: consider using req.KeyId
	key, err := s.barbican.GetSecret(ctx, s.cfg.KeyManager.KeyID)
	if err != nil {
		klog.V(4).Infof("Failed to get key %v: ", err)
		return nil, err
	}

	plain, err := aescbc.Decrypt(req.Ciphertext, key)
	if err != nil {
		klog.V(4).Infof("Failed to decrypt data %v: ", err)
		return nil, err
	}

	return &pb.DecryptResponse{Plaintext: plain}, nil
}

// Encrypt encrypts DEK
func (s *KMSserver) Encrypt(ctx context.Context, req *pb.EncryptRequest) (*pb.EncryptResponse, error) {
	klog.V(4).Infof("Encrypt Request by Kubernetes api server")

	key, err := s.barbican.GetSecret(ctx, s.cfg.KeyManager.KeyID)

	if err != nil {
		klog.V(4).Infof("Failed to get key %v: ", err)
		return nil, err
	}

	cipher, err := aescbc.Encrypt(req.Plaintext, key)

	if err != nil {
		klog.V(4).Infof("Failed to encrypt data %v: ", err)
		return nil, err
	}
	return &pb.EncryptResponse{Ciphertext: cipher, KeyId: s.cfg.KeyManager.KeyID}, nil
}
