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

package volumeservice

import (
	"github.com/stretchr/testify/assert"
	"os"
	"strings"
	"testing"
)

var fakeUserName = "user"
var fakePassword = "pass"
var fakeAuthUrl = "https://169.254.169.254/identity/v3"
var fakeTenantID = "c869168a828847f39f7f06edd7305637"
var fakeDomainID = "2a73b8f597c04551a0fdc8e95544be8a"
var fakeRegion = "RegionOne"

// Test GetConfigFromEnv
func TestGetConfigFromEnv(t *testing.T) {
	env := clearEnviron(t)
	defer resetEnviron(t, env)

	// init env
	os.Setenv("OS_AUTH_URL", fakeAuthUrl)
	os.Setenv("OS_USERNAME", fakeUserName)
	os.Setenv("OS_PASSWORD", fakePassword)
	os.Setenv("OS_TENANT_ID", fakeTenantID)
	os.Setenv("OS_DOMAIN_ID", fakeDomainID)
	os.Setenv("OS_REGION_NAME", fakeRegion)

	// Init assert
	assert := assert.New(t)

	// Invoke GetConfigFromEnv
	cfg, err := getConfig("")
	assert.Nil(err)

	// Assert
	assert.Equal(cfg.Global.AuthURL, fakeAuthUrl)
	assert.Equal(cfg.Global.Username, fakeUserName)
	assert.Equal(cfg.Global.Password, fakePassword)
	assert.Equal(cfg.Global.TenantID, fakeTenantID)
	assert.Equal(cfg.Global.DomainID, fakeDomainID)
	assert.Equal(cfg.Global.Region, fakeRegion)
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
