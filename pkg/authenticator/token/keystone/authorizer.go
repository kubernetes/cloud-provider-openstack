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
	"log"
	"github.com/gophercloud/gophercloud"

	"k8s.io/apiserver/pkg/authorization/authorizer"
	"encoding/json"
)

type KeystoneAuthorizer struct {
	authURL string
	client  *gophercloud.ServiceClient
	pl      policyList
}

func resourceMatches(p Policy, a authorizer.Attributes) bool {
	if p.NonResourceSpec != nil && p.ResourceSpec != nil {
		log.Printf("Policy has both resource and nonresource sections. skipping : %#v", p)
		return false
	}

	if p.ResourceSpec.Verb == "" {
		log.Printf("verb is empty. skipping : %#v", p)
		return false
	}

	if p.ResourceSpec.APIGroup == nil || p.ResourceSpec.Namespace == nil || p.ResourceSpec.Resource == nil {
		log.Printf("version/namespace/resource should be all set. skipping : %#v", p)
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
							log.Printf(">>>> matched rule : %s", string(output))
						}
						return true
					}
				}
			}
		}
	}
	return false
}

func nonResourceMatches(p Policy, a authorizer.Attributes) bool {
	if p.NonResourceSpec.Verb == "" {
		log.Printf("verb is empty. skipping : %#v", p)
		return false
	}

	if p.NonResourceSpec.NonResourcePath == nil {
		log.Printf("path should be set. skipping : %#v", p)
		return false
	}

	if p.NonResourceSpec.Verb == "*" || p.NonResourceSpec.Verb == a.GetVerb() {
		if *p.NonResourceSpec.NonResourcePath == "*" || *p.NonResourceSpec.NonResourcePath == a.GetPath() {
			if *p.ResourceSpec.Resource == "*" || *p.ResourceSpec.Resource == a.GetResource() {
				allowed := match(p.Match, a)
				if allowed {
					output, err := json.MarshalIndent(p, "", "  ")
					if err == nil {
						log.Printf(">>>> matched rule : %s", string(output))
					}
					return true
				}
			}
		}
	}
	return false
}

func match(match Match, attributes authorizer.Attributes) bool {
	user := attributes.GetUser()
	if match.Type == "group" {
		for _, group  := range user.GetGroups() {
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
		log.Printf("unknown type %s. skipping.", match.Type)
	}
	return false
}

func (KeystoneAuthorizer *KeystoneAuthorizer) Authorize(a authorizer.Attributes) (authorized bool, reason string, err error) {
	log.Printf("Authorizing user : %#v\n", a.GetUser())
	for _, p := range KeystoneAuthorizer.pl {
		if p.NonResourceSpec != nil && p.ResourceSpec != nil {
			log.Printf("Policy has both resource and nonresource sections. skipping : %#v", p)
			continue
		}
		if p.ResourceSpec != nil {
			if resourceMatches(*p, a) {
				return true, "", nil
			}
		} else if p.NonResourceSpec != nil {
			if nonResourceMatches(*p, a) {
				return true, "", nil
			}
		}
	}
	return false, "No policy matched.", nil
}
