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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"k8s.io/apiserver/pkg/authentication/authenticator"
	"github.com/dims/k8s-keystone-auth/pkg/authenticator/token/keystone"
)

type userInfo struct {
	Username string              `json:"username"`
	UID      string              `json:"uid"`
	Groups   []string            `json:"groups"`
	Extra    map[string][]string `json:"extra"`
}
type status struct {
	Authenticated bool     `json:"authenticated"`
	User          userInfo `json:"user"`
}

type webhookHandler struct {
	authenticator authenticator.Token
}

func webhookServer(authenticator authenticator.Token) http.Handler {
	return &webhookHandler{
		authenticator: authenticator,
	}
}

func (h *webhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	err := decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var apiVersion = data["apiVersion"].(string)
	var kind = data["kind"].(string)

	if kind != "TokenReview" || apiVersion != "authentication.k8s.io/v1beta1" {
		http.Error(w, fmt.Sprintf("unknown kind/apiVersion %q %q", kind, apiVersion),
			http.StatusBadRequest)
		return
	}
	var token = data["spec"].(map[string]interface{})["token"].(string)

	user, authenticated, err := h.authenticator.AuthenticateToken(token)

	if ! authenticated {
		var response status
		response.Authenticated = false
		data["status"] = response

		output, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(output)
		return
	}

	var info userInfo
	info.Username = user.GetName()
	info.UID = user.GetUID()
	info.Groups = user.GetGroups()
	info.Extra = user.GetExtra()

	var response status
	response.Authenticated = true
	response.User = info

	data["status"] = response

	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}

var (
	listenAddr     string
	tlsCertFile    string
	tlsPrivateKey  string
	keystoneURL    string
	keystoneCaFile string
)

func main() {
	flag.StringVar(&listenAddr, "listen", "localhost:8443", "<address>:<port> to listen on")
	flag.StringVar(&tlsCertFile, "tls-cert-file", "", "File containing the default x509 Certificate for HTTPS.")
	flag.StringVar(&tlsPrivateKey, "tls-private-key-file", "", "File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&keystoneURL, "keystone-url", "http://localhost/identity/v3/", "URL for the OpenStack Keystone API")
	flag.StringVar(&keystoneCaFile, "keystone-ca-file", "", "File containing the certificate authority for Keystone Service.")
	flag.Parse()

	if tlsCertFile == "" || tlsPrivateKey == "" {
		log.Fatal("Please specify --tls-cert-file and --tls-private-key-file arguments.")
	}

	handler, err := keystone.NewKeystoneAuthenticator(keystoneURL, keystoneCaFile)
	if err != nil {
		log.Fatal(err.Error())
	}

	http.Handle("/webhook", webhookServer(handler))
	log.Println("Starting webhook..")
	log.Fatal(
		http.ListenAndServeTLS(":8443",
			tlsCertFile,
			tlsPrivateKey,
			nil))
}
