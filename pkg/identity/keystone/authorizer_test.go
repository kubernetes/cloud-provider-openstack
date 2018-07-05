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
	"os"
	"testing"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	th "github.com/gophercloud/gophercloud/testhelper"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func TestAuthorizer(t *testing.T) {
	provider, err := openstack.NewClient("127.0.0.1")
	th.AssertNoErr(t, err)
	client := &gophercloud.ServiceClient{
		ProviderClient: provider,
		Endpoint:       "127.0.0.1",
	}

	// Read policies from an example file
	path, err := os.Getwd()
	th.AssertNoErr(t, err)
	path += "/authorizer_test_policy.json"
	policy, err := newFromFile(path)
	th.AssertNoErr(t, err)

	a := &Authorizer{authURL: "127.0.0.1", client: client, pl: policy}

	// Create two users with different permissions
	user1 := &user.DefaultInfo{
		Name:   "user1",
		Groups: []string{"group1"},
		Extra: map[string][]string{
			"alpha.kubernetes.io/identity/project/name": {"project1"},
			"alpha.kubernetes.io/identity/roles":        {"role1"},
		},
	}
	user2 := &user.DefaultInfo{
		Name:   "user2",
		Groups: []string{"group2"},
		Extra: map[string][]string{
			"alpha.kubernetes.io/identity/project/name": {"project2"},
			"alpha.kubernetes.io/identity/roles":        {"role2"},
		},
	}

	// TODO(Fedosin): The tests below cover all main cases. Nevertheless, the list can (and should) be expanded
	// with additional tests for other scenarios.

	// Resource user match
	attrs := authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "user_resource1"}
	decision, _, _ := a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "list", Resource: "user_resource2"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "patch", Resource: "user_resource1"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: true, Verb: "get", Resource: "user_resource1"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Resource group match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "group_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "patch", Resource: "group_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: true, Verb: "get", Resource: "group_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Resource group and role match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "group_role_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Resource project match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "project_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "patch", Resource: "project_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: true, Verb: "get", Resource: "project_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Resource role match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "role_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "patch", Resource: "role_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: true, Verb: "get", Resource: "role_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Core api group resource match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "core_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "core_resource", APIGroup: "NonCoreAPIGroup"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Nonresource user match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "get", Path: "/user"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "patch", Path: "/user"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: false, Verb: "get", Path: "/user"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Nonresource group match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "get", Path: "/group"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "patch", Path: "/group"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: false, Verb: "get", Path: "/group"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Nonresource project match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "get", Path: "/project"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "patch", Path: "/project"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: false, Verb: "get", Path: "/project"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Nonresource role match
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "get", Path: "/role"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: false, Verb: "patch", Path: "/role"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: user2, ResourceRequest: false, Verb: "get", Path: "/role"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Unknown match type
	attrs = authorizer.AttributesRecord{User: user1, ResourceRequest: true, Verb: "get", Resource: "unknown_type_resource"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)
}
