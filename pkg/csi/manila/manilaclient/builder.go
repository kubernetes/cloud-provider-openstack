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
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/apiversions"
	"k8s.io/cloud-provider-openstack/pkg/client"
)

const (
	minimumManilaVersion = "2.37"
)

var (
	manilaMicroversionRegexp = regexp.MustCompile(`^(\d+)\.(\d+)$`)
)

type ClientBuilder struct {
	UserAgent          string
	ExtraUserAgentData []string
}

func (cb *ClientBuilder) New(ctx context.Context, o *client.AuthOpts) (Interface, error) {
	return New(ctx, o, cb.UserAgent, cb.ExtraUserAgentData)
}

func New(ctx context.Context, o *client.AuthOpts, userAgent string, extraUserAgentData []string) (*Client, error) {
	// Authenticate and create Manila v2 client
	// If UseClouds is set, read clouds.yaml file
	if o.UseClouds {
		err := client.ReadClouds(o)
		if err != nil {
			return nil, fmt.Errorf("failed to read clouds.yaml: %v", err)
		}
	}

	provider, err := client.NewOpenStackClient(o, userAgent, extraUserAgentData...)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %v", err)
	}

	client, err := openstack.NewSharedFileSystemV2(provider, gophercloud.EndpointOpts{
		Region:       o.Region,
		Availability: o.EndpointType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manila v2 client: %v", err)
	}

	// Check client's and server's versions for compatibility

	client.Microversion = minimumManilaVersion
	if err = validateManilaClient(ctx, client); err != nil {
		return nil, fmt.Errorf("manila v2 client validation failed: %v", err)
	}

	return &Client{c: client}, nil
}

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

func validateManilaClient(ctx context.Context, c *gophercloud.ServiceClient) error {
	serverVersion, err := apiversions.Get(ctx, c, "v2").Extract()
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
