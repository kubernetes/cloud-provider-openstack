package server

import (
	"bytes"
	"testing"

	"golang.org/x/net/context"
	pb "k8s.io/apiserver/pkg/storage/value/encrypt/envelope/v1beta1"
	"k8s.io/cloud-provider-openstack/pkg/kms/barbican"
)

var s = new(KMSserver)

func TestInitConfig(t *testing.T) {
}

func TestVersion(t *testing.T) {
	req := &pb.VersionRequest{Version: "v1beta1"}
	_, err := s.Version(context.TODO(), req)
	if err != nil {
		t.FailNow()
	}
}

func TestEncryptDecrypt(t *testing.T) {
	s.barbican = &barbican.FakeBarbican{}
	fakeData := []byte("fakedata")
	encreq := &pb.EncryptRequest{Version: "v1beta1", Plain: fakeData}
	encresp, err := s.Encrypt(context.TODO(), encreq)
	if err != nil {
		t.Log(err)
		t.FailNow()
	}
	decreq := &pb.DecryptRequest{Version: "v1beta1", Cipher: encresp.Cipher}
	decresp, err := s.Decrypt(context.TODO(), decreq)
	if err != nil || !bytes.Equal(decresp.Plain, fakeData) {
		t.Log(err)
		t.FailNow()
	}
}
