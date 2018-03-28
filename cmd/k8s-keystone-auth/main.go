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
	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"net/http"
	"os"

	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	kflag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/cloud-provider-openstack/pkg/identity/keystone"
	"k8s.io/cloud-provider-openstack/pkg/identity/webhook"
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
	version        string
)

func main() {
	flag.CommandLine.Parse([]string{})
	pflag.StringVar(&listenAddr, "listen", "0.0.0.0:8443", "<address>:<port> to listen on")
	pflag.StringVar(&tlsCertFile, "tls-cert-file", "", "File containing the default x509 Certificate for HTTPS.")
	pflag.StringVar(&tlsPrivateKey, "tls-private-key-file", "", "File containing the default x509 private key matching --tls-cert-file.")
	pflag.StringVar(&keystoneURL, "keystone-url", os.Getenv("OS_AUTH_URL"), "URL for the OpenStack Keystone API")
	pflag.StringVar(&keystoneCaFile, "keystone-ca-file", "", "File containing the certificate authority for Keystone Service.")
	pflag.StringVar(&policyFile, "keystone-policy-file", "", "File containing the policy.")

	kflag.InitFlags()
	logs.InitLogs()
	defer logs.FlushLogs()

	glog.V(1).Infof("k8s-keystone-auth version: %s", version)

	if keystoneURL == "" {
		glog.Fatal("please specify --keystone-url or set the OS_AUTH_URL environment variable.")
	}
	if tlsCertFile == "" || tlsPrivateKey == "" {
		glog.Fatal("Please specify --tls-cert-file and --tls-private-key-file arguments.")
	}
	if policyFile == "" {
		glog.Infof("Argument --keystone-policy-file missing. Only keystone authentication will work. Use RBAC for authorization.")
	}

	authentication_handler, err := keystone.NewKeystoneAuthenticator(keystoneURL, keystoneCaFile)
	if err != nil {
		glog.Fatal(err.Error())
	}

	authorization_handler, err := keystone.NewKeystoneAuthorizer(keystoneURL, keystoneCaFile, policyFile)
	if err != nil {
		glog.Fatal(err.Error())
	}

	http.Handle("/webhook", webhookServer(authentication_handler, authorization_handler))
	glog.Infof("Starting webhook...")
	glog.Fatal(
		http.ListenAndServeTLS(listenAddr,
			tlsCertFile,
			tlsPrivateKey,
			nil))
}
