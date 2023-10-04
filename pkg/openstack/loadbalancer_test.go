package openstack

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type testPopListener struct {
	existingListeners []listeners.Listener
	id                string
	result            []string
	name              string
}

func TestPopListener(t *testing.T) {
	items := []testPopListener{
		{
			existingListeners: []listeners.Listener{},
			id:                "foobar",
			result:            []string{},
			name:              "empty listeners, id not exists",
		},
		{
			existingListeners: []listeners.Listener{
				{
					ID: "barfoo",
				},
			},
			id:     "foobar",
			result: []string{"barfoo"},
			name:   "id not found from listeners",
		},
		{
			existingListeners: []listeners.Listener{
				{
					ID: "barfoo",
				},
			},
			id:     "barfoo",
			result: []string{},
			name:   "id found from listeners",
		},
		{
			existingListeners: []listeners.Listener{
				{
					ID: "barfoo",
				},
				{
					ID: "barfoo2",
				},
				{
					ID: "barfoo3",
				},
				{
					ID: "barfoo4",
				},
			},
			id:     "barfoo",
			result: []string{"barfoo2", "barfoo3", "barfoo4"},
			name:   "barfoo multiple delete id from listeners",
		},
		{
			existingListeners: []listeners.Listener{
				{
					ID: "barfoo",
				},
				{
					ID: "barfoo2",
				},
				{
					ID: "barfoo3",
				},
				{
					ID: "barfoo4",
				},
			},
			id:     "barfoo2",
			result: []string{"barfoo", "barfoo3", "barfoo4"},
			name:   "barfoo2 multiple delete id from listeners",
		},
		{
			existingListeners: []listeners.Listener{
				{
					ID: "barfoo",
				},
				{
					ID: "barfoo2",
				},
				{
					ID: "barfoo3",
				},
				{
					ID: "barfoo4",
				},
			},
			id:     "barfoo3",
			result: []string{"barfoo", "barfoo2", "barfoo4"},
			name:   "barfoo3 multiple delete id from listeners",
		},
		{
			existingListeners: []listeners.Listener{
				{
					ID: "barfoo",
				},
				{
					ID: "barfoo2",
				},
				{
					ID: "barfoo3",
				},
				{
					ID: "barfoo4",
				},
			},
			id:     "barfoo4",
			result: []string{"barfoo", "barfoo2", "barfoo3"},
			name:   "barfoo4 multiple delete id from listeners",
		},
	}

	for _, item := range items {
		result := popListener(item.existingListeners, item.id)
		ids := []string{}
		for _, res := range result {
			ids = append(ids, res.ID)
		}
		sort.Strings(item.result)
		sort.Strings(ids)
		assert.Equal(t, ids, item.result, item.name)
	}
}

type testGetRulesToCreateAndDelete struct {
	testName      string
	wantedRules   []rules.CreateOpts
	existingRules []rules.SecGroupRule
	toCreate      []rules.CreateOpts
	toDelete      []rules.SecGroupRule
}

