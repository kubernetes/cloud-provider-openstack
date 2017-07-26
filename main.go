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
	"io/ioutil"
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

var httpClient = &http.Client{}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
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

	// use the Keystone V3 API - "GET /v3/auth/tokens" to
	// validate the token we get from the user.
	// http://git.openstack.org/cgit/openstack/keystone/tree/api-ref/source/v3/authenticate-v3.inc#n437
	urlStr := fmt.Sprintf("%s/auth/tokens/", keystoneURL)
	request, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	request.Header.Add("X-Auth-Token", token)
	request.Header.Add("X-Subject-Token", token)

	resp, err := httpClient.Do(request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != 200 {
		var response status
		response.Authenticated = false
		data["status"] = response

		output, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(output)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var keystone_data map[string]interface{}
	err = json.Unmarshal(body, &keystone_data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	token_info := keystone_data["token"].(map[string]interface{})
	project_info := token_info["project"].(map[string]interface{})

	var info userInfo
	info.Username = project_info["name"].(string)
	info.UID = project_info["id"].(string)

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
	listenAddr    string
	tlsCertFile   string
	tlsPrivateKey string
	keystoneURL   string
)

func main() {
	flag.StringVar(&listenAddr, "listen", "localhost:8443", "<address>:<port> to listen on")
	flag.StringVar(&tlsCertFile, "tls-cert-file", "", "File containing the default x509 Certificate for HTTPS.")
	flag.StringVar(&tlsPrivateKey, "tls-private-key-file", "", "File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&keystoneURL, "keystone-url", "http://localhost/identity/v3/", "URL for the OpenStack Keystone API")
	flag.Parse()

	if tlsCertFile == "" || tlsPrivateKey == "" {
		log.Fatal("Please specify --tls-cert-file and --tls-private-key-file arguments.")
	}

	http.HandleFunc("/webhook", handleWebhook)
	log.Println("Starting webhook..")
	log.Fatal(
		http.ListenAndServeTLS(":8443",
			tlsCertFile,
			tlsPrivateKey,
			nil))
}