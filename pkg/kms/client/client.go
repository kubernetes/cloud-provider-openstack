package main

import (
	"log"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "k8s.io/kms/apis/v2"
)

//This client is for test purpose only, Kubernetes api server will call to kms plugin grpc server

func main() {
	connection, err := grpc.Dial("unix:///var/lib/kms/kms.sock", grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer func() { _ = connection.Close() }()
	if err != nil {
		log.Fatalf("Connection to KMS plugin failed, error: %v", err)
	}

	kmsClient := pb.NewKeyManagementServiceClient(connection)
	request := &pb.StatusRequest{}
	status, err := kmsClient.Status(context.TODO(), request)
	if err != nil {
		log.Fatalf("Error in getting version from KMS Plugin: %v", err)
	}

	if status.Version != "v2" {
		log.Fatalf("Unsupported KMS Plugin version: %s", status.Version)
	}

	log.Printf("KMS plugin version: %s", status.Version)

	secretBytes := []byte("mypassword")

	//Encryption Request to KMS Plugin
	encRequest := &pb.EncryptRequest{
		Plaintext: secretBytes,
	}
	encResponse, err := kmsClient.Encrypt(context.TODO(), encRequest)
	if err != nil {
		log.Fatalf("Encrypt Request Failed: %v", err)
	}

	cipher := string(encResponse.Ciphertext)
	log.Printf("cipher: %s", cipher)

	//Decryption Request to KMS plugin
	decRequest := &pb.DecryptRequest{
		Ciphertext: encResponse.Ciphertext,
	}

	decResponse, err := kmsClient.Decrypt(context.TODO(), decRequest)
	if err != nil {
		log.Fatalf("Unable to decrypt response: %v", err)
	}

	log.Printf("Decryption response: %v", decResponse)
}
