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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// OpenStackOptions contains fields used for authenticating to OpenStack
type OpenStackOptions struct {
	OSAuthURL     string `name:"os-authURL"`
	OSUserID      string `name:"os-userID"`
	OSUsername    string `name:"os-userName"`
	OSPassword    string `name:"os-password"`
	OSProjectID   string `name:"os-projectID"`
	OSProjectName string `name:"os-projectName"`
	OSDomainID    string `name:"os-domainID"`
	OSDomainName  string `name:"os-domainName"`
	OSRegionName  string `name:"os-region"`
}

// NewOpenStackOptions reads k8s secrets and constructs a new instance of OpenStackOptions
func NewOpenStackOptions(c clientset.Interface, secretRef *v1.SecretReference) (*OpenStackOptions, error) {
	o := &OpenStackOptions{}
	return o, buildOpenStackOptionsTo(c, o, secretRef)
}

func buildOpenStackOptionsTo(c clientset.Interface, o *OpenStackOptions, secretRef *v1.SecretReference) error {
	secrets, err := c.CoreV1().Secrets(secretRef.Namespace).Get(secretRef.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	secretsData := make(map[string]string)
	for k, v := range secrets.Data {
		secretsData[k] = string(v)
	}

	_, err = extractParams(&optionConstraints{allOptional: true}, secretsData, o)

	return err
}

// ToAuthOptions converts OpenStackOptions to gophercloud.AuthOptions
func (o *OpenStackOptions) ToAuthOptions() *gophercloud.AuthOptions {
	return &gophercloud.AuthOptions{
		IdentityEndpoint: o.OSAuthURL,
		UserID:           o.OSUserID,
		Username:         o.OSUsername,
		Password:         o.OSPassword,
		TenantID:         o.OSProjectID,
		TenantName:       o.OSProjectName,
		DomainID:         o.OSDomainID,
		DomainName:       o.OSDomainName,
	}
}
