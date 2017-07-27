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

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"

	"k8s.io/apiserver/pkg/authentication/user"
)

// KeystoneAuthenticator contacts openstack keystone to validate user's token passed in the request.
// The keystone endpoint is passed during apiserver startup
type KeystoneAuthenticator struct {
	authURL string
	client  *gophercloud.ServiceClient
}

// AuthenticatePassword checks the token via Keystone call
func (keystoneAuthenticator *KeystoneAuthenticator) AuthenticateToken(token string) (user.Info, bool, error) {

	// We can use the Keystone GET /v3/auth/tokens API to validate the token
	// and get information about the user as well
	// http://git.openstack.org/cgit/openstack/keystone/tree/api-ref/source/v3/authenticate-v3.inc#n437
	// https://developer.openstack.org/api-ref/identity/v3/?expanded=validate-and-show-information-for-token-detail
	request_opts := gophercloud.RequestOpts{
		MoreHeaders: map[string]string{
			"X-Auth-Token":    token,
			"X-Subject-Token": token,
		},
	}
	url := keystoneAuthenticator.client.ServiceURL("auth", "tokens")
	response, err := keystoneAuthenticator.client.Request("GET", url, &request_opts)
	if err != nil {
		glog.V(4).Info("Failed: bad response from API call: %v", err)
		return nil, false, errors.New("Failed to authenticate")
	}

	defer response.Body.Close()
	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		glog.V(4).Infof("Cannot get HTTP response body from keystone token validate: %v", err)
		return nil, false, errors.New("Failed to authenticate")
	}

	obj := struct {
		Token struct {
			User struct {
				Id   string `json:"id"`
				Name string `json:"name"`
			} `json:"user"`
			Project struct {
				Id   string `json:"id"`
				Name string `json:"name"`
			} `json:"project"`
			Roles []struct {
				Name string `json:"name"`
			} `json:"roles"`
		} `json:"token"`
	}{}

	err = json.Unmarshal(bodyBytes, &obj)
	if err != nil {
		glog.V(4).Infof("Cannot unmarshal response: %v", err)
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
		"alpha.kubernetes.io/identity/roles":        roles,
		"alpha.kubernetes.io/identity/project/id":   []string{obj.Token.Project.Id},
		"alpha.kubernetes.io/identity/project/name": []string{obj.Token.Project.Name},
	}

	authenticated_user := &user.DefaultInfo{
		Name:   obj.Token.User.Name,
		UID:    obj.Token.User.Id,
		Groups: []string{obj.Token.Project.Id},
		Extra:  extra,
	}

	return authenticated_user, true, nil
}
