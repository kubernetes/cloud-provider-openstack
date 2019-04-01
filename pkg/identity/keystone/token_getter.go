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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"io/ioutil"
	"net/http"
)

type Options struct {
	AuthOptions    gophercloud.AuthOptions
	ClientCertPath string
	ClientKeyPath  string
	ClientCAPath   string
}

// GetToken creates a token by authenticate with keystone.
func GetToken(options Options) (*tokens3.Token, error) {
	var token *tokens3.Token
	var setTransport bool

	// Create new identity client
	client, err := openstack.NewClient(options.AuthOptions.IdentityEndpoint)
	if err != nil {
		msg := fmt.Errorf("failed: Initializing openstack authentication client: %v", err)
		return token, msg
	}
	tlsConfig := &tls.Config{}
	setTransport = false

	if options.ClientCertPath != "" && options.ClientKeyPath != "" {
		clientCert, err := ioutil.ReadFile(options.ClientCertPath)
		if err != nil {
			msg := fmt.Errorf("failed: Cannot read cert file: %v", err)
			return token, msg
		}

		clientKey, err := ioutil.ReadFile(options.ClientKeyPath)
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
		setTransport = true
	}

	if options.ClientCAPath != "" {
		certBytes, err := ioutil.ReadFile(options.ClientCAPath)
		if err != nil {
			msg := fmt.Errorf("failed: Cannot read CA file: %v", err)
			return token, msg
		}

		certpool, err := x509.SystemCertPool()
		if err != nil {
			msg := fmt.Errorf("failed: Cannot create cert pool:: %v", err)
			return token, msg
		}

		certpool.AppendCertsFromPEM(certBytes)
		tlsConfig.RootCAs = certpool
		setTransport = true
	}

	if setTransport {
		transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig}
		client.HTTPClient.Transport = transport
	}

	v3Client, err := openstack.NewIdentityV3(client, gophercloud.EndpointOpts{})
	if err != nil {
		msg := fmt.Errorf("failed: Initializing openstack authentication client: %v", err)
		return token, msg
	}

	// Issue new unscoped token
	result := tokens3.Create(v3Client, &options.AuthOptions)
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
