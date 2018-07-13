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

package shareoptions

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	"k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// OpenStackOptions contains fields used for authenticating to OpenStack
type OpenStackOptions struct {
	// Mandatory options

	OSAuthURL    string `name:"os-authURL" value:"alwaysRequires=os-password|os-trustID"`
	OSRegionName string `name:"os-region"`

	// User authentication

	OSPassword    string `name:"os-password" value:"requires=os-domainID|os-domainName,os-projectID|os-projectName,os-userID|os-userName"`
	OSUserID      string `name:"os-userID" value:"requires=os-password"`
	OSUsername    string `name:"os-userName" value:"requires=os-password"`
	OSDomainID    string `name:"os-domainID" value:"requires=os-password"`
	OSDomainName  string `name:"os-domainName" value:"requires=os-password"`
	OSProjectID   string `name:"os-projectID" value:"requires=os-password"`
	OSProjectName string `name:"os-projectName" value:"requires=os-password"`

	// Trustee authentication

	OSTrustID         string `name:"os-trustID" value:"requires=os-trusteeID,os-trusteePassword"`
	OSTrusteeID       string `name:"os-trusteeID" value:"requires=os-trustID"`
	OSTrusteePassword string `name:"os-trusteePassword" value:"requires=os-trustID"`
	// TODO:
	// OSCertAuthority   string `name:"os-certAuthority" value:"requires=os-trustID"`
}

// NewOpenStackOptionsFromSecret reads k8s secrets, validates and populates OpenStackOptions
func NewOpenStackOptionsFromSecret(c clientset.Interface, secretRef *v1.SecretReference) (*OpenStackOptions, error) {
	params, err := readSecrets(c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewOpenStackOptionsFromMap(params)
}

// NewOpenStackOptionsFromMap validates and populates OpenStackOptions
func NewOpenStackOptionsFromMap(params map[string]string) (*OpenStackOptions, error) {
	opts := &OpenStackOptions{}
	return opts, buildOpenStackOptionsTo(opts, params)
}

func buildOpenStackOptionsTo(opts *OpenStackOptions, params map[string]string) error {
	_, err := extractParams(&optionConstraints{}, params, opts)
	return err
}

// ToAuthOptions converts OpenStackOptions to gophercloud.AuthOptions
func (o *OpenStackOptions) ToAuthOptions() *gophercloud.AuthOptions {
	authOpts := &gophercloud.AuthOptions{
		IdentityEndpoint: o.OSAuthURL,
		UserID:           o.OSUserID,
		Username:         o.OSUsername,
		Password:         o.OSPassword,
		TenantID:         o.OSProjectID,
		TenantName:       o.OSProjectName,
		DomainID:         o.OSDomainID,
		DomainName:       o.OSDomainName,
	}

	if o.OSTrustID != "" {
		// gophercloud doesn't have dedicated options for trustee credentials
		authOpts.UserID = o.OSTrusteeID
		authOpts.Password = o.OSTrusteePassword
	}

	return authOpts
}

// ToAuthOptionsExt converts OpenStackOptions to trusts.AuthOptsExt
func (o *OpenStackOptions) ToAuthOptionsExt() *trusts.AuthOptsExt {
	return &trusts.AuthOptsExt{
		AuthOptionsBuilder: o.ToAuthOptions(),
		TrustID:            o.OSTrustID,
	}
}
