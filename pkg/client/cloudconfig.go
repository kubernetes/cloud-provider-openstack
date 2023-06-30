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

package client

import (
	"context"
	"os"
	"time"

	"github.com/gophercloud/gophercloud"

	gcfg "gopkg.in/gcfg.v1"

	"k8s.io/cloud-provider-openstack/pkg/util/filewatcher"
	klog "k8s.io/klog/v2"
)

// CloudConfig allows individual controllers to define their own
// cloud config format, but also enforces basic compatibility with this.
// It works a bit like Kubernetes when your main types e.g. Pod etc. all
// have to implement runtime.Object etc.
type CloudConfig interface {
	AuthOpts() *AuthOpts
}

// CloudConfigFactoryOpts allows the behaviour of the cloud config factory
// to be modified.
type CloudConfigFactoryOpts struct {
	// Period allows the file polling period to be set.
	Period time.Duration
	// ProviderFunc allows a custom provider creation function
	// to be specified, otherwise it will default to NewOpenStackClient.
	ProviderFunc func(*AuthOpts, string, ...string) (*gophercloud.ProviderClient, error)
}

// CloudConfigFactory allows access to the cloud config and OpenStack
// providers in a canonical way.  The config files are polled periodically
// so it's similar to a Watch of a Secret, and the provided CloudConfig
// interface will be updated accordingly.
type CloudConfigFactory struct {
	// opts defines the factory's behaviour.
	opts CloudConfigFactoryOpts
	// watchers do the reading and caching of file contents.
	// we can subscribe to callbacks when the file contents
	// change.
	watchers []filewatcher.FileWatcher
}

// NewCloudConfigFactory creates a new factory initialized with a set of
// paths to cloud config files.  These files will be read in order and
// applied additively to the final configuration.
func NewCloudConfigFactory(ctx context.Context, opts CloudConfigFactoryOpts, configFilePaths ...string) (*CloudConfigFactory, error) {
	if opts.ProviderFunc == nil {
		opts.ProviderFunc = NewOpenStackClient
	}

	config := &CloudConfigFactory{
		opts:     opts,
		watchers: make([]filewatcher.FileWatcher, len(configFilePaths)),
	}

	for i, path := range configFilePaths {
		watcher, err := filewatcher.NewPollingFileWatcher(path, opts.Period)
		if err != nil {
			return nil, err
		}

		watcher.Run(ctx)

		config.watchers[i] = watcher
	}

	return config, nil
}

// GetConfig gets the current contents of each watched configuration file
// and applies it to the provided application-specific cloud configuration.
// Please note that this will return an immutable copy of the point-in-time config.
// A call to Provider() will provide on that's updated on underlying config
// file changes.
func (c *CloudConfigFactory) GetConfig(cfg CloudConfig) error {
	for _, watcher := range c.watchers {
		if err := gcfg.FatalOnly(gcfg.ReadInto(cfg, watcher.Contents())); err != nil {
			klog.Errorf("Failed to read cloud configuration file: %v", err)
			return err
		}
	}

	authOpts := cfg.AuthOpts()

	if authOpts.UseClouds {
		if authOpts.CloudsFile != "" {
			os.Setenv("OS_CLIENT_CONFIG_FILE", authOpts.CloudsFile)
		}

		// TODO: this needs watching in order to pick up credential rotation.
		if err := ReadClouds(authOpts); err != nil {
			return err
		}

		klog.V(5).Infof("Credentials are loaded from %s:", authOpts.CloudsFile)
	}

	return nil
}

// Provider returns a new OpenStack provider. The provider's token will be atomically
// updated on a configuration file change.
func (c *CloudConfigFactory) Provider(cfg CloudConfig, userAgent string, userAgentData ...string) (*gophercloud.ProviderClient, error) {
	if err := c.GetConfig(cfg); err != nil {
		return nil, err
	}

	provider, err := c.opts.ProviderFunc(cfg.AuthOpts(), userAgent, userAgentData...)
	if err != nil {
		return nil, err
	}

	// When a config file changes, apply any changes to the provider that we can e.g.
	// updating the application credential or password, facilitates credential rotation.
	// Note that we update into the original config file, so any other updates will
	// appear at runtime, so be aware of the difference between pass by value and pass
	// reference.
	configFileChanged := func([]byte) {
		if err := c.GetConfig(cfg); err != nil {
			klog.Errorf("Failed to reload cloud config: %v", err)
			return
		}

		newProvider, err := c.opts.ProviderFunc(cfg.AuthOpts(), userAgent, userAgentData...)
		if err != nil {
			klog.Errorf("Failed to reload openstack provider: %v", err)
			return
		}

		// TODO: sadly we we can only change the token, if for example
		// the endpoint changed, then those URLs are already cached in
		// the individual service clients.
		provider.CopyTokenFrom(newProvider)
	}

	// NOTE: at present this will overwrite previous callbacks registered for the
	// same user agent, but the good news is references to the provider will be freed
	// and can be garbage collected, thus preventing a memory leak.
	for _, watcher := range c.watchers {
		watcher.Subscribe("cloud-config-"+userAgent, configFileChanged)
	}

	return provider, nil
}
