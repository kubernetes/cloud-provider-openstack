/*
Copyright 2019 The Kubernetes Authors.

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

package options

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/cloud-provider-openstack/pkg/client"
)

func TestNewOpenstackOptionsWithCloudConfig(t *testing.T) {
	tests := []struct {
		name        string
		secretData  map[string]string
		expectError bool
		expected    *client.AuthOpts
	}{
		{
			name: "valid cloud.conf format",
			secretData: map[string]string{
				"cloud.conf": `[Global]
auth-url = https://keystone.example.com:5000/v3
username = manila-user
password = secret-password
tenant-name = manila-project
domain-name = default
region = RegionOne`,
			},
			expectError: false,
			expected: &client.AuthOpts{
				AuthURL:    "https://keystone.example.com:5000/v3",
				Username:   "manila-user",
				Password:   "secret-password",
				TenantName: "manila-project",
				DomainName: "default",
				Region:     "RegionOne",
			},
		},
		{
			name: "empty cloud.conf",
			secretData: map[string]string{
				"cloud.conf": "",
			},
			expectError: true,
		},
		{
			name: "invalid cloud.conf format",
			secretData: map[string]string{
				"cloud.conf": "invalid ini format [",
			},
			expectError: true,
		},
		{
			name: "cloud.conf with minimal required fields",
			secretData: map[string]string{
				"cloud.conf": `[Global]
auth-url = https://keystone.example.com:5000/v3
username = user
password = pass
tenant-name = project`,
			},
			expectError: false,
			expected: &client.AuthOpts{
				AuthURL:    "https://keystone.example.com:5000/v3",
				Username:   "user",
				Password:   "pass",
				TenantName: "project",
			},
		},
		{
			name: "cloud.conf takes precedence over individual keys",
			secretData: map[string]string{
				"cloud.conf": `[Global]
auth-url = https://cloud-config.example.com:5000/v3
username = cloud-config-user
password = cloud-config-password
tenant-name = cloud-config-project`,
				"os-authURL":     "https://individual.example.com:5000/v3",
				"os-userName":    "individual-user",
				"os-password":    "individual-password",
				"os-projectName": "individual-project",
			},
			expectError: false,
			expected: &client.AuthOpts{
				AuthURL:    "https://cloud-config.example.com:5000/v3",
				Username:   "cloud-config-user",
				Password:   "cloud-config-password",
				TenantName: "cloud-config-project",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewOpenstackOptions(tt.secretData)

			if tt.expectError {
				assert.Error(t, err, "expected error but got none")
				return
			}

			assert.NoError(t, err, "unexpected error")
			assert.NotNil(t, result, "expected result but got nil")

			if tt.expected != nil {
				assert.Equal(t, tt.expected, result, "AuthOpts struct mismatch")
			}
		})
	}
}

func TestNewOpenstackOptionsWithIndividualKeys(t *testing.T) {
	tests := []struct {
		name        string
		secretData  map[string]string
		expectError bool
		expected    *client.AuthOpts
	}{
		{
			name: "valid individual keys format",
			secretData: map[string]string{
				"os-authURL":     "https://keystone.example.com:5000/v3",
				"os-userName":    "manila-user",
				"os-password":    "secret-password",
				"os-projectName": "manila-project",
				"os-domainID":    "default-domain-id",
				"os-region":      "RegionOne",
			},
			expectError: false,
			expected: &client.AuthOpts{
				AuthURL:    "https://keystone.example.com:5000/v3",
				Username:   "manila-user",
				Password:   "secret-password",
				TenantName: "manila-project",
				DomainID:   "default-domain-id",
				Region:     "RegionOne",
			},
		},
		{
			name: "minimal individual keys",
			secretData: map[string]string{
				"os-authURL":     "https://keystone.example.com:5000/v3",
				"os-userName":    "user",
				"os-password":    "pass",
				"os-projectName": "project",
				"os-domainID":    "domain-id",
			},
			expectError: false,
			expected: &client.AuthOpts{
				AuthURL:    "https://keystone.example.com:5000/v3",
				Username:   "user",
				Password:   "pass",
				TenantName: "project",
				DomainID:   "domain-id",
			},
		},
		{
			name: "trustee authentication",
			secretData: map[string]string{
				"os-authURL":         "https://keystone.example.com:5000/v3",
				"os-trustID":         "trust-id-123",
				"os-trusteeID":       "trustee-id-456",
				"os-trusteePassword": "trustee-password",
				"os-region":          "RegionOne",
			},
			expectError: false,
			expected: &client.AuthOpts{
				AuthURL:         "https://keystone.example.com:5000/v3",
				TrustID:         "trust-id-123",
				TrusteeID:       "trustee-id-456",
				TrusteePassword: "trustee-password",
				Region:          "RegionOne",
			},
		},
		{
			name:        "empty secret data",
			secretData:  map[string]string{},
			expectError: false, // Validator allows empty data, returns empty AuthOpts
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewOpenstackOptions(tt.secretData)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)

			if tt.expected != nil {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
