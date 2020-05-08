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
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gophercloud/gophercloud"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"k8s.io/apiserver/pkg/authorization/authorizer"
)

// Authorizer contacts openstack keystone to check whether the user can perform
// requested operations.
// The keystone endpoint and policy list are passed during apiserver startup
type Authorizer struct {
	authURL string
	client  *gophercloud.ServiceClient
	pl      policyList
	mu      sync.Mutex
}

func findString(a string, list []string) bool {
	sort.Strings(list)
	index := sort.SearchStrings(list, a)
	if index < len(list) && list[index] == a {
		return true
	}
	return false
}

// getAllowed gets the allowed resources based on the definition.
func getAllowed(definition string, str string) (sets.String, error) {
	allowed := sets.NewString()

	if definition == str || definition == "*" || str == "" {
		allowed.Insert(str)
	} else if strings.Index(definition, "!") == 0 && strings.Index(definition, "[") != 1 {
		// "!namespace"
		if definition[1:] == str || definition[1:] == "*" {
			return nil, fmt.Errorf("")
		}
		allowed.Insert(str)
	} else if strings.Index(definition, "[") == 0 && strings.Index(definition, "]") == (len(definition)-1) {
		// "['namespace1', 'namespace2']"
		var items []string
		if err := json.Unmarshal([]byte(strings.Replace(definition, "'", "\"", -1)), &items); err != nil {
			klog.V(4).Infof("Skip the permission definition %s", definition)
			return nil, fmt.Errorf("")
		}
		for _, val := range items {
			if val == "*" {
				allowed.Insert(str)
				continue
			}
			allowed.Insert(val)
		}
	} else if strings.Index(definition, "!") == 0 && strings.Index(definition, "[") == 1 && strings.Index(definition, "]") == (len(definition)-1) {
		// "!['namespace1', 'namespace2']"
		var items []string
		if err := json.Unmarshal([]byte(strings.Replace(definition[1:], "'", "\"", -1)), &items); err != nil {
			klog.V(4).Infof("Skip the permission definition %s", definition)
			return nil, fmt.Errorf("")
		}
		found := false
		for _, val := range items {
			if val == str || val == "*" {
				found = true
			}
		}

		if found {
			return nil, fmt.Errorf("")
		}
		allowed.Insert(str)
	}

	return allowed, nil
}

func resourcePermissionAllowed(permissionSpec map[string][]string, attr authorizer.Attributes) bool {
	ns := attr.GetNamespace()
	res := attr.GetResource()
	verb := attr.GetVerb()
	klog.V(4).Infof("Request namespace: %s, resource: %s, verb: %s", ns, res, verb)

	for key, value := range permissionSpec {
		klog.V(4).Infof("Evaluating %s: %s", key, value)

		allowedVerbs := sets.NewString()
		for _, val := range value {
			allowedVerbs.Insert(strings.ToLower(val))
		}
		if allowedVerbs.Has("*") {
			allowedVerbs.Insert(verb)
		}

		keyList := strings.Split(key, "/")
		if len(keyList) != 2 {
			// Ignore this spec
			klog.V(4).Infof("Skip the permission definition %s", key)
			continue
		}
		nsDef := strings.ToLower(strings.TrimSpace(keyList[0]))
		resDef := strings.ToLower(strings.TrimSpace(keyList[1]))

		allowedNamespaces, err := getAllowed(nsDef, ns)
		if err != nil {
			continue
		}

		allowedResources, err := getAllowed(resDef, res)
		if err != nil {
			continue
		}

		klog.V(4).Infof("allowedNamespaces: %s, allowedResources: %s, allowedVerbs: %s", allowedNamespaces.List(), allowedResources.List(), allowedVerbs.List())

		if allowedNamespaces.Has(ns) && allowedResources.Has(res) && allowedVerbs.Has(verb) {
			return true
		}
	}

	return false
}

func nonResourcePermissionAllowed(permissionSpec map[string][]string, attr authorizer.Attributes) bool {
	path := attr.GetPath()
	verb := attr.GetVerb()

	for key, value := range permissionSpec {
		allowedVerbs := sets.NewString()
		for _, val := range value {
			allowedVerbs.Insert(val)
		}
		if allowedVerbs.Has("*") {
			allowedVerbs.Insert(verb)
		}

		if key == path && allowedVerbs.Has(verb) {
			return true
		}
	}

	return false
}

func resourceMatches(p policy, a authorizer.Attributes) bool {
	if *p.ResourceSpec.APIGroup != "*" && *p.ResourceSpec.APIGroup != a.GetAPIGroup() {
		return false
	}
	if *p.ResourceSpec.Namespace != "*" && *p.ResourceSpec.Namespace != a.GetNamespace() {
		return false
	}
	if len(a.GetSubresource()) > 0 {
		if !findString("*", p.ResourceSpec.Resources) &&
			!findString(strings.Join([]string{a.GetResource(), "*"}, "/"), p.ResourceSpec.Resources) &&
			!findString(strings.Join([]string{a.GetResource(), a.GetSubresource()}, "/"), p.ResourceSpec.Resources) {
			return false
		}
	} else {
		if !findString("*", p.ResourceSpec.Resources) &&
			!findString(a.GetResource(), p.ResourceSpec.Resources) {
			return false
		}
	}
	if !findString("*", p.ResourceSpec.Verbs) && !findString(a.GetVerb(), p.ResourceSpec.Verbs) {
		return false
	}
	allowed := match(p.Match, a)
	if !allowed {
		return false
	}
	output, err := json.MarshalIndent(p, "", "  ")
	if err == nil {
		klog.V(6).Infof("matched rule : %s", string(output))
	}
	return true
}

