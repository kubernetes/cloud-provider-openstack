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
	"sync"

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"

	"k8s.io/api/core/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/kubernetes"
)

// Authenticator contacts openstack keystone to validate user's token passed in the request.
// The keystone endpoint is passed during apiserver startup
type Authenticator struct {
	authURL    string
	client     *gophercloud.ServiceClient
	k8sClient  *kubernetes.Clientset
	syncConfig *syncConfig
	mu         sync.Mutex
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

func (a *Authenticator) syncProjectData(obj *keystoneResponse) error {

	for _, p := range a.syncConfig.ProjectBlackList {
		if obj.Token.Project.ID == p {
			glog.Infof("Project %v is in black list. Skipping.")
			return nil
		}
	}

	if a.k8sClient == nil {
		return errors.New("cannot sync data because k8s client is not initialized")
	}

	namespaceName := a.syncConfig.formatNamespaceName(
		obj.Token.Project.ID,
		obj.Token.Project.Name,
		obj.Token.User.Domain.ID,
	)

	_, err := a.k8sClient.CoreV1().Namespaces().Get(namespaceName, metav1.GetOptions{})

	if k8s_errors.IsNotFound(err) {
		// The required namespace is not found. Create it then.
		namespace := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		namespace, err = a.k8sClient.CoreV1().Namespaces().Create(namespace)
		if err != nil {
			glog.Warningf("Cannot create a namespace for the user: %v", err)
			return errors.New("Internal server error")
		}
	} else if err != nil {
		// Some other error.
		glog.Warningf("Cannot get a response from the server: %v", err)
		return errors.New("Internal server error")
	}

	return nil
}

// AuthenticateToken checks the token via Keystone call
func (a *Authenticator) AuthenticateToken(token string) (user.Info, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

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
		glog.Warningf("Failed: bad response from API call: %v", err)
		return nil, false, errors.New("Failed to authenticate")
	}

	defer response.Body.Close()
	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		glog.Warningf("Cannot get HTTP response body from keystone token validate: %v", err)
		return nil, false, errors.New("Failed to authenticate")
	}

	var obj keystoneResponse

	err = json.Unmarshal(bodyBytes, &obj)
	if err != nil {
		glog.Warningf("Cannot unmarshal response: %v", err)
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
		"alpha.kubernetes.io/identity/roles":            roles,
		"alpha.kubernetes.io/identity/project/id":       {obj.Token.Project.ID},
		"alpha.kubernetes.io/identity/project/name":     {obj.Token.Project.Name},
		"alpha.kubernetes.io/identity/user/domain/id":   {obj.Token.User.Domain.ID},
		"alpha.kubernetes.io/identity/user/domain/name": {obj.Token.User.Domain.Name},
	}

	authenticatedUser := &user.DefaultInfo{
		Name:   obj.Token.User.Name,
		UID:    obj.Token.User.ID,
		Groups: []string{obj.Token.Project.ID},
		Extra:  extra,
	}

	if a.syncConfig != nil {
		err = a.syncProjectData(&obj)
		if err != nil {
			glog.Errorf("an error occurred during data synchronization: %v", err)
			return nil, false, err
		}
	}

	return authenticatedUser, true, nil
}
