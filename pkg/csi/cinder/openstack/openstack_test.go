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
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

var fakeFileName = "cloud.conf"
var fakeUserName = "user"
var fakePassword = "pass"
var fakeAuthUrl = "https://169.254.169.254/identity/v3"
var fakeTenantID = "c869168a828847f39f7f06edd7305637"
var fakeDomainID = "2a73b8f597c04551a0fdc8e95544be8a"
var fakeRegion = "RegionOne"
var fakeCAfile = "fake-ca.crt"
var fakeBsVersion = "v3"

// Test GetConfigFromFile
func TestGetConfigFromFile(t *testing.T) {
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
rescan-on-resize=true
bs-version=` + fakeBsVersion

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
	expectedOpts.BlockStorage.BSVersion = fakeBsVersion

	// Invoke GetConfigFromFile
	actualAuthOpts, err := GetConfigFromFile(fakeFileName)
	if err != nil {
		t.Errorf("failed to GetConfigFromFile: %v", err)
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

func TestBlockStorageClientV2(t *testing.T) {
	t.Log("using cinder v2 api controlled by config file")

	// init file
	tmpFile, err := ioutil.TempFile(os.TempDir(), "csi-")
	assert.NoError(t, err)

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
rescan-on-resize=true
bs-version=v2`

	_, err = tmpFile.WriteString(fakeFileContent)
	assert.NoError(t, err)
	_ = tmpFile.Close()
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()

	// patch temporary config file position for testing
	var backup string
	backup, configFile = configFile, tmpFile.Name()
	defer func() {
		configFile = backup
	}()

	// check client version,
	provider, err := CreateOpenStackProvider()
	if err, ok := err.(*gophercloud.ErrEndpointNotFound); ok {
		t.Skipf("BlockStorage endpoint not supported: %v", err)
	}
	assert.NoError(t, err)

	client := provider.(*OpenStack)
	assert.Equal(t, "volumev2", client.blockstorage.Type)
}

// test cinder v3 as default
func TestBlockStorageClient(t *testing.T) {
	t.Log("using cinder v3 api as default controlled by config file")
	// init file
	tmpFile, err := ioutil.TempFile(os.TempDir(), "csi-")
	assert.NoError(t, err)

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

	_, err = tmpFile.WriteString(fakeFileContent)
	assert.NoError(t, err)
	_ = tmpFile.Close()
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()

	// patch temporary config file position for testing
	var backup string
	backup, configFile = configFile, tmpFile.Name()
	defer func() {
		configFile = backup
	}()

	// check client version,
	provider, err := CreateOpenStackProvider()
	if err, ok := err.(*gophercloud.ErrEndpointNotFound); ok {
		t.Skipf("BlockStorage endpoint not supported: %v", err)
	}
	assert.NoError(t, err)

	client := provider.(*OpenStack)
	assert.Equal(t, "volumev3", client.blockstorage.Type)
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
