/*
Copyright 2017 The Kubernetes Authors.

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
	"errors"
	"encoding/json"
	"fmt"
	"net/http"
	"log"
	//"strings"

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/utils"

	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
)

// Construct a Keystone v3 client, bail out if we cannot find the v3 API endpoint
func createIdentityV3Provider(options gophercloud.AuthOptions, transport http.RoundTripper) (*gophercloud.ProviderClient, error) {
	client, err := openstack.NewClient(options.IdentityEndpoint)
	if err != nil {
		return nil, err
	}

	if transport != nil {
		client.HTTPClient.Transport = transport
	}

	versions := []*utils.Version{
		{ID: "v3.0", Priority: 30, Suffix: "/v3/"},
	}
	chosen, _, err := utils.ChooseVersion(client, versions)
	if err != nil {
		return nil, fmt.Errorf("Unable to find identity API v3 version : %v", err)
	}

	switch chosen.ID {
	case "v3.0":
		return client, nil
	default:
		// The switch statement must be out of date from the versions list.
		return nil, fmt.Errorf("Unsupported identity API version: %s", chosen.ID)
	}
}

func createKeystoneClient(authURL string, caFile string) (*gophercloud.ServiceClient, error) {
	// FIXME: Enable this check later
	//if !strings.HasPrefix(authURL, "https") {
	//	return nil, errors.New("Auth URL should be secure and start with https")
	//}
	var transport http.RoundTripper
	if authURL == "" {
		return nil, errors.New("Auth URL is empty")
	}
	if caFile != "" {
		roots, err := certutil.NewPool(caFile)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{}
		config.RootCAs = roots
		transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: config})
	}
	opts := gophercloud.AuthOptions{IdentityEndpoint: authURL}
	provider, err := createIdentityV3Provider(opts, transport)
	if err != nil {
		return nil, err
	}

	// We should use the V3 API
	client, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		glog.V(4).Info("Failed: Unable to use keystone v3 identity service: %v", err)
		return nil, errors.New("Failed to authenticate")
	}
	if err != nil {
		glog.V(4).Info("Failed: Starting openstack authenticate client: %v", err)
		return nil, errors.New("Failed to authenticate")
	}

	// Make sure we look under /v3 for resources
	client.IdentityBase = client.IdentityEndpoint
	client.Endpoint = client.IdentityEndpoint
	return client, nil
}

// NewKeystoneAuthenticator returns a password authenticator that validates credentials using openstack keystone
func NewKeystoneAuthenticator(authURL string, caFile string) (*KeystoneAuthenticator, error) {
	client, err := createKeystoneClient(authURL, caFile)
	if err != nil {
		return nil, err
	}

	return &KeystoneAuthenticator{authURL: authURL, client: client}, nil
}

func NewKeystoneAuthorizer(authURL string, caFile string, policyFile string) (*KeystoneAuthorizer, error) {
	client, err := createKeystoneClient(authURL, caFile)
	if err != nil {
		return nil, err
	}

	policyList, err := NewFromFile(policyFile)
	output, err := json.MarshalIndent(policyList, "", "  ")
	if err == nil {
		log.Printf(">>> Policy %s", string(output))
	} else {
		log.Fatalf(">>> Error %#v", err)
	}

	return &KeystoneAuthorizer{authURL: authURL, client: client, pl:policyList}, nil
}