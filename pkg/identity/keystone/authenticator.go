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

package keystone

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/users"
	"k8s.io/apiserver/pkg/authentication/user"
)

type tokenInfo struct {
	userName    string
	userID      string
	roles       []string
	projectName string
	projectID   string
	domainName  string
	domainID    string
}

type IKeystone interface {
	GetTokenInfo(context.Context, string) (*tokenInfo, error)
	GetGroups(context.Context, string, string) ([]string, error)
}

type Keystoner struct {
	client *gophercloud.ServiceClient
}

func NewKeystoner(client *gophercloud.ServiceClient) *Keystoner {
	return &Keystoner{
		client: client,
	}
}

// revive:disable:unexported-return
func (k *Keystoner) GetTokenInfo(ctx context.Context, token string) (*tokenInfo, error) {
	k.client.SetToken(token)
	ret := tokens.Get(ctx, k.client, token)

	tokenUser, err := ret.ExtractUser()
	if err != nil {
		return nil, fmt.Errorf("failed to extract user information from Keystone response: %v", err)
	}

	project, err := ret.ExtractProject()
	if err != nil {
		return nil, fmt.Errorf("failed to extract project information from Keystone response: %v", err)
	}
	if project == nil {
		return nil, fmt.Errorf("failed to extract project information from Keystone response")
	}

	roles, err := ret.ExtractRoles()
	if err != nil {
		return nil, fmt.Errorf("failed to extract roles information from Keystone response: %v", err)
	}

	userRoles := make([]string, 0, len(roles))
	for _, role := range roles {
		userRoles = append(userRoles, role.Name)
	}

	return &tokenInfo{
		userName:    tokenUser.Name,
		userID:      tokenUser.ID,
		projectName: project.Name,
		projectID:   project.ID,
		roles:       userRoles,
		domainID:    tokenUser.Domain.ID,
		domainName:  tokenUser.Domain.Name,
	}, nil
}

// revive:enable:unexported-return

func (k *Keystoner) GetGroups(ctx context.Context, token string, userID string) ([]string, error) {
	k.client.SetToken(token)
	allGroupPages, err := users.ListGroups(k.client, userID).AllPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups from Keystone: %v", err)
	}

	allGroups, err := groups.ExtractGroups(allGroupPages)
	if err != nil {
		return nil, fmt.Errorf("failed to extract user groups from Keystone response: %v", err)
	}

	userGroups := make([]string, 0, len(allGroups))
	for _, g := range allGroups {
		userGroups = append(userGroups, g.Name)
	}

	return userGroups, nil
}

// Authenticator contacts openstack keystone to validate user's token passed in the request.
type Authenticator struct {
	keystoner IKeystone
}

// AuthenticateToken checks the token via Keystone call
func (a *Authenticator) AuthenticateToken(ctx context.Context, token string) (user.Info, bool, error) {
	tokenInfo, err := a.keystoner.GetTokenInfo(ctx, token)
	if err != nil {
		return nil, false, fmt.Errorf("failed to authenticate: %v", err)
	}

	userGroups, err := a.keystoner.GetGroups(ctx, token, tokenInfo.userID)
	if err != nil {
		return nil, false, fmt.Errorf("failed to authenticate: %v", err)
	}

	extra := map[string][]string{
		Roles:       tokenInfo.roles,
		ProjectID:   {tokenInfo.projectID},
		ProjectName: {tokenInfo.projectName},
		DomainID:    {tokenInfo.domainID},
		DomainName:  {tokenInfo.domainName},
	}

	userGroups = append(userGroups, tokenInfo.projectID)
	authenticatedUser := &user.DefaultInfo{
		Name:   tokenInfo.userName,
		UID:    tokenInfo.userID,
		Groups: userGroups,
		Extra:  extra,
	}

	return authenticatedUser, true, nil
}
