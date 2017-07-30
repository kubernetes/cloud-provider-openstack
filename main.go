/*
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

package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/dims/k8s-keystone-auth/pkg/authenticator/token/keystone"
	"github.com/dims/k8s-keystone-auth/pkg/identity/webhook"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func webhookServer(authenticator authenticator.Token, authorizer authorizer.Authorizer) http.Handler {
	return &webhook.WebhookHandler{
		Authenticator: authenticator,
		Authorizer:    authorizer,
	}
}

var (
	listenAddr     string
	tlsCertFile    string
	tlsPrivateKey  string
	keystoneURL    string
	keystoneCaFile string
	policyFile     string
)

func main() {
	flag.StringVar(&listenAddr, "listen", "localhost:8443", "<address>:<port> to listen on")
	flag.StringVar(&tlsCertFile, "tls-cert-file", "", "File containing the default x509 Certificate for HTTPS.")
	flag.StringVar(&tlsPrivateKey, "tls-private-key-file", "", "File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&keystoneURL, "keystone-url", "http://localhost/identity/v3/", "URL for the OpenStack Keystone API")
	flag.StringVar(&keystoneCaFile, "keystone-ca-file", "", "File containing the certificate authority for Keystone Service.")
	flag.StringVar(&policyFile, "keystone-policy-file", "", "File containing the policy.")
	flag.Parse()

	if tlsCertFile == "" || tlsPrivateKey == "" {
		log.Fatal("Please specify --tls-cert-file and --tls-private-key-file arguments.")
	}
	if policyFile == "" {
		log.Fatal("Please specify --keystone-policy-file argument.")
	}

	authentication_handler, err := keystone.NewKeystoneAuthenticator(keystoneURL, keystoneCaFile)
	if err != nil {
		log.Fatal(err.Error())
	}

	authorization_handler, err := keystone.NewKeystoneAuthorizer(keystoneURL, keystoneCaFile, policyFile)
	if err != nil {
		log.Fatal(err.Error())
	}

	http.Handle("/webhook", webhookServer(authentication_handler, authorization_handler))
	log.Println("Starting webhook..")
	log.Fatal(
		http.ListenAndServeTLS(":8443",
			tlsCertFile,
			tlsPrivateKey,
			nil))
}
