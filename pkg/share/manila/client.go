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

package manila

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/apiversions"
	gophercloudutils "github.com/gophercloud/gophercloud/openstack/utils"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions"
)

const (
	minimumManilaVersion = "2.21"
)

var (
	microversionRegexp = regexp.MustCompile("^\\d+\\.\\d+$")
)

func splitMicroversion(microversion string) (major, minor int) {
	if err := validateMicroversion(microversion); err != nil {
		return
	}

	parts := strings.Split(microversion, ".")
	major, _ = strconv.Atoi(parts[0])
	minor, _ = strconv.Atoi(parts[1])

	return
}

func validateMicroversion(microversion string) error {
	if !microversionRegexp.MatchString(microversion) {
		return fmt.Errorf("invalid microversion format in %q", microversion)
	}

	return nil
}

func compareVersionsLessThan(a, b string) bool {
	aMaj, aMin := splitMicroversion(a)
	bMaj, bMin := splitMicroversion(b)

	return aMaj < bMaj || (aMaj == bMaj && aMin < bMin)
}

func createHTTPclient(o *shareoptions.OpenStackOptions) (http.Client, error) {
	if o.OSCertAuthority == "" {
		return http.Client{}, nil
	}

	if o.OSTLSInsecure == "" {
		o.OSTLSInsecure = "false"
	}

	allowInsecure, err := strconv.ParseBool(o.OSTLSInsecure)
	if err != nil {
		return http.Client{}, fmt.Errorf("failed to parse parameter os-insecureTLS: %v", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM([]byte(o.OSCertAuthority))

	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: allowInsecure,
	}

	tlsConfig.BuildNameToCertificate()

	return http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}, nil
}

func authenticateClient(o *shareoptions.OpenStackOptions) (*gophercloud.ProviderClient, error) {
	provider, err := openstack.NewClient(o.OSAuthURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create Keystone client: %v", err)
	}

	provider.HTTPClient, err = createHTTPclient(o)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %v", err)
	}

	const (
		v2 = "v2.0"
		v3 = "v3"
	)

	chosenVersion, _, err := gophercloudutils.ChooseVersion(provider, []*gophercloudutils.Version{
		{ID: v2, Priority: 20, Suffix: "/v2.0/"},
		{ID: v3, Priority: 30, Suffix: "/v3/"},
	})

	switch chosenVersion.ID {
	case v2:
		if o.OSTrustID != "" {
			return nil, fmt.Errorf("Keystone %s does not support trustee authentication", v2)
		}

		err = openstack.AuthenticateV2(provider, *o.ToAuthOptions(), gophercloud.EndpointOpts{})
	case v3:
		err = openstack.AuthenticateV3(provider, *o.ToAuthOptionsExt(), gophercloud.EndpointOpts{})
	default:
		return nil, fmt.Errorf("unrecognized Keystone version: %s", chosenVersion.ID)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with Keystone: %v", err)
	}

	return provider, nil
}

func validateManilaClient(c *gophercloud.ServiceClient) error {
	serverVersion, err := apiversions.Get(c, "v2").Extract()
	if err != nil {
		return fmt.Errorf("failed to get Manila v2 API microversions: %v", err)
	}

	if err = validateMicroversion(serverVersion.MinVersion); err != nil {
		return fmt.Errorf("server's minimum microversion is invalid: %v", err)
	}

	if err = validateMicroversion(serverVersion.Version); err != nil {
		return fmt.Errorf("server's maximum microversion is invalid: %v", err)
	}

	if compareVersionsLessThan(c.Microversion, serverVersion.MinVersion) {
		return fmt.Errorf("client's microversion %s is lower than server's minimum microversion %s", c.Microversion, serverVersion.MinVersion)
	}

	if compareVersionsLessThan(serverVersion.Version, c.Microversion) {
		return fmt.Errorf("client's microversion %s is higher than server's highest supported microversion %s", c.Microversion, serverVersion.Version)
	}

	return nil
}

// NewManilaV2Client Creates Manila v2 client
// Authenticates to the Manila service with credentials passed in shareoptions.OpenStackOptions
func NewManilaV2Client(o *shareoptions.OpenStackOptions) (*gophercloud.ServiceClient, error) {
	// Authenticate and create Manila v2 client

	provider, err := authenticateClient(o)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %v", err)
	}

	client, err := openstack.NewSharedFileSystemV2(provider, gophercloud.EndpointOpts{Region: o.OSRegionName})
	if err != nil {
		return nil, fmt.Errorf("failed to create Manila v2 client: %v", err)
	}

	// Check client's and server's versions for compatibility

	client.Microversion = minimumManilaVersion
	if err = validateManilaClient(client); err != nil {
		return nil, fmt.Errorf("failed to validate Manila v2 client: %v", err)
	}

	return client, nil
}
