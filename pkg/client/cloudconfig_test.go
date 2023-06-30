/*
Copyright 2023 The Kubernetes Authors.

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

package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gophercloud/gophercloud"
	"github.com/stretchr/testify/assert"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/util/testutil"
)

const (
	refreshPeriod = time.Second

	// Basic stuff for testing.
	fakeFileName         = "cloud.conf"
	fakeOverrideFileName = "cloud-override.conf"
	fakeUserName         = "user"
	fakePassword         = "pass"
	fakeAuthURL          = "https://169.254.169.254/identity/v3"
	fakeTenantID         = "c869168a828847f39f7f06edd7305637"
	fakeDomainID         = "2a73b8f597c04551a0fdc8e95544be8a"
	fakeRegion           = "RegionOne"
	fakeCAfile           = "fake-ca.crt"
	fakeCloudName        = "openstack"

	// Stuff to use to test dynamic updates.
	fakePasswordUpdate = "Passw0rd" // You know who you are...
)

// fakeCloudConfig returns a basic cloud config file.
func fakeCloudConfig() string {
	return `[Global]
username=` + fakeUserName + `
password=` + fakePassword + `
auth-url=` + fakeAuthURL + `
tenant-id=` + fakeTenantID + `
domain-id=` + fakeDomainID + `
ca-file=` + fakeCAfile + `
region=` + fakeRegion + `
[BlockStorage]
rescan-on-resize=true`
}

// fakeCloudConfigUpdate is an updated version of the basic cloud
// config, but doing a simple credential rotation.
func fakeCloudConfigUpdate() string {
	return `[Global]
username=` + fakeUserName + `
password=` + fakePasswordUpdate + `
auth-url=` + fakeAuthURL + `
tenant-id=` + fakeTenantID + `
domain-id=` + fakeDomainID + `
ca-file=` + fakeCAfile + `
region=` + fakeRegion + `
[BlockStorage]
rescan-on-resize=true`
}

// fakeCloudConfigOverride returns a basic cloud config override.
func fakeCloudConfigOverride() string {
	return `[BlockStorage]
rescan-on-resize=false`
}

// fakeCloudsYAML returns an OpenStack clouds.yaml formatted file.
func fakeCloudsYAML() string {
	return `clouds:
  openstack:
    auth:
      auth_url: ` + fakeAuthURL + `
      username: ` + fakeUserName + `
      password: ` + fakePassword + `
      project_id: ` + fakeTenantID + `
      domain_id: ` + fakeDomainID + `
    region_name: ` + fakeRegion + `
    cacert: ` + fakeCAfile
}

// fakeCloudConfigUseClouds returns a cloud config that indirects to
// a file on the file system, useful for bakinf credentials into an
// image rather than exposing them as a Secret...
func fakeCloudConfigUseClouds(path string) string {
	return `[Global]
use-clouds = true
clouds-file = ` + path + `
cloud = ` + fakeCloudName
}

// fakeProviderFunc allows us to override the default and mock for testing.
func fakeProviderFunc(cfg *client.AuthOpts, userAgent string, extraUserAgent ...string) (*gophercloud.ProviderClient, error) {
	provider := &gophercloud.ProviderClient{
		// TokenID will be different each time due to a "reauthentication".
		TokenID: uuid.New().String(),
	}

	return provider, nil
}

// TestBlockStorageOpts are "appllication-specific" options that
// can be enabled for individul controllers.
type TestBlockStorageOpts struct {
	RescanOnResize bool `gcfg:"rescan-on-resize"`
}

// TestCloudConfig is a concrete cloud config definition for am
// application.
type TestCloudConfig struct {
	Global       client.AuthOpts
	BlockStorage TestBlockStorageOpts
}

// AuthOpts implmenets the CloudConfig interface.
func (c *TestCloudConfig) AuthOpts() *client.AuthOpts {
	return &c.Global
}

// mustNewCloudConfigFactory returns a new cloud config factory or dies.
func mustNewCloudConfigFactory(t *testing.T, ctx context.Context, paths ...string) *client.CloudConfigFactory {
	t.Helper()

	opts := client.CloudConfigFactoryOpts{
		Period:       refreshPeriod,
		ProviderFunc: fakeProviderFunc,
	}

	factory, err := client.NewCloudConfigFactory(ctx, opts, paths...)
	assert.Nil(t, err)

	return factory
}

// TestGetConfig tests the cloud config factory can read the config, especially
// with application-specific sections.
func TestGetConfig(t *testing.T) {
	t.Parallel()

	path, cleanup := testutil.MustCreateFile(t, fakeCloudConfig())
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factory := mustNewCloudConfigFactory(t, ctx, path)

	var cfg TestCloudConfig

	err := factory.GetConfig(&cfg)
	assert.Nil(t, err)

	assert.Equal(t, fakeUserName, cfg.Global.Username)
	assert.Equal(t, fakePassword, cfg.Global.Password)
	assert.Equal(t, fakeAuthURL, cfg.Global.AuthURL)
	assert.Equal(t, fakeTenantID, cfg.Global.TenantID)
	assert.Equal(t, fakeDomainID, cfg.Global.DomainID)
	assert.Equal(t, fakeCAfile, cfg.Global.CAFile)
	assert.Equal(t, fakeRegion, cfg.Global.Region)
	assert.Equal(t, true, cfg.BlockStorage.RescanOnResize)
}

// TestGetConfigOverride essentially duplicates TestGetConfig, but uses two
// config files, the latter should override values defined in the first.
func TestGetConfigOverride(t *testing.T) {
	t.Parallel()

	path1, cleanup1 := testutil.MustCreateFile(t, fakeCloudConfig())
	defer cleanup1()

	path2, cleanup2 := testutil.MustCreateFile(t, fakeCloudConfigOverride())
	defer cleanup2()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factory := mustNewCloudConfigFactory(t, ctx, path1, path2)

	var cfg TestCloudConfig

	err := factory.GetConfig(&cfg)
	assert.Nil(t, err)

	assert.Equal(t, fakeUserName, cfg.Global.Username)
	assert.Equal(t, fakePassword, cfg.Global.Password)
	assert.Equal(t, fakeAuthURL, cfg.Global.AuthURL)
	assert.Equal(t, fakeTenantID, cfg.Global.TenantID)
	assert.Equal(t, fakeDomainID, cfg.Global.DomainID)
	assert.Equal(t, fakeCAfile, cfg.Global.CAFile)
	assert.Equal(t, fakeRegion, cfg.Global.Region)
	assert.Equal(t, false, cfg.BlockStorage.RescanOnResize)
}

// TestGetConfigUseClouds tests that a cloud config can redirect configuration
// to come from an OpenStack clouds.yaml file.
func TestGetConfigUseClouds(t *testing.T) {
	t.Parallel()

	path1, cleanup1 := testutil.MustCreateFile(t, fakeCloudsYAML())
	defer cleanup1()

	path2, cleanup2 := testutil.MustCreateFile(t, fakeCloudConfigUseClouds(path1))
	defer cleanup2()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	factory := mustNewCloudConfigFactory(t, ctx, path2)

	var cfg TestCloudConfig

	err := factory.GetConfig(&cfg)
	assert.Nil(t, err)

	assert.Equal(t, fakeUserName, cfg.Global.Username)
	assert.Equal(t, fakePassword, cfg.Global.Password)
	assert.Equal(t, fakeAuthURL, cfg.Global.AuthURL)
	assert.Equal(t, fakeTenantID, cfg.Global.TenantID)
	assert.Equal(t, fakeDomainID, cfg.Global.DomainID)
	assert.Equal(t, fakeCAfile, cfg.Global.CAFile)
	assert.Equal(t, fakeRegion, cfg.Global.Region)
	assert.Equal(t, false, cfg.BlockStorage.RescanOnResize)
	assert.Equal(t, true, cfg.Global.UseClouds)
	assert.Equal(t, path1, cfg.Global.CloudsFile)
	assert.Equal(t, fakeCloudName, cfg.Global.Cloud)
}

// TestConfigUpdate tests that polling the cloud config picks up
// changes to the underlying files.
func TestGetConfigUpdate(t *testing.T) {
	t.Parallel()

	path, cleanup := testutil.MustCreateFile(t, fakeCloudConfig())
	defer cleanup()

	// Wait for at least a refresh period to ensure the changes are picked up.
	ctx, cancel := context.WithTimeout(context.Background(), 2*refreshPeriod)
	defer cancel()

	factory := mustNewCloudConfigFactory(t, ctx, path)

	var cfg TestCloudConfig

	err := factory.GetConfig(&cfg)
	assert.Nil(t, err)

	// Just check the password, we've done the rest in TestGetConfig.
	assert.Equal(t, fakePassword, cfg.Global.Password)

	testutil.MustUpdateFile(t, path, fakeCloudConfigUpdate())

	<-ctx.Done()

	err = factory.GetConfig(&cfg)
	assert.Nil(t, err)

	assert.Equal(t, fakePasswordUpdate, cfg.Global.Password)
}

// TestProviderUpdate tests a provider created with the factory is updated
// when the cloud config updates and has its token rotated.
func TestProviderUpdate(t *testing.T) {
	t.Parallel()

	path, cleanup := testutil.MustCreateFile(t, fakeCloudConfig())
	defer cleanup()

	// Wait for at least a refresh period to ensure the changes are picked up.
	ctx, cancel := context.WithTimeout(context.Background(), 2*refreshPeriod)
	defer cancel()

	factory := mustNewCloudConfigFactory(t, ctx, path)

	var cfg TestCloudConfig

	provider, err := factory.Provider(&cfg, "user-agent")
	assert.Nil(t, err)

	token := provider.Token()
	assert.NotEmpty(t, token)

	testutil.MustUpdateFile(t, path, fakeCloudConfigUpdate())

	<-ctx.Done()

	newToken := provider.Token()
	assert.NotEmpty(t, newToken)
	assert.NotEqual(t, newToken, token)
}
