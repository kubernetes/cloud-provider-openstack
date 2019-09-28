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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/apiversions"
	"github.com/spf13/pflag"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
)

const (
	minimumManilaVersion = "2.21"
)

var (
	microversionRegexp = regexp.MustCompile("^\\d+\\.\\d+$")

	userAgentData []string
)

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

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
// Authenticates to the Manila service with credentials passed in openstack_provider.AuthOpts
func NewManilaV2Client(o *openstack_provider.AuthOpts) (*gophercloud.ServiceClient, error) {
	// Authenticate and create Manila v2 client
	provider, err := openstack_provider.NewOpenStackClient(o, "manila-provisioner", userAgentData...)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %v", err)
	}

	client, err := openstack.NewSharedFileSystemV2(provider, gophercloud.EndpointOpts{
		Region:       o.Region,
		Availability: o.EndpointType,
	})
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
