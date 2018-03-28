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

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"

	"k8s.io/apiserver/pkg/authorization/authorizer"
)

// Authorizer contacts openstack keystone to check whether the user can perform
// requested operations.
// The keystone endpoint and policy list are passed during apiserver startup
type Authorizer struct {
	authURL string
	client  *gophercloud.ServiceClient
	pl      policyList
}

func resourceMatches(p policy, a authorizer.Attributes) bool {
	if p.ResourceSpec.Verb == "" {
		glog.Infof("verb is empty. skipping : %#v", p)
		return false
	}

	if p.ResourceSpec.APIGroup == nil || p.ResourceSpec.Namespace == nil || p.ResourceSpec.Resource == nil {
		glog.Infof("version/namespace/resource should be all set. skipping : %#v", p)
		return false
	}

	if p.ResourceSpec.Verb == "*" || p.ResourceSpec.Verb == a.GetVerb() {
		if *p.ResourceSpec.APIGroup == "*" || *p.ResourceSpec.APIGroup == a.GetAPIGroup() {
			if *p.ResourceSpec.Namespace == "*" || *p.ResourceSpec.Namespace == a.GetNamespace() {
				if *p.ResourceSpec.Resource == "*" || *p.ResourceSpec.Resource == a.GetResource() {
					allowed := match(p.Match, a)
					if allowed {
						output, err := json.MarshalIndent(p, "", "  ")
						if err == nil {
							glog.V(6).Infof("matched rule : %s", string(output))
						}
						return true
					}
				}
			}
		}
	}
	return false
}

func nonResourceMatches(p policy, a authorizer.Attributes) bool {
	if p.NonResourceSpec.Verb == "" {
		glog.Infof("verb is empty. skipping : %#v", p)
		return false
	}

	if p.NonResourceSpec.NonResourcePath == nil {
		glog.Infof("path should be set. skipping : %#v", p)
		return false
	}

	if p.NonResourceSpec.Verb == "*" || p.NonResourceSpec.Verb == a.GetVerb() {
		if *p.NonResourceSpec.NonResourcePath == "*" || *p.NonResourceSpec.NonResourcePath == a.GetPath() {
			allowed := match(p.Match, a)
			if allowed {
				output, err := json.MarshalIndent(p, "", "  ")
				if err == nil {
					glog.V(6).Infof("matched rule : %s", string(output))
				}
				return true
			}
		}
	}
	return false
}

func match(match policyMatch, attributes authorizer.Attributes) bool {
	user := attributes.GetUser()
	if match.Type == "group" {
		for _, group := range user.GetGroups() {
			if match.Value == "*" || group == match.Value {
				return true
			}
		}
	} else if match.Type == "user" {
		if match.Value == "*" || user.GetName() == match.Value || user.GetUID() == match.Value {
			return true
		}
	} else if match.Type == "project" {
		if val, ok := user.GetExtra()["alpha.kubernetes.io/identity/project/id"]; ok {
			if ok {
				for _, item := range val {
					if match.Value == "*" || item == match.Value {
						return true
					}
				}
			}
		}
		if val, ok := user.GetExtra()["alpha.kubernetes.io/identity/project/name"]; ok {
			if ok {
				for _, item := range val {
					if match.Value == "*" || item == match.Value {
						return true
					}
				}
			}
		}
	} else if match.Type == "role" {
		if val, ok := user.GetExtra()["alpha.kubernetes.io/identity/roles"]; ok {
			if ok {
				for _, item := range val {
					if match.Value == "*" || item == match.Value {
						return true
					}
				}

			}
		}
	} else {
		glog.Infof("unknown type %s. skipping.", match.Type)
	}
	return false
}

// Authorize checks whether the user can perform an operation
func (a *Authorizer) Authorize(attributes authorizer.Attributes) (authorized authorizer.Decision, reason string, err error) {
	glog.Infof("Authorizing user : %#v\n", attributes.GetUser())
	for _, p := range a.pl {
		if p.NonResourceSpec != nil && p.ResourceSpec != nil {
			glog.Infof("Policy has both resource and nonresource sections. skipping : %#v", p)
			continue
		}
		if p.ResourceSpec != nil {
			if resourceMatches(*p, attributes) {
				return authorizer.DecisionAllow, "", nil
			}
		} else if p.NonResourceSpec != nil {
			if nonResourceMatches(*p, attributes) {
				return authorizer.DecisionAllow, "", nil
			}
		}
	}
	return authorizer.DecisionDeny, "No policy matched.", nil
}
