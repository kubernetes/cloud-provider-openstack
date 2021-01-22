/*
Copyright 2016 The Kubernetes Authors.

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
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
)

func TestMatchSubnet(t *testing.T) {

	subnet := subnets.Subnet{
		Name: "test-123",
		Tags: []string{
			"alice",
			"bob",
		},
	}

	glob := floatingSubnetSpec{
		subnet: "test-*",
	}
	rexp := floatingSubnetSpec{
		subnet: "~test-.*",
	}
	tag := floatingSubnetSpec{
		subnetTags: "alice",
	}
	tagname := floatingSubnetSpec{
		subnet:     "test-*",
		subnetTags: "alice",
	}

	runName(t, &subnet, glob, true)
	runName(t, &subnet, rexp, true)
	runName(t, &subnet, tagname, true)

	runTag(t, &subnet, tag, true)
	runTag(t, &subnet, tagname, true)

	all := floatingSubnetSpec{
		subnetTags: "alice,bob",
	}
	one := floatingSubnetSpec{
		subnetTags: "alice,peter",
	}
	runTag(t, &subnet, all, true)
	runTag(t, &subnet, one, true)
	all.subnetTags = "&" + all.subnetTags
	one.subnetTags = "&" + one.subnetTags
	runTag(t, &subnet, all, true)
	runTag(t, &subnet, one, false)
}

func runName(t *testing.T, subnet *subnets.Subnet, spec floatingSubnetSpec, expected bool) {
	runNameNeg(t, subnet, spec, expected)
	spec.subnet = "other*"
	runNameNeg(t, subnet, spec, !expected)
	spec.subnet = "~other.*"
	runNameNeg(t, subnet, spec, !expected)
	spec.subnet = "*"
	runNameNeg(t, subnet, spec, true)
	spec.subnet = "~.*"
	runNameNeg(t, subnet, spec, true)
}

func runNameNeg(t *testing.T, subnet *subnets.Subnet, spec floatingSubnetSpec, expected bool) {
	runMatch(t, subnet, spec, expected)
	spec.subnet = "!" + spec.subnet
	runMatch(t, subnet, spec, !expected)
}

func runTag(t *testing.T, subnet *subnets.Subnet, spec floatingSubnetSpec, expected bool) {
	runMatch(t, subnet, spec, expected)

	spec.subnetTags = "!" + spec.subnetTags
	runMatch(t, subnet, spec, !expected)

	if strings.Index(spec.subnetTags, ",") < 0 {
		spec.subnetTags = "other"
		runMatch(t, subnet, spec, !expected)

		spec.subnetTags = "!" + spec.subnetTags
		runMatch(t, subnet, spec, expected)
	}
}

func runMatch(t *testing.T, subnet *subnets.Subnet, spec floatingSubnetSpec, expected bool) {
	m, err := spec.Matcher(true)
	assert.NoError(t, err)
	assert.Equal(t, m(subnet), expected)
}
