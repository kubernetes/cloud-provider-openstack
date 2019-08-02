/*
Copyright 2019 The Kubernetes Authors.

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

package manilaclient

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/apiversions"
	gophercloudutils "github.com/gophercloud/gophercloud/openstack/utils"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/options"
	"k8s.io/cloud-provider-openstack/pkg/version"
)

type ClientBuilder struct {
	UserAgentData []string
}

func (cb *ClientBuilder) New(o *options.OpenstackOptions) (Interface, error) {
	return New(o, cb.UserAgentData)
}

const (
	minimumManilaVersion = "2.37"
)

var (
	manilaMicroversionRegexp = regexp.MustCompile(`^(\d+)\.(\d+)$`)
)

func splitManilaMicroversion(microversion string) (major, minor int) {
	if err := validateManilaMicroversion(microversion); err != nil {
		return
	}

	parts := strings.Split(microversion, ".")
	major, _ = strconv.Atoi(parts[0])
	minor, _ = strconv.Atoi(parts[1])

	return
}

func validateManilaMicroversion(microversion string) error {
	if !manilaMicroversionRegexp.MatchString(microversion) {
		return fmt.Errorf("invalid microversion format in %q", microversion)
	}

	return nil
}

func compareManilaVersionsLessThan(a, b string) bool {
	aMaj, aMin := splitManilaMicroversion(a)
	bMaj, bMin := splitManilaMicroversion(b)

	return aMaj < bMaj || (aMaj == bMaj && aMin < bMin)
}

func createHTTPClient(pemCertAuthority []byte, tlsInsecure bool) (http.Client, error) {
	if pemCertAuthority == nil || len(pemCertAuthority) == 0 {
		return http.Client{}, nil
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(pemCertAuthority)

	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: tlsInsecure,
	}

	tlsConfig.BuildNameToCertificate()

	return http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}, nil
}

func authenticateOpenStackClient(o *options.OpenstackOptions, userAgentData []string) (*gophercloud.ProviderClient, error) {
	provider, err := openstack.NewClient(o.OSAuthURL)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Keystone: %v", err)
	}

	var certAuth []byte

	if o.OSCertAuthorityPath != "" {
		certAuth, err = ioutil.ReadFile(o.OSCertAuthorityPath)
		if err != nil {
			return nil, fmt.Errorf("cannot read CA file %s: %v", o.OSCertAuthorityPath, err)
		}
	}

	provider.HTTPClient, err = createHTTPClient(certAuth, o.OSTLSInsecure == "true")
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %v", err)
	}

	userAgent := gophercloud.UserAgent{}
	userAgent.Prepend(fmt.Sprintf("manila-csi-plugin/%s", version.Version))
	for _, data := range userAgentData {
		userAgent.Prepend(data)
	}
	provider.UserAgent = userAgent

	const (
		v2 = "v2.0"
		v3 = "v3"
	)

	chosenVersion, _, err := gophercloudutils.ChooseVersion(provider, []*gophercloudutils.Version{
		{ID: v2, Priority: 20, Suffix: "/v2.0/"},
		{ID: v3, Priority: 30, Suffix: "/v3/"},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to choose Keystone version: %v", err)
	}

	switch chosenVersion.ID {
	case v2:
		if o.OSTrustID != "" {
			return nil, fmt.Errorf("Keystone %s does not support trustee authentication", v2)
		}

		err = openstack.AuthenticateV2(provider, *o.ToAuthOptions(), gophercloud.EndpointOpts{})
	case v3:
		err = openstack.AuthenticateV3(provider, *o.ToAuthOptionsExt(), gophercloud.EndpointOpts{})
	default:
		err = fmt.Errorf("unrecognized Keystone version: %s", chosenVersion.ID)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with Keystone %s: %v", chosenVersion.ID, err)
	}

	return provider, err
}

func validateManilaClient(c *gophercloud.ServiceClient) error {
	serverVersion, err := apiversions.Get(c, "v2").Extract()
	if err != nil {
		return fmt.Errorf("failed to get Manila v2 API microversions: %v", err)
	}

	if err = validateManilaMicroversion(serverVersion.MinVersion); err != nil {
		return fmt.Errorf("server's minimum microversion is invalid: %v", err)
	}

	if err = validateManilaMicroversion(serverVersion.Version); err != nil {
		return fmt.Errorf("server's maximum microversion is invalid: %v", err)
	}

	if compareManilaVersionsLessThan(c.Microversion, serverVersion.MinVersion) {
		return fmt.Errorf("client's microversion %s is lower than server's minimum microversion %s", c.Microversion, serverVersion.MinVersion)
	}

	if compareManilaVersionsLessThan(serverVersion.Version, c.Microversion) {
		return fmt.Errorf("client's microversion %s is higher than server's highest supported microversion %s", c.Microversion, serverVersion.Version)
	}

	return nil
}

func New(o *options.OpenstackOptions, userAgentData []string) (*Client, error) {
	// Authenticate and create Manila v2 client

	provider, err := authenticateOpenStackClient(o, userAgentData)
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
		return nil, fmt.Errorf("Manila v2 client validation failed: %v", err)
	}

	return &Client{c: client}, nil
}
