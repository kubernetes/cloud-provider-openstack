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

package openstack

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

var fakeFileName = "cloud.conf"
var fakeOverrideFileName = "cloud-override.conf"
var fakeUserName = "user"
var fakePassword = "pass"
var fakeAuthUrl = "https://169.254.169.254/identity/v3"
var fakeTenantID = "c869168a828847f39f7f06edd7305637"
var fakeDomainID = "2a73b8f597c04551a0fdc8e95544be8a"
var fakeRegion = "RegionOne"
var fakeCAfile = "fake-ca.crt"
var fakeCloudName = "openstack"

// Test GetConfigFromFiles
func TestGetConfigFromFiles(t *testing.T) {
	// init file
	var fakeFileContent = `
[Global]
username=` + fakeUserName + `
password=` + fakePassword + `
auth-url=` + fakeAuthUrl + `
tenant-id=` + fakeTenantID + `
domain-id=` + fakeDomainID + `
ca-file=` + fakeCAfile + `
region=` + fakeRegion + `
[BlockStorage]
rescan-on-resize=true`

	f, err := os.Create(fakeFileName)
	if err != nil {
		t.Errorf("failed to create file: %v", err)
	}

	_, err = f.WriteString(fakeFileContent)
	f.Close()
	if err != nil {
		t.Errorf("failed to write file: %v", err)
	}
	defer os.Remove(fakeFileName)

	// Init assert
	assert := assert.New(t)
	expectedOpts := Config{}
	expectedOpts.Global.Username = fakeUserName
	expectedOpts.Global.Password = fakePassword
	expectedOpts.Global.DomainID = fakeDomainID
	expectedOpts.Global.AuthURL = fakeAuthUrl
	expectedOpts.Global.CAFile = fakeCAfile
	expectedOpts.Global.TenantID = fakeTenantID
	expectedOpts.Global.Region = fakeRegion
	expectedOpts.BlockStorage.RescanOnResize = true

	// Invoke GetConfigFromFiles
	actualAuthOpts, err := GetConfigFromFiles([]string{fakeFileName})
	if err != nil {
		t.Errorf("failed to GetConfigFromFiles: %v", err)
	}

	// Assert
	assert.Equal(expectedOpts, actualAuthOpts)

	// Create an override config file
	var fakeOverrideFileContent = `
[BlockStorage]
rescan-on-resize=false`

	f, err = os.Create(fakeOverrideFileName)
	if err != nil {
		t.Errorf("failed to create file: %v", err)
	}

	_, err = f.WriteString(fakeOverrideFileContent)
	f.Close()
	if err != nil {
		t.Errorf("failed to write file: %v", err)
	}
	defer os.Remove(fakeOverrideFileName)

	// expectedOpts should reflect the overridden value of rescan-on-resize. All
	// other values should be the same as before because they come from the
	// 'base' configuration
	expectedOpts.BlockStorage.RescanOnResize = false

	// Invoke GetConfigFromFiles with both the base and override config files
	actualAuthOpts, err = GetConfigFromFiles([]string{fakeFileName, fakeOverrideFileName})
	if err != nil {
		t.Errorf("failed to GetConfigFromFiles: %v", err)
	}

	// Assert
	assert.Equal(expectedOpts, actualAuthOpts)
}

func TestGetConfigFromFileWithUseClouds(t *testing.T) {

	wd, err := os.Getwd()
	if err != nil {
		t.Errorf("failed to find current working directory: %v", err)
	}

	// init file
	var fakeFileContent = `
[Global]
use-clouds = true
clouds-file = ` + wd + `/fixtures/clouds.yaml
cloud = ` + fakeCloudName + `
[BlockStorage]
rescan-on-resize=true`

	f, err := os.Create(fakeFileName)
	if err != nil {
		t.Errorf("failed to create file: %v", err)
	}

	_, err = f.WriteString(fakeFileContent)
	f.Close()
	if err != nil {
		t.Errorf("failed to write file: %v", err)
	}
	defer os.Remove(fakeFileName)

	// Init assert
	assert := assert.New(t)
	expectedOpts := Config{}
	expectedOpts.Global.Username = fakeUserName
	expectedOpts.Global.Password = fakePassword
	expectedOpts.Global.DomainID = fakeDomainID
	expectedOpts.Global.AuthURL = fakeAuthUrl
	expectedOpts.Global.CAFile = fakeCAfile
	expectedOpts.Global.TenantID = fakeTenantID
	expectedOpts.Global.Region = fakeRegion
	expectedOpts.Global.EndpointType = gophercloud.AvailabilityPublic
	expectedOpts.Global.UseClouds = true
	expectedOpts.Global.CloudsFile = wd + "/fixtures/clouds.yaml"
	expectedOpts.Global.Cloud = fakeCloudName
	expectedOpts.BlockStorage.RescanOnResize = true

	// Invoke GetConfigFromFiles
	actualAuthOpts, err := GetConfigFromFiles([]string{fakeFileName})
	if err != nil {
		t.Errorf("failed to GetConfigFromFiles: %v", err)
	}

	// Assert
	assert.Equal(expectedOpts, actualAuthOpts)
}

func TestUserAgentFlag(t *testing.T) {
	tests := []struct {
		name        string
		shouldParse bool
		flags       []string
		expected    []string
	}{
		{"no_flag", true, []string{}, nil},
		{"one_flag", true, []string{"--user-agent=cluster/abc-123"}, []string{"cluster/abc-123"}},
		{"multiple_flags", true, []string{"--user-agent=a/b", "--user-agent=c/d"}, []string{"a/b", "c/d"}},
		{"flag_with_space", true, []string{"--user-agent=a b"}, []string{"a b"}},
		{"flag_split_with_space", true, []string{"--user-agent=a", "b"}, []string{"a"}},
		{"empty_flag", false, []string{"--user-agent"}, nil},
	}

	for _, testCase := range tests {
		userAgentData = []string{}

		t.Run(testCase.name, func(t *testing.T) {
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			AddExtraFlags(fs)

			err := fs.Parse(testCase.flags)

			if testCase.shouldParse && err != nil {
				t.Errorf("Flags failed to parse")
			} else if !testCase.shouldParse && err == nil {
				t.Errorf("Flags should not have parsed")
			} else if testCase.shouldParse {
				if !reflect.DeepEqual(userAgentData, testCase.expected) {
					t.Errorf("userAgentData %#v did not match expected value %#v", userAgentData, testCase.expected)
				}
			}
		})
	}
}

func clearEnviron(t *testing.T) []string {
	env := os.Environ()
	for _, pair := range env {
		if strings.HasPrefix(pair, "OS_") {
			i := strings.Index(pair, "=") + 1
			os.Unsetenv(pair[:i-1])
		}
	}
	return env
}
func resetEnviron(t *testing.T, items []string) {
	for _, pair := range items {
		if strings.HasPrefix(pair, "OS_") {
			i := strings.Index(pair, "=") + 1
			if err := os.Setenv(pair[:i-1], pair[i:]); err != nil {
				t.Errorf("Setenv(%q, %q) failed during reset: %v", pair[:i-1], pair[i:], err)
			}
		}
	}
}
