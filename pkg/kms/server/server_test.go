package server

import (
	"bytes"
	"testing"

	"golang.org/x/net/context"
	"k8s.io/cloud-provider-openstack/pkg/kms/barbican"
	pb "k8s.io/kms/apis/v2"
)

var s = new(KMSserver)

func TestInitConfig(t *testing.T) {
}

func TestStatus(t *testing.T) {
	req := &pb.StatusRequest{}
	_, err := s.Status(context.TODO(), req)
	if err != nil {
		t.FailNow()
	}
}

func TestEncryptDecrypt(t *testing.T) {
	s.barbican = &barbican.FakeBarbican{}
	fakeData := []byte("fakedata")
	encreq := &pb.EncryptRequest{Plaintext: fakeData}
	encresp, err := s.Encrypt(context.TODO(), encreq)
	if err != nil {
		t.Log(err)
		t.FailNow()
	}
	decreq := &pb.DecryptRequest{Ciphertext: encresp.Ciphertext}
	decresp, err := s.Decrypt(context.TODO(), decreq)
	if err != nil || !bytes.Equal(decresp.Plaintext, fakeData) {
		t.Log(err)
		t.FailNow()
	}
}
