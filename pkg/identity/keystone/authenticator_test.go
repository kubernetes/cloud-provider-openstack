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
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	th "github.com/gophercloud/gophercloud/testhelper"
)

func TestAuthenticateToken(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Auth-Token")

		if token == "GoodToken" {
			w.WriteHeader(http.StatusOK)
			resp := `{
				"token": {
					"methods": [
						"token"
					],
					"expires_at": "2015-11-05T22:00:11.000000Z",
					"user": {
						"domain": {
							"id": "default",
							"name": "Default"
						},
						"id": "10a2e6e717a245d9acad3e5f97aeca3d",
						"name": "admin",
						"password_expires_at": null
					},
					"project": {
						"id": "74a4e7d5f4e24a4c9cd01b8deec4bee5",
						"name": "the_project"
					},
					"roles": [
						{
							"id": "51cc68287d524c759f47c811e6463340",
							"name": "admin"
						},
						{
							"id": "5af76e3aec294349929db2a0a27d3192",
							"name": "developer"
						}
					],
					"audit_ids": [
						"mAjXQhiYRyKwkB4qygdLVg"
					],
					"issued_at": "2015-11-05T21:00:33.819948Z"
				}
			}`
			fmt.Fprintf(w, resp)

		} else if token == "WrongToken" {
			w.WriteHeader(http.StatusUnauthorized)
			resp := `{  
				"error":{  
				   "message":"Unauthorized.",
				   "code":401,
				   "title":"Unauthorized"
				}
			 }`
			fmt.Fprintf(w, resp)

		} else if token == "NoBody" {
			w.WriteHeader(http.StatusOK)

		} else if token == "MalformedBody" {
			w.WriteHeader(http.StatusOK)
			resp := "NotJSON"
			fmt.Fprintf(w, resp)
		}
	})

	provider, _ := openstack.NewClient(th.Endpoint())
	cli := &gophercloud.ServiceClient{
		ProviderClient: provider,
		Endpoint:       th.Endpoint(),
	}

	a := &Authenticator{authURL: th.Endpoint(), client: cli}

	user, ok, err := a.AuthenticateToken("GoodToken")
	th.AssertEquals(t, "admin", user.GetName())
	th.AssertEquals(t, "10a2e6e717a245d9acad3e5f97aeca3d", user.GetUID())
	th.AssertNoErr(t, err)
	th.CheckEquals(t, ok, true)

	th.AssertEquals(t, "74a4e7d5f4e24a4c9cd01b8deec4bee5", user.GetExtra()["alpha.kubernetes.io/identity/project/id"][0])
	th.AssertEquals(t, "the_project", user.GetExtra()["alpha.kubernetes.io/identity/project/name"][0])
	th.AssertEquals(t, "default", user.GetExtra()["alpha.kubernetes.io/identity/user/domain/id"][0])
	th.AssertEquals(t, "Default", user.GetExtra()["alpha.kubernetes.io/identity/user/domain/name"][0])
	th.AssertEquals(t, "admin", user.GetExtra()["alpha.kubernetes.io/identity/roles"][0])
	th.AssertEquals(t, "developer", user.GetExtra()["alpha.kubernetes.io/identity/roles"][1])

	_, ok, err = a.AuthenticateToken("WrongToken")
	th.AssertEquals(t, (err != nil), true)
	th.CheckEquals(t, ok, false)

	_, ok, err = a.AuthenticateToken("NoBody")
	th.AssertEquals(t, (err != nil), true)
	th.CheckEquals(t, ok, false)

	_, ok, err = a.AuthenticateToken("MalformedBody")
	th.AssertEquals(t, (err != nil), true)
	th.CheckEquals(t, ok, false)
}
