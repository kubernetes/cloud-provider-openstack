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
		return fmt.Errorf("Invalid microversion format in %q", microversion)
	}

	return nil
}

func compareVersionsLessThan(a, b string) bool {
	aMaj, aMin := splitMicroversion(a)
	bMaj, bMin := splitMicroversion(b)

	return aMaj < bMaj || (aMaj == bMaj && aMin < bMin)
}

// NewManilaV2Client Creates Manila v2 client
// Authenticates to the Manila service with credentials passed in env variables
func NewManilaV2Client(osOptions *shareoptions.OpenStackOptions) (*gophercloud.ServiceClient, error) {
	// Authenticate

	provider, err := openstack.AuthenticatedClient(*osOptions.ToAuthOptions())
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate Manila v2 client: %v", err)
	}

	client, err := openstack.NewSharedFileSystemV2(provider, gophercloud.EndpointOpts{Region: osOptions.OSRegionName})
	if err != nil {
		return nil, fmt.Errorf("failed to create Manila v2 client: %v", err)
	}

	// Check client's and server's versions for compatibility

	client.Microversion = minimumManilaVersion

	serverVersion, err := apiversions.Get(client, "v2").Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to get Manila v2 API microversions: %v", err)
	}

	if err = validateMicroversion(serverVersion.MinVersion); err != nil {
		return nil, fmt.Errorf("server's minimum microversion is invalid: %v", err)
	}

	if err = validateMicroversion(serverVersion.Version); err != nil {
		return nil, fmt.Errorf("server's maximum microversion is invalid: %v", err)
	}

	if compareVersionsLessThan(client.Microversion, serverVersion.MinVersion) {
		return nil, fmt.Errorf("client's microversion %s is lower than server's minimum microversion %s", client.Microversion, serverVersion.MinVersion)
	}

	if compareVersionsLessThan(serverVersion.Version, client.Microversion) {
		return nil, fmt.Errorf("client's microversion %s is higher than server's highest supported microversion %s", client.Microversion, serverVersion.Version)
	}

	return client, nil
}
