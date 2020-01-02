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

package flexvolume

import (
	"fmt"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/volumeactions"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v2/volumes"
	gcfg "gopkg.in/gcfg.v1"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
	"k8s.io/klog"
)

type cinderClient struct {
	cinder  *gophercloud.ServiceClient
	keyring string
}

type openStackConfig struct {
	openstack_provider.Config
	RBD struct {
		Keyring string `gcfg:"keyring"`
	}
}

func readConfig(configFile string) (openStackConfig, error) {
	var config *os.File
	config, err := os.Open(configFile)
	if err != nil {
		return openStackConfig{}, err
	}
	defer config.Close()

	if config == nil {
		err := fmt.Errorf("no OpenStack cloud provider config file given")
		return openStackConfig{}, err
	}

	var cfg openStackConfig
	err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	return cfg, err
}

func newCinderClient(configFile string) (*cinderClient, error) {
	cfg, err := readConfig(configFile)
	if err != nil {
		return nil, err
	}

	provider, err := openstack_provider.NewOpenStackClient(&cfg.Config.Global, "cinder-flex-volume-driver")
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewBlockStorageV2(provider, gophercloud.EndpointOpts{
		Region: cfg.Global.Region,
	})
	if err != nil {
		return nil, err
	}

	cc := cinderClient{
		cinder:  client,
		keyring: cfg.RBD.Keyring,
	}

	return &cc, nil
}

// Get cinder volume info by volumeID
func (client *cinderClient) getVolume(volumeID string) (*volumes.Volume, error) {
	volume, err := volumes.Get(client.cinder, volumeID).Extract()
	if err != nil {
		return nil, err
	}

	return volume, nil
}

func (client *cinderClient) getConnectionInfo(id string, copts *volumeactions.InitializeConnectionOpts) (map[string]interface{}, error) {
	connectionInfo, err := volumeactions.InitializeConnection(client.cinder, id, copts).Extract()
	if err != nil && err.Error() != "EOF" {
		return nil, err
	}

	return connectionInfo, nil
}

func (client *cinderClient) attach(id string, opts volumeactions.AttachOpts) error {
	attachResult := volumeactions.Attach(client.cinder, id, opts)
	if attachResult.Err != nil && attachResult.Err.Error() != "EOF" {
		return attachResult.Err
	}

	return nil
}

func (client *cinderClient) terminateConnection(id string, copts *volumeactions.TerminateConnectionOpts) error {
	terminateResult := volumeactions.TerminateConnection(client.cinder, id, copts)
	if terminateResult.Err != nil && terminateResult.Err.Error() != "EOF" {
		klog.Warningf("Terminate cinder volume %s failed: %v", id, terminateResult.Err)
	}

	return nil
}

func (client *cinderClient) detach(id string) error {
	detachOpts := volumeactions.DetachOpts{}
	detachResult := volumeactions.Detach(client.cinder, id, detachOpts)
	if detachResult.Err != nil && detachResult.Err.Error() != "EOF" {
		klog.Warningf("Detach cinder volume %s failed: %v", id, detachResult.Err)
		return detachResult.Err
	}

	return nil
}
