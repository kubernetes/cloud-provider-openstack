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
	"sort"

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

func findString(a string, list []string) bool {
	sort.Strings(list)
	index := sort.SearchStrings(list, a)
	if index < len(list) && list[index] == a {
		return true
	}
	return false
}

func resourceMatches(p policy, a authorizer.Attributes) bool {
	if *p.ResourceSpec.APIGroup == "*" || *p.ResourceSpec.APIGroup == a.GetAPIGroup() {
		if *p.ResourceSpec.Namespace == "*" || *p.ResourceSpec.Namespace == a.GetNamespace() {
			if findString("*", p.ResourceSpec.Resources) || findString(a.GetResource(), p.ResourceSpec.Resources) {
				if findString("*", p.ResourceSpec.Verbs) || findString(a.GetVerb(), p.ResourceSpec.Verbs) {
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
	if findString("", p.NonResourceSpec.Verbs) {
		glog.Infof("verb is empty. skipping : %#v", p)
		return false
	}

	if p.NonResourceSpec.NonResourcePath == nil {
		glog.Infof("path should be set. skipping : %#v", p)
		return false
	}

	if findString("*", p.NonResourceSpec.Verbs) || findString(a.GetVerb(), p.NonResourceSpec.Verbs) {
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

func match(match []policyMatch, attributes authorizer.Attributes) bool {
	user := attributes.GetUser()
	var find = false
	types := []string{TypeGroup, TypeProject, TypeRole, TypeUser}

	for _, m := range match {
		if !findString(m.Type, types) {
			glog.Warningf("unknown type %s", m.Type)
			return false
		}
		if findString("*", m.Values) {
			continue
		}

		find = false

		if m.Type == TypeGroup {
			for _, group := range user.GetGroups() {
				if findString(group, m.Values) {
					find = true
					break
				}
			}
			if !find {
				return false
			}
		} else if m.Type == TypeUser {
			if !findString(user.GetName(), m.Values) && !findString(user.GetUID(), m.Values) {
				return false
			}
		} else if m.Type == TypeProject {
			if val, ok := user.GetExtra()["alpha.kubernetes.io/identity/project/id"]; ok {
				for _, item := range val {
					if findString(item, m.Values) {
						find = true
						break
					}
				}
				if find {
					continue
				}
			}

			if val, ok := user.GetExtra()["alpha.kubernetes.io/identity/project/name"]; ok {
				for _, item := range val {
					if findString(item, m.Values) {
						find = true
						break
					}
				}
				if find {
					continue
				}
			}
			return false
		} else if m.Type == TypeRole {
			if val, ok := user.GetExtra()["alpha.kubernetes.io/identity/roles"]; ok {
				for _, item := range val {
					if findString(item, m.Values) {
						find = true
						break
					}
				}
				if find {
					continue
				}
			}
			return false
		} else {
			glog.Infof("unknown type %s. skipping.", m.Type)
		}
	}

	return true
}

// Authorize checks whether the user can perform an operation
func (a *Authorizer) Authorize(attributes authorizer.Attributes) (authorized authorizer.Decision, reason string, err error) {
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

	glog.Warningf("Authorization failed, user: %#v, attributes: %#v\n", attributes.GetUser(), attributes)
	return authorizer.DecisionDeny, "No policy matched.", nil
}
