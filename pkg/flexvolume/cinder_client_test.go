/*
Copyright 2014 The Kubernetes Authors.

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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFlexVolumeConfig(t *testing.T) {

	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		t.Error(err)
	}

	cfgFile := dir + "/test_config"
	_ = os.Remove(cfgFile)

	var config = `
 [Global]
 auth-url = http://auth.url
 password = mypass
 user-id = user
 tenant-name = demo
 region = RegionOne
 [RBD]
 keyring = mykeyring`
	data := []byte(config)
	err = ioutil.WriteFile(cfgFile, data, 0644)
	if err != nil {
		t.Error(err)
	}
	defer os.Remove(cfgFile)

	cfg, err := readConfig(cfgFile)
	if err != nil {
		t.Fatalf("Should succeed when a valid config is provided: %s", err)
	}
	if cfg.Global.AuthURL != "http://auth.url" {
		t.Errorf("incorrect IdentityEndpoint: %s", cfg.Global.AuthURL)
	}

	if cfg.Global.UserID != "user" {
		t.Errorf("incorrect userid: %s", cfg.Global.UserID)
	}

	if cfg.Global.Password != "mypass" {
		t.Errorf("incorrect password: %s", cfg.Global.Password)
	}

	// config file wins over environment variable
	if cfg.Global.TenantName != "demo" {
		t.Errorf("incorrect tenant name: %s", cfg.Global.TenantName)
	}

	if cfg.Global.Region != "RegionOne" {
		t.Errorf("incorrect region: %s", cfg.Global.Region)
	}
}
