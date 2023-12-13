/*
Copyright 2023 The Kubernetes Authors.

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

package openstack

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	"gopkg.in/godo.v2/glob"
)

// floatingSubnetSpec contains the specification of the public subnet to use for
// a public network. If given it may either describe the subnet id or
// a subnet name pattern for the subnet to use. If a pattern is given
// the first subnet matching the name pattern with an allocatable floating ip
// will be selected.
type floatingSubnetSpec struct {
	subnetID   string
	subnet     string
	subnetTags string
}

// TweakSubNetListOpsFunction is used to modify List Options for subnets
type TweakSubNetListOpsFunction func(*subnets.ListOpts)

// matcher matches a subnet
type matcher func(subnet *subnets.Subnet) bool

// negate returns a negated matches for a given one
func negate(f matcher) matcher { return func(s *subnets.Subnet) bool { return !f(s) } }

func andMatcher(a, b matcher) matcher {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func(s *subnets.Subnet) bool {
		return a(s) && b(s)
	}
}

// reexpNameMatcher creates a subnet matcher matching a subnet by name for a given regexp.
func regexpNameMatcher(r *regexp.Regexp) matcher {
	return func(s *subnets.Subnet) bool { return r.FindString(s.Name) == s.Name }
}

// subnetNameMatcher creates a subnet matcher matching a subnet by name for a given glob
// or regexp
func subnetNameMatcher(pat string) (matcher, error) {
	// try to create floating IP in matching subnets
	var match matcher
	not := false
	if strings.HasPrefix(pat, "!") {
		not = true
		pat = pat[1:]
	}
	if strings.HasPrefix(pat, "~") {
		rexp, err := regexp.Compile(pat[1:])
		if err != nil {
			return nil, fmt.Errorf("invalid subnet regexp pattern %q: %v", pat[1:], err)
		}
		match = regexpNameMatcher(rexp)
	} else {
		match = regexpNameMatcher(glob.Globexp(pat))
	}
	if not {
		match = negate(match)
	}
	return match, nil
}

// subnetTagMatcher matches a subnet by a given tag spec
func subnetTagMatcher(tags string) matcher {
	// try to create floating IP in matching subnets
	var match matcher

	list, not, all := tagList(tags)

	match = func(s *subnets.Subnet) bool {
		for _, tag := range list {
			found := false
			for _, t := range s.Tags {
				if t == tag {
					found = true
					break
				}
			}
			if found {
				if !all {
					return !not
				}
			} else {
				if all {
					return not
				}
			}
		}
		return not != all
	}
	return match
}

func (s *floatingSubnetSpec) Configured() bool {
	if s != nil && (s.subnetID != "" || s.MatcherConfigured()) {
		return true
	}
	return false
}

func (s *floatingSubnetSpec) ListSubnetsForNetwork(lbaas *LbaasV2, networkID string) ([]subnets.Subnet, error) {
	matcher, err := s.Matcher(false)
	if err != nil {
		return nil, err
	}
	list, err := lbaas.listSubnetsForNetwork(networkID, s.tweakListOpts)
	if err != nil {
		return nil, err
	}
	if matcher == nil {
		return list, nil
	}

	// filter subnets according to spec
	var foundSubnets []subnets.Subnet
	for _, subnet := range list {
		if matcher(&subnet) {
			foundSubnets = append(foundSubnets, subnet)
		}
	}
	return foundSubnets, nil
}

// tweakListOpts can be used to optimize a subnet list query for the
// actually described subnet filter
func (s *floatingSubnetSpec) tweakListOpts(opts *subnets.ListOpts) {
	if s.subnetTags != "" {
		list, not, all := tagList(s.subnetTags)
		tags := strings.Join(list, ",")
		if all {
			if not {
				opts.NotTagsAny = tags // at least one tag must be missing
			} else {
				opts.Tags = tags // all tags must be present
			}
		} else {
			if not {
				opts.NotTags = tags // none of the tags are present
			} else {
				opts.TagsAny = tags // at least one tag is present
			}
		}
	}
}

func (s *floatingSubnetSpec) MatcherConfigured() bool {
	if s != nil && s.subnetID == "" && (s.subnet != "" || s.subnetTags != "") {
		return true
	}
	return false
}

func addField(s, name, value string) string {
	if value == "" {
		return s
	}
	if s == "" {
		s += ", "
	}
	return fmt.Sprintf("%s%s: %q", s, name, value)
}

func (s *floatingSubnetSpec) String() string {
	if s == nil || (s.subnetID == "" && s.subnet == "" && s.subnetTags == "") {
		return "<none>"
	}
	pat := addField("", "subnetID", s.subnetID)
	pat = addField(pat, "pattern", s.subnet)
	return addField(pat, "tags", s.subnetTags)
}

func (s *floatingSubnetSpec) Matcher(tag bool) (matcher, error) {
	if !s.MatcherConfigured() {
		return nil, nil
	}
	var match matcher
	var err error
	if s.subnet != "" {
		match, err = subnetNameMatcher(s.subnet)
		if err != nil {
			return nil, err
		}
	}
	if tag && s.subnetTags != "" {
		match = andMatcher(match, subnetTagMatcher(s.subnetTags))
	}
	if match == nil {
		match = func(s *subnets.Subnet) bool { return true }
	}
	return match, nil
}

func tagList(tags string) ([]string, bool, bool) {
	not := strings.HasPrefix(tags, "!")
	if not {
		tags = tags[1:]
	}
	all := strings.HasPrefix(tags, "&")
	if all {
		tags = tags[1:]
	}
	list := strings.Split(tags, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}
	return list, not, all
}
