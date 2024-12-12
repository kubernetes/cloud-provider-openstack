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
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
)

func GetSecurityGroupRules(ctx context.Context, client *gophercloud.ServiceClient, opts rules.ListOpts) ([]rules.SecGroupRule, error) {
	mc := metrics.NewMetricContext("security_group_rule", "list")
	page, err := rules.List(client, opts).AllPages(ctx)
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	return rules.ExtractRules(page)
}
