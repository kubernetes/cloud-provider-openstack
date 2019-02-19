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
	"reflect"
	"testing"

	"github.com/gophercloud/gophercloud"
)

func TestOpenStackOptionsToAuthOptions(t *testing.T) {
	osOptions := OpenStackOptions{
		OSAuthURL:     "OSAuthURL",
		OSUserID:      "OSUserID",
		OSUsername:    "OSUsername",
		OSPassword:    "OSPassword",
		OSProjectID:   "OSProjectID",
		OSProjectName: "OSProjectName",
		OSDomainID:    "OSDomainID",
		OSDomainName:  "OSDomainName",
	}

	authOptions := osOptions.ToAuthOptions()

	eq := reflect.DeepEqual(authOptions, &gophercloud.AuthOptions{
		IdentityEndpoint: "OSAuthURL",
		UserID:           "OSUserID",
		Username:         "OSUsername",
		Password:         "OSPassword",
		TenantID:         "OSProjectID",
		TenantName:       "OSProjectName",
		DomainID:         "OSDomainID",
		DomainName:       "OSDomainName",
	})

	if !eq {
		t.Error("bad conversion from OpenStackOptions to gophercloud.AuthOptions")
	}
}
