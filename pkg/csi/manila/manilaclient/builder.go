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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/apiversions"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
)

type ClientBuilder struct {
	UserAgent          string
	ExtraUserAgentData []string
}

func (cb *ClientBuilder) New(o *openstack_provider.AuthOpts) (Interface, error) {
	return New(o, cb.UserAgent, cb.ExtraUserAgentData)
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

func New(o *openstack_provider.AuthOpts, userAgent string, extraUserAgentData []string) (*Client, error) {
	// Authenticate and create Manila v2 client
	provider, err := openstack_provider.NewOpenStackClient(o, userAgent, extraUserAgentData...)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %v", err)
	}

	client, err := openstack.NewSharedFileSystemV2(provider, gophercloud.EndpointOpts{Region: o.Region})
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

func NewFromServiceClient(c *gophercloud.ServiceClient) *Client {
	return &Client{c: c}
}
