/*
Copyright 2018 The Kubernetes Authors.

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

package keystone

import (
	"fmt"

	"crypto/tls"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"io/ioutil"
	"net/http"
)

// GetToken creates a token by authenticate with keystone.
func GetToken(options gophercloud.AuthOptions, clientCertPath string, clientKeyPath string) (*tokens3.Token, error) {
	var token *tokens3.Token

	// Create new identity client
	client, err := openstack.NewClient(options.IdentityEndpoint)
	if err != nil {
		msg := fmt.Errorf("failed: Initializing openstack authentication client: %v", err)
		return token, msg
	}
	tlsConfig := &tls.Config{}

	if clientCertPath != "" && clientKeyPath != "" {
		clientCert, err := ioutil.ReadFile(clientCertPath)
		if err != nil {
			msg := fmt.Errorf("failed: Cannot read cert file: %v", err)
			return token, msg
		}

		clientKey, err := ioutil.ReadFile(clientKeyPath)
		if err != nil {
			msg := fmt.Errorf("failed: Cannot read key file: %v", err)
			return token, msg
		}

		cert, err := tls.X509KeyPair([]byte(clientCert), []byte(clientKey))
		if err != nil {
			msg := fmt.Errorf("failed: Cannot create keypair:: %v", err)
			return token, msg
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		tlsConfig.BuildNameToCertificate()

		transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig}
		client.HTTPClient.Transport = transport
	}

	v3Client, err := openstack.NewIdentityV3(client, gophercloud.EndpointOpts{})
	if err != nil {
		msg := fmt.Errorf("failed: Initializing openstack authentication client: %v", err)
		return token, msg
	}

	// Issue new unscoped token
	result := tokens3.Create(v3Client, &options)
	if result.Err != nil {
		return token, result.Err
	}
	token, err = result.ExtractToken()
	if err != nil {
		msg := fmt.Errorf("failed: Cannot extract the token from the response")
		return token, msg
	}

	return token, nil
}
