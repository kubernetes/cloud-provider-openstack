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
			ProjectName: {"project1"},
			Roles:       {"role1"},
		},
	}
	user2 := &user.DefaultInfo{
		Name:   "user2",
		Groups: []string{"group2"},
		Extra: map[string][]string{
			ProjectName: {"project2"},
			Roles:       {"role2"},
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

	// Allow subresource with specific value, e.i. "user_resource1/subresource1"
	attrs = authorizer.AttributesRecord{
		User:            user1,
		ResourceRequest: true,
		Verb:            "get",
		Resource:        "user_resource1",
		Subresource:     "subresource1",
	}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	// Deny subresource with specific value, e.i. "user_resource1/subresource2", it must not be present in policy.json
	attrs = authorizer.AttributesRecord{
		User:            user1,
		ResourceRequest: true,
		Verb:            "get",
		Resource:        "user_resource1",
		Subresource:     "subresource2",
	}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Allow subresource with representing wildcard,  e.i. "user_resource3/wildcard_resource",
	// expected to be tested with policy where resources: ["user_resource3/*"]
	attrs = authorizer.AttributesRecord{
		User:            user1,
		ResourceRequest: true,
		Verb:            "get",
		Resource:        "user_resource3",
		Subresource:     "wildcard_resource",
	}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	// Allow nonresource endpoint with specific value, e.i. "/api"
	attrs = authorizer.AttributesRecord{
		User:            user1,
		ResourceRequest: false,
		Verb:            "get",
		Path:            "/api",
	}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	// Deny nonresource endpoint with specific value, e.i. "/imaginary_api/path/none", it must not be present in policy.json
	attrs = authorizer.AttributesRecord{
		User:            user1,
		ResourceRequest: false,
		Verb:            "get",
		Path:            "/imaginary_api/path/none",
	}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Allow nonresource endpoint with specific prefix, e.i. "/api/represents/wildcard",
	// expected to be tested with policy where path: ["/api/*"]
	attrs = authorizer.AttributesRecord{
		User:            user1,
		ResourceRequest: false,
		Verb:            "get",
		Path:            "/api/represents/wildcard",
	}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)
}

func TestAuthorizerVersion2(t *testing.T) {
	provider, err := openstack.NewClient("127.0.0.1")
	th.AssertNoErr(t, err)
	client := &gophercloud.ServiceClient{
		ProviderClient: provider,
		Endpoint:       "127.0.0.1",
	}

	path, err := os.Getwd()
	th.AssertNoErr(t, err)
	path += "/authorizer_test_policy_version2.json"
	policy, err := newFromFile(path)
	th.AssertNoErr(t, err)

	a := &Authorizer{authURL: "127.0.0.1", client: client, pl: policy}

	developer := &user.DefaultInfo{
		Name:   "developer",
		Groups: []string{"group1"},
		Extra: map[string][]string{
			ProjectName: {"demo"},
			Roles:       {"developer"},
		},
	}
	viewer := &user.DefaultInfo{
		Name:   "viewer",
		Groups: []string{"group2"},
		Extra: map[string][]string{
			ProjectName: {"demo"},
			ProjectID:   {"ff9db8980cf24a74bc9dd796b6ce811f"},
			Roles:       {"viewer"},
		},
	}
	anotherviewer := &user.DefaultInfo{
		Name:   "anotherviewer",
		Groups: []string{"group"},
		Extra: map[string][]string{
			ProjectName: {"alt_demo"},
			ProjectID:   {"cd08a539b7c845ddb92c5d08752101d1"},
			Roles:       {"viewer"},
		},
	}
	clusteradmin := &user.DefaultInfo{
		Name:   "clusteradmin",
		Groups: []string{"group2"},
		Extra: map[string][]string{
			ProjectName: {"demo"},
			Roles:       {"clusteradmin"},
		},
	}

	// Test developer
	attrs := authorizer.AttributesRecord{User: developer, ResourceRequest: true, Verb: "get", Namespace: "default", Resource: "pods"}
	decision, _, _ := a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: developer, ResourceRequest: true, Verb: "get", Namespace: "default", Resource: "clusterroles"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: developer, ResourceRequest: true, Verb: "get", Namespace: "", Resource: "clusterroles"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: developer, ResourceRequest: true, Verb: "create", Namespace: "", Resource: "clusterrolebindings"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Test developer, resource name should be case insensitive.
	attrs = authorizer.AttributesRecord{User: developer, ResourceRequest: true, Verb: "get", Namespace: "", Resource: "podsecuritypolicies"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	// Test viewer
	attrs = authorizer.AttributesRecord{User: viewer, ResourceRequest: true, Verb: "get", Namespace: "default", Resource: "deployments"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: viewer, ResourceRequest: true, Verb: "get", Namespace: "kube-system", Resource: "services"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: viewer, ResourceRequest: true, Verb: "create", Namespace: "default", Resource: "pods"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	// Test anotherviewer, the result should be the same with viewer
	attrs = authorizer.AttributesRecord{User: anotherviewer, ResourceRequest: true, Verb: "get", Namespace: "default", Resource: "deployments"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: anotherviewer, ResourceRequest: true, Verb: "get", Namespace: "kube-system", Resource: "services"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: anotherviewer, ResourceRequest: true, Verb: "create", Namespace: "default", Resource: "pods"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: clusteradmin, ResourceRequest: true, Verb: "create", Namespace: "", Resource: "clusterroles"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: clusteradmin, ResourceRequest: true, Verb: "get", Namespace: "kube-system", Resource: "secrets"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)

	attrs = authorizer.AttributesRecord{User: developer, ResourceRequest: false, Verb: "get", Path: "/healthz"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: viewer, ResourceRequest: false, Verb: "get", Path: "/healthz"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionDeny, decision)

	attrs = authorizer.AttributesRecord{User: clusteradmin, ResourceRequest: false, Verb: "get", Path: "/healthz"}
	decision, _, _ = a.Authorize(attrs)
	th.AssertEquals(t, authorizer.DecisionAllow, decision)
}
