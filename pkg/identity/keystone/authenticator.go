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
	"encoding/json"
	"errors"
	"io/ioutil"

	"github.com/gophercloud/gophercloud"
	"k8s.io/klog"

	"k8s.io/apiserver/pkg/authentication/user"
)

// Authenticator contacts openstack keystone to validate user's token passed in the request.
// The keystone endpoint is passed during apiserver startup
type Authenticator struct {
	authURL string
	client  *gophercloud.ServiceClient
}

type keystoneResponse struct {
	Token struct {
		User struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Domain struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"domain"`
		} `json:"user"`
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
		Roles []struct {
			Name string `json:"name"`
		} `json:"roles"`
	} `json:"token"`
}

// AuthenticateToken checks the token via Keystone call
func (a *Authenticator) AuthenticateToken(token string) (user.Info, bool, error) {
	// We can use the Keystone GET /v3/auth/tokens API to validate the token
	// and get information about the user as well
	// http://git.openstack.org/cgit/openstack/keystone/tree/api-ref/source/v3/authenticate-v3.inc#n437
	// https://developer.openstack.org/api-ref/identity/v3/?expanded=validate-and-show-information-for-token-detail
	requestOpts := gophercloud.RequestOpts{
		MoreHeaders: map[string]string{
			"X-Auth-Token":    token,
			"X-Subject-Token": token,
		},
	}
	url := a.client.ServiceURL("auth", "tokens")
	response, err := a.client.Request("GET", url, &requestOpts)
	if err != nil {
		klog.Warningf("Failed: bad response from API call: %v", err)
		return nil, false, errors.New("Failed to authenticate")
	}

	defer response.Body.Close()
	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		klog.Warningf("Cannot get HTTP response body from keystone token validate: %v", err)
		return nil, false, errors.New("Failed to authenticate")
	}

	var obj keystoneResponse

	err = json.Unmarshal(bodyBytes, &obj)
	if err != nil {
		klog.Warningf("Cannot unmarshal response: %v", err)
		return nil, false, errors.New("Failed to authenticate")
	}

	var roles []string
	if obj.Token.Roles != nil && len(obj.Token.Roles) > 0 {
		roles = make([]string, len(obj.Token.Roles))
		for i := 0; i < len(obj.Token.Roles); i++ {
			roles[i] = obj.Token.Roles[i].Name
		}
	} else {
		roles = make([]string, 0)
	}

	extra := map[string][]string{
		Roles:       roles,
		ProjectID:   {obj.Token.Project.ID},
		ProjectName: {obj.Token.Project.Name},
		DomainID:    {obj.Token.User.Domain.ID},
		DomainName:  {obj.Token.User.Domain.Name},
	}

	authenticatedUser := &user.DefaultInfo{
		Name:   obj.Token.User.Name,
		UID:    obj.Token.User.ID,
		Groups: []string{obj.Token.Project.ID},
		Extra:  extra,
	}

	return authenticatedUser, true, nil
}