func nonResourceMatches(p policy, a authorizer.Attributes) bool {
	if findString("", p.NonResourceSpec.Verbs) {
		klog.Infof("verb is empty. skipping : %#v", p)
		return false
	}

	if p.NonResourceSpec.NonResourcePath == nil {
		klog.Infof("path should be set. skipping : %#v", p)
		return false
	}

	if !findString("*", p.NonResourceSpec.Verbs) && !findString(a.GetVerb(), p.NonResourceSpec.Verbs) {
		return false
	}
	if *p.NonResourceSpec.NonResourcePath != "*" && *p.NonResourceSpec.NonResourcePath != a.GetPath() &&
		// Allow a trailing * subpath match
		!(strings.HasSuffix(*p.NonResourceSpec.NonResourcePath, "*") &&
			strings.HasPrefix(a.GetPath(), strings.TrimRight(*p.NonResourceSpec.NonResourcePath, "*"))) {
		return false
	}
	allowed := match(p.Match, a)
	if !allowed {
		return false
	}
	output, err := json.MarshalIndent(p, "", "  ")
	if err == nil {
		klog.V(6).Infof("matched rule : %s", string(output))
	}
	return true
}

func match(match []policyMatch, attributes authorizer.Attributes) bool {
	user := attributes.GetUser()
	var find = false
	types := []string{TypeGroup, TypeProject, TypeRole, TypeUser}

	for _, m := range match {
		if !findString(m.Type, types) {
			klog.Warningf("unknown type %s", m.Type)
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
			if val, ok := user.GetExtra()[ProjectID]; ok {
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

			if val, ok := user.GetExtra()[ProjectName]; ok {
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
			if val, ok := user.GetExtra()[Roles]; ok {
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
			klog.Infof("unknown type %s. skipping.", m.Type)
		}
	}

	return true
}

// Authorize checks whether the user can perform an operation
func (a *Authorizer) Authorize(attributes authorizer.Attributes) (authorized authorizer.Decision, reason string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Get roles and projects from the request.
	user := attributes.GetUser()
	userRoles := sets.NewString()
	if val, ok := user.GetExtra()[Roles]; ok {
		for _, role := range val {
			userRoles.Insert(role)
		}
	}

	// We support both project name and project ID.
	userProjects := sets.NewString()
	if val, ok := user.GetExtra()[ProjectName]; ok {
		for _, project := range val {
			userProjects.Insert(project)
		}
	}
	if val, ok := user.GetExtra()[ProjectID]; ok {
		for _, project := range val {
			userProjects.Insert(project)
		}
	}

	klog.V(4).Infof("Request userRoles: %s, userProjects: %s", userRoles.List(), userProjects.List())

	// The permission is whitelist. Make sure we go through all the policies that match the user roles and projects. If
	// the operation is allowed explicitly, stop the loop and return "allowed".
	for _, p := range a.pl {
		policyRoles := sets.NewString()
		policyProjects := sets.NewString()

		if p.Users != nil {
			if val, ok := p.Users["roles"]; ok {
				for _, role := range val {
					policyRoles.Insert(role)
				}
			}
			if val, ok := p.Users["projects"]; ok {
				for _, project := range val {
					policyProjects.Insert(project)
				}
			}

			klog.V(4).Infof("policyRoles: %s, policyProjects: %s", policyRoles.List(), policyProjects.List())

			if !userRoles.IsSuperset(policyRoles) || !policyProjects.HasAny(userProjects.List()...) {
				continue
			}
		}

		// ResourcePermissionsSpec and NonResourcePermissionsSpec take precedence over ResourceSpec and NonResourceSpec
		if attributes.IsResourceRequest() {
			if p.ResourcePermissionsSpec != nil {
				if resourcePermissionAllowed(p.ResourcePermissionsSpec, attributes) {
					return authorizer.DecisionAllow, "", nil
				}
			} else if p.ResourceSpec != nil {
				if resourceMatches(*p, attributes) {
					return authorizer.DecisionAllow, "", nil
				}
			}
		} else {
			if p.NonResourcePermissionsSpec != nil {
				if nonResourcePermissionAllowed(p.NonResourcePermissionsSpec, attributes) {
					return authorizer.DecisionAllow, "", nil
				}
			} else if p.NonResourceSpec != nil {
				if nonResourceMatches(*p, attributes) {
					return authorizer.DecisionAllow, "", nil
				}
			}
		}
	}

	klog.V(4).Infof("Authorization failed, user: %#v, attributes: %#v\n", attributes.GetUser(), attributes)
	return authorizer.DecisionDeny, "No policy matched.", nil
}
