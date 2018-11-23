package main

import (
	"fmt"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	pb "k8s.io/apiserver/pkg/storage/value/encrypt/envelope/v1beta1"
	"os"
)

//This client is for test purpose only, Kubernetes api server will call to kms plugin grpc server

func main() {

	connection, err := grpc.Dial("unix:///var/lib/kms/kms.sock", grpc.WithInsecure())
	defer connection.Close()
	if err != nil {
		fmt.Printf("\nConnection to KMS plugin failed, error: %v", err)
	}

	kmsClient := pb.NewKeyManagementServiceClient(connection)
	request := &pb.VersionRequest{Version: "v1beta1"}
	_, err = kmsClient.Version(context.TODO(), request)

	if err != nil {
		fmt.Printf("\nError in getting version from KMS Plugin: %v", err)
	}

	secretBytes := []byte("mypassword")

	//Encryption Request to KMS Plugin
	encRequest := &pb.EncryptRequest{
		Version: "v1beta1",
		Plain:   secretBytes}
	encResponse, err := kmsClient.Encrypt(context.TODO(), encRequest)

	if err != nil {
		fmt.Printf("\nEncrypt Request Failed: %v", err)
		os.Exit(1)
	}

	cipher := string(encResponse.Cipher)
	fmt.Println("cipher:", cipher)

	//Decryption Request to KMS plugin
	decRequest := &pb.DecryptRequest{
		Version: "v1beta1",
		Cipher:  encResponse.Cipher,
	}

	decResponse, err := kmsClient.Decrypt(context.TODO(), decRequest)

	fmt.Printf("\n\ndecryption response %v", decResponse)
}