func TestGetRulesToCreateAndDelete(t *testing.T) {
	tests := []testGetRulesToCreateAndDelete{
		{
			testName:      "Empty elements",
			wantedRules:   []rules.CreateOpts{},
			existingRules: []rules.SecGroupRule{},
			toCreate:      []rules.CreateOpts{},
			toDelete:      []rules.SecGroupRule{},
		},
		{
			testName: "Removal of default egress SG rules",
			wantedRules: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/8",
				},
			},
			existingRules: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "egress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "0.0.0.0/0",
				}, {
					ID:             "baz",
					Direction:      "egress",
					EtherType:      "IPv6",
					SecGroupID:     "foo",
					RemoteIPPrefix: "::/0",
				},
			},
			toCreate: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/8",
				},
			},
			toDelete: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "egress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "0.0.0.0/0",
				}, {
					ID:             "baz",
					Direction:      "egress",
					EtherType:      "IPv6",
					SecGroupID:     "foo",
					RemoteIPPrefix: "::/0",
				},
			},
		},
		{
			testName: "Protocol case mismatch",
			wantedRules: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/8",
				},
			},
			existingRules: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "tcp",
					RemoteIPPrefix: "10.0.0.0/8",
				},
			},
			toCreate: []rules.CreateOpts{},
			toDelete: []rules.SecGroupRule{},
		},
		{
			testName: "changing a port number",
			wantedRules: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   124,
					PortRangeMin:   124,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/8",
				},
			},
			existingRules: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   123,
					PortRangeMin:   123,
				},
			},
			toCreate: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   124,
					PortRangeMin:   124,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/8",
				},
			},
			toDelete: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   123,
					PortRangeMin:   123,
				},
			},
		},
		{
			testName: "changing the CIDR",
			wantedRules: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/24",
				},
			},
			existingRules: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   123,
					PortRangeMin:   123,
				},
			},
			toCreate: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/24",
				},
			},
			toDelete: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   123,
					PortRangeMin:   123,
				},
			},
		},
		{
			testName:    "wiping all rules",
			wantedRules: []rules.CreateOpts{},
			existingRules: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   123,
					PortRangeMin:   123,
				},
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   124,
					PortRangeMin:   124,
				},
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   125,
					PortRangeMin:   125,
				},
			},
			toCreate: []rules.CreateOpts{},
			toDelete: []rules.SecGroupRule{
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   123,
					PortRangeMin:   123,
				},
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   124,
					PortRangeMin:   124,
				},
				{
					ID:             "bar",
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					RemoteIPPrefix: "10.0.0.0/8",
					PortRangeMax:   125,
					PortRangeMin:   125,
				},
			},
		},
		{
			testName: "several rules for an empty SG",
			wantedRules: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/8",
				}, {
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   124,
					PortRangeMin:   124,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.10.0/24",
				}, {
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   124,
					PortRangeMin:   124,
					Protocol:       "UDP",
					RemoteIPPrefix: "10.0.12.0/24",
				},
			},
			existingRules: []rules.SecGroupRule{},
			toCreate: []rules.CreateOpts{
				{
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   123,
					PortRangeMin:   123,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.0.0/8",
				}, {
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   124,
					PortRangeMin:   124,
					Protocol:       "TCP",
					RemoteIPPrefix: "10.0.10.0/24",
				}, {
					Direction:      "ingress",
					EtherType:      "IPv4",
					SecGroupID:     "foo",
					PortRangeMax:   124,
					PortRangeMin:   124,
					Protocol:       "UDP",
					RemoteIPPrefix: "10.0.12.0/24",
				},
			},
			toDelete: []rules.SecGroupRule{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			toCreate, toDelete := getRulesToCreateAndDelete(tt.wantedRules, tt.existingRules)
			assert.ElementsMatch(t, tt.toCreate, toCreate)
			assert.ElementsMatch(t, tt.toDelete, toDelete)
		})
	}
}

func TestLbaasV2_GetLoadBalancerName(t *testing.T) {
	lbaas := &LbaasV2{}
	var ctx context.Context

	type testArgs struct {
		ctx         context.Context
		clusterName string
		service     *corev1.Service
	}
	tests := []struct {
		name     string
		testArgs testArgs
	}{
		{
			name: "check valid input with short name",
			testArgs: testArgs{
				ctx:         ctx,
				clusterName: "my-valid-cluster",
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "valid-cluster-namespace",
						Name:      "valid-name",
					},
				},
			},
		},
		{
			name: "check valid input with longer names",
			testArgs: testArgs{
				ctx:         ctx,
				clusterName: "a-longer-valid-cluster",
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "a-longer-valid-cluster-namespace",
						Name:      "a-longer-valid-name",
					},
				},
			},
		},
		{
			name: "check empty input",
			testArgs: testArgs{
				ctx:         ctx,
				clusterName: "",
				service:     &corev1.Service{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedResult := fmt.Sprintf("%s%s_%s_%s", servicePrefix, tt.testArgs.clusterName, tt.testArgs.service.Namespace, tt.testArgs.service.Name)

			if len(expectedResult) > 255 {
				expectedResult = expectedResult[:255]
			}

			if got := lbaas.GetLoadBalancerName(tt.testArgs.ctx, tt.testArgs.clusterName, tt.testArgs.service); got != expectedResult {
				t.Errorf("LbaasV2.GetLoadBalancerName() = %v, want %v", got, expectedResult)
			}
		})
	}
}
