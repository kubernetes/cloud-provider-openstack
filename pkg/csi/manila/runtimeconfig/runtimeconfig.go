/*
Copyright 2020 The Kubernetes Authors.

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

package runtimeconfig

import (
	"encoding/json"
	"io/ioutil"
	"os"
)

var (
	// Path to the runtime config file
	RuntimeConfigFilename string
)

type RuntimeConfig struct {
	Nfs *NfsConfig `json:"nfs,omitempty"`
}

func Get() (*RuntimeConfig, error) {
	// File contents are deliberately not cached
	// as they may change over time.
	data, err := ioutil.ReadFile(RuntimeConfigFilename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	var cfg *RuntimeConfig
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
