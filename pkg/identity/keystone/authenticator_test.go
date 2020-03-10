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

package keystone

import (
	"testing"

	th "github.com/gophercloud/gophercloud/testhelper"
	"k8s.io/apiserver/pkg/authentication/user"
)

func TestAuthenticateToken(t *testing.T) {
	keystone := &MockIKeystone{}
	keystone.
		On("GetTokenInfo", "token").
		Return(&tokenInfo{
			userName:    "user-name",
			userID:      "user-id",
			projectID:   "project-id",
			projectName: "project-name",
			domainName:  "domain-name",
			domainID:    "domain-id",
			roles:       []string{"role1", "role2"},
		}, nil).
		Once()
	keystone.
		On("GetGroups", "token", "user-id").
		Return([]string{"group1", "group2"}, nil).
		Once()

	a := &Authenticator{
		keystoner: keystone,
	}
	userInfo, allowed, err := a.AuthenticateToken("token")

	th.AssertNoErr(t, err)
	th.AssertEquals(t, true, allowed)

	expectedUserInfo := &user.DefaultInfo{
		Name:   "user-name",
		UID:    "user-id",
		Groups: []string{"group1", "group2"},
		Extra: map[string][]string{
			Roles:       {"role1", "role2"},
			ProjectID:   {"project-id"},
			ProjectName: {"project-name"},
			DomainID:    {"domain-id"},
			DomainName:  {"domain-name"},
		},
	}
	th.AssertDeepEquals(t, expectedUserInfo, userInfo)

	keystone.AssertExpectations(t)
}
