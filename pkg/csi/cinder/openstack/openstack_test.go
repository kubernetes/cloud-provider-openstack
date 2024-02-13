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
	"testing"

	"github.com/gophercloud/gophercloud"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"k8s.io/cloud-provider-openstack/pkg/client"
)

var fakeFileName = "cloud.conf"
var fakeOverrideFileName = "cloud-override.conf"
var fakeUserName = "user"
var fakePassword = "pass"
var fakeAuthURL = "https://169.254.169.254/identity/v3"
var fakeTenantID = "c869168a828847f39f7f06edd7305637"
var fakeDomainID = "2a73b8f597c04551a0fdc8e95544be8a"
var fakeRegion = "RegionOne"
var fakeCAfile = "fake-ca.crt"
var fakeCloudName = "openstack"

var fakeUserName_cloud2 = "user"
var fakePassword_cloud2 = "pass"
var fakeAuthURL_cloud2 = "https://169.254.169.254/identity/v3"
var fakeTenantID_cloud2 = "c869168a828847f39f7f06edd7305637"
var fakeDomainID_cloud2 = "2a73b8f597c04551a0fdc8e95544be8a"
var fakeRegion_cloud2 = "RegionTwo"
var fakeCAfile_cloud2 = "fake-ca.crt"
var fakeCloudName_cloud2 = "openstack_cloud2"

var fakeUserName_cloud3 = "user_cloud3"
var fakePassword_cloud3 = "pass_cloud3"
var fakeAuthURL_cloud3 = "https://961.452.961.452/identity/v3"
var fakeTenantID_cloud3 = "66c684738f74161ad8b41cb56224b311"
var fakeDomainID_cloud3 = "032da590a2714eda744bd321b5356c7e"
var fakeRegion_cloud3 = "AnotherRegion"
var fakeCAfile_cloud3 = "fake-ca_cloud3.crt"
var fakeCloudName_cloud3 = "openstack_cloud3"

// Test GetConfigFromFiles
func TestGetConfigFromFiles(t *testing.T) {
	// init file
	var fakeFileContent = `
[Global]
username=` + fakeUserName + `
password=` + fakePassword + `
auth-url=` + fakeAuthURL + `
tenant-id=` + fakeTenantID + `
domain-id=` + fakeDomainID + `
ca-file=` + fakeCAfile + `
region=` + fakeRegion + `
[Global "cloud2"]
username=` + fakeUserName_cloud2 + `
password=` + fakePassword_cloud2 + `
auth-url=` + fakeAuthURL_cloud2 + `
tenant-id=` + fakeTenantID_cloud2 + `
domain-id=` + fakeDomainID_cloud2 + `
ca-file=` + fakeCAfile_cloud2 + `
region=` + fakeRegion_cloud2 + `
[Global "cloud3"]
username=` + fakeUserName_cloud3 + `
password=` + fakePassword_cloud3 + `
auth-url=` + fakeAuthURL_cloud3 + `
tenant-id=` + fakeTenantID_cloud3 + `
domain-id=` + fakeDomainID_cloud3 + `
ca-file=` + fakeCAfile_cloud3 + `
region=` + fakeRegion_cloud3 + `
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
	expectedOpts.Global = make(map[string]*client.AuthOpts, 3)

	expectedOpts.Global[""] = &client.AuthOpts{
		Username: fakeUserName,
		Password: fakePassword,
		DomainID: fakeDomainID,
		AuthURL:  fakeAuthURL,
		CAFile:   fakeCAfile,
		TenantID: fakeTenantID,
		Region:   fakeRegion,
	}
	expectedOpts.Global["cloud2"] = &client.AuthOpts{
		Username: fakeUserName_cloud2,
		Password: fakePassword_cloud2,
		DomainID: fakeDomainID_cloud2,
		AuthURL:  fakeAuthURL_cloud2,
		CAFile:   fakeCAfile_cloud2,
		TenantID: fakeTenantID_cloud2,
		Region:   fakeRegion_cloud2,
	}
	expectedOpts.Global["cloud3"] = &client.AuthOpts{
		Username: fakeUserName_cloud3,
		Password: fakePassword_cloud3,
		DomainID: fakeDomainID_cloud3,
		AuthURL:  fakeAuthURL_cloud3,
		CAFile:   fakeCAfile_cloud3,
		TenantID: fakeTenantID_cloud3,
		Region:   fakeRegion_cloud3,
	}

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
[Global "cloud2"]
use-clouds = true
clouds-file = ` + wd + `/fixtures/clouds.yaml
cloud = ` + fakeCloudName_cloud2 + `
[Global "cloud3"]
use-clouds = true
clouds-file = ` + wd + `/fixtures/clouds.yaml
cloud = ` + fakeCloudName_cloud3 + `
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
	expectedOpts.Global = make(map[string]*client.AuthOpts, 3)

	expectedOpts.Global[""] = &client.AuthOpts{
		Username:     fakeUserName,
		Password:     fakePassword,
		DomainID:     fakeDomainID,
		AuthURL:      fakeAuthURL,
		CAFile:       fakeCAfile,
		TenantID:     fakeTenantID,
		Region:       fakeRegion,
		EndpointType: gophercloud.AvailabilityPublic,
		UseClouds:    true,
		CloudsFile:   wd + "/fixtures/clouds.yaml",
		Cloud:        fakeCloudName,
	}
	expectedOpts.Global["cloud2"] = &client.AuthOpts{
		Username:     fakeUserName_cloud2,
		Password:     fakePassword_cloud2,
		DomainID:     fakeDomainID_cloud2,
		AuthURL:      fakeAuthURL_cloud2,
		CAFile:       fakeCAfile_cloud2,
		TenantID:     fakeTenantID_cloud2,
		Region:       fakeRegion_cloud2,
		EndpointType: gophercloud.AvailabilityPublic,
		UseClouds:    true,
		CloudsFile:   wd + "/fixtures/clouds.yaml",
		Cloud:        fakeCloudName_cloud2,
	}
	expectedOpts.Global["cloud3"] = &client.AuthOpts{
		Username:     fakeUserName_cloud3,
		Password:     fakePassword_cloud3,
		DomainID:     fakeDomainID_cloud3,
		AuthURL:      fakeAuthURL_cloud3,
		CAFile:       fakeCAfile_cloud3,
		TenantID:     fakeTenantID_cloud3,
		Region:       fakeRegion_cloud3,
		EndpointType: gophercloud.AvailabilityPublic,
		UseClouds:    true,
		CloudsFile:   wd + "/fixtures/clouds.yaml",
		Cloud:        fakeCloudName_cloud3,
	}

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
