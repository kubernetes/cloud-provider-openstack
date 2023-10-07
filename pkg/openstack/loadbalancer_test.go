package openstack

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
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

func Test_getListenerProtocol(t *testing.T) {
	type testArg struct {
		protocol corev1.Protocol
		svcConf  *serviceConfig
	}

	tests := []struct {
		name     string
		testArg  testArg
		expected listeners.Protocol
	}{
		{
			name: "not nil svcConf and tlsContainerRef is not empty",
			testArg: testArg{
				svcConf: &serviceConfig{
					tlsContainerRef: "tls-container-ref",
				},
			},
			expected: listeners.ProtocolTerminatedHTTPS,
		},
		{
			name: "not nil svcConf and keepClientIP is true",
			testArg: testArg{
				svcConf: &serviceConfig{
					keepClientIP: true,
				},
			},
			expected: listeners.ProtocolHTTP,
		},
		{
			name: "nil svcConf with TCP protocol",
			testArg: testArg{
				svcConf:  nil,
				protocol: corev1.ProtocolTCP,
			},
			expected: listeners.ProtocolTCP,
		},
		{
			name: "nil svcConf with UDP protocol",
			testArg: testArg{
				svcConf:  nil,
				protocol: corev1.ProtocolUDP,
			},
			expected: listeners.ProtocolUDP,
		},
		{
			name: "test for no specification on svc and a random protocol to test it return value",
			testArg: testArg{
				svcConf:  nil,
				protocol: corev1.ProtocolSCTP,
			},
			expected: listeners.ProtocolSCTP,
		},
		{
			name: "passing a svcConf tls container ref with a keep client IP",
			testArg: testArg{
				svcConf: &serviceConfig{
					tlsContainerRef: "tls-container-ref",
					keepClientIP:    true,
				},
			},
			expected: listeners.ProtocolTerminatedHTTPS,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getListenerProtocol(tt.testArg.protocol, tt.testArg.svcConf); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("getListenerProtocol() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func Test_getIntFromServiceAnnotation(t *testing.T) {
	type args struct {
		service        *corev1.Service
		annotationKey  string
		defaultSetting int
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "return default setting if no service annotation",
			args: args{
				defaultSetting: 1,
				annotationKey:  "bar",
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"foo": "2"},
					},
				},
			},
			want: 1,
		},
		{
			name: "return annotation key if it exists in service annotation",
			args: args{
				defaultSetting: 1,
				annotationKey:  "foo",
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"foo": "2"},
					},
				},
			},
			want: 2,
		},
		{
			name: "return default setting if key isn't valid integer",
			args: args{
				defaultSetting: 1,
				annotationKey:  "foo",
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"foo": "bar"},
					},
				},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, getIntFromServiceAnnotation(tt.args.service, tt.args.annotationKey, tt.args.defaultSetting))
		})
	}
}

func TestLbaasV2_GetLoadBalancerName(t *testing.T) {
	lbaas := &LbaasV2{}

	type testArgs struct {
		ctx         context.Context
		clusterName string
		service     *corev1.Service
	}
	tests := []struct {
		name     string
		testArgs testArgs
		expected string
	}{
		{
			name: "valid input with short name",
			testArgs: testArgs{
				ctx:         context.Background(),
				clusterName: "my-valid-cluster",
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "valid-cluster-namespace",
						Name:      "valid-name",
					},
				},
			},
			expected: "kube_service_my-valid-cluster_valid-cluster-namespace_valid-name",
		},
		{
			name: "input that surpass value maximum length",
			testArgs: testArgs{
				ctx:         context.Background(),
				clusterName: "a-longer-valid-cluster",
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "a-longer-valid-cluster-namespace",
						Name:      "a-longer-valid-name-for-the-load-balance-name-to-test-if-the-length-of-value-is-longer-than-required-maximum-length-random-addition-hardcode-number-to-make-it-above-length-255-at-the-end-yeah-so-the-rest-is-additional-input",
					},
				},
			},
			expected: "kube_service_a-longer-valid-cluster_a-longer-valid-cluster-namespace_a-longer-valid-name-for-the-load-balance-name-to-test-if-the-length-of-value-is-longer-than-required-maximum-length-random-addition-hardcode-number-to-make-it-above-length-255-at-the-end",
		},
		{
			name: "empty input",
			testArgs: testArgs{
				ctx:         context.Background(),
				clusterName: "",
				service:     &corev1.Service{},
			},
			expected: "kube_service___",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lbaas.GetLoadBalancerName(tt.testArgs.ctx, tt.testArgs.clusterName, tt.testArgs.service)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func Test_buildPoolCreateOpt(t *testing.T) {
	type args struct {
		protocol string
		svcConf  *serviceConfig
		service  *corev1.Service
		lbaasV2  *LbaasV2
	}
	tests := []struct {
		name string
		args args
		want pools.CreateOpts
	}{
		{
			name: "test for proxy protocol enabled",
			args: args{
				protocol: "TCP",
				svcConf: &serviceConfig{
					keepClientIP:        true,
					tlsContainerRef:     "tls-container-ref",
					enableProxyProtocol: true,
				},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
							LBMethod:   "SOURCE_IP_PORT",
						},
					},
				},
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						SessionAffinity: corev1.ServiceAffinityClientIP,
					},
				},
			},
			want: pools.CreateOpts{
				Name:        "test for proxy protocol enabled",
				Protocol:    pools.ProtocolPROXY,
				LBMethod:    "SOURCE_IP_PORT",
				Persistence: &pools.SessionPersistence{Type: "SOURCE_IP"},
			},
		},
		{
			name: "test for pool protocol http with proxy protocol disabled",
			args: args{
				protocol: "HTTP",
				svcConf: &serviceConfig{
					keepClientIP:        true,
					tlsContainerRef:     "tls-container-ref",
					enableProxyProtocol: false,
				},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
							LBMethod:   "SOURCE_IP_PORT",
						},
					},
				},
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						SessionAffinity: corev1.ServiceAffinityClientIP,
					},
				},
			},
			want: pools.CreateOpts{
				Name:        "test for pool protocol http with proxy protocol disabled",
				Protocol:    pools.ProtocolHTTP,
				LBMethod:    "SOURCE_IP_PORT",
				Persistence: &pools.SessionPersistence{Type: "SOURCE_IP"},
			},
		},
		{
			name: "test for pool protocol UDP with proxy protocol disabled",
			args: args{
				protocol: "UDP",
				svcConf: &serviceConfig{
					keepClientIP:        true,
					tlsContainerRef:     "tls-container-ref",
					enableProxyProtocol: false,
				},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
							LBMethod:   "SOURCE_IP_PORT",
						},
					},
				},
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						SessionAffinity: corev1.ServiceAffinityClientIP,
					},
				},
			},
			want: pools.CreateOpts{
				Name:        "test for pool protocol UDP with proxy protocol disabled",
				Protocol:    pools.ProtocolHTTP,
				LBMethod:    "SOURCE_IP_PORT",
				Persistence: &pools.SessionPersistence{Type: "SOURCE_IP"},
			},
		},
		{
			name: "test for session affinity none",
			args: args{
				protocol: "TCP",
				svcConf: &serviceConfig{
					keepClientIP:    true,
					tlsContainerRef: "tls-container-ref",
				},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
							LBMethod:   "SOURCE_IP_PORT",
						},
					},
				},
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						SessionAffinity: corev1.ServiceAffinityNone,
					},
				},
			},
			want: pools.CreateOpts{
				Name:        "test for session affinity none",
				Protocol:    pools.ProtocolHTTP,
				LBMethod:    "SOURCE_IP_PORT",
				Persistence: nil,
			},
		},
		{
			name: "test for session affinity client ip",
			args: args{
				protocol: "TCP",
				svcConf: &serviceConfig{
					keepClientIP:    true,
					tlsContainerRef: "tls-container-ref",
				},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
							LBMethod:   "SOURCE_IP_PORT",
						},
					},
				},
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						SessionAffinity: corev1.ServiceAffinityClientIP,
					},
				},
			},
			want: pools.CreateOpts{
				Name:        "test for session affinity client ip",
				Protocol:    pools.ProtocolHTTP,
				LBMethod:    "SOURCE_IP_PORT",
				Persistence: &pools.SessionPersistence{Type: "SOURCE_IP"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.args.lbaasV2.buildPoolCreateOpt(tt.args.protocol, tt.args.service, tt.args.svcConf, tt.name)
			assert.Equal(t, got, tt.want)
		})
	}
}

func Test_getSecurityGroupName(t *testing.T) {
	tests := []struct {
		name     string
		service  *corev1.Service
		expected string
	}{
		{
			name: "regular test security group name and length",
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					UID:       "12345",
					Namespace: "security-group-namespace",
					Name:      "security-group-name",
				},
			},
			expected: "lb-sg-12345-security-group-namespace-security-group-name",
		},
		{
			name: "security group name longer than 255 byte",
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					UID:       "12345678-90ab-cdef-0123-456789abcdef",
					Namespace: "security-group-longer-test-namespace",
					Name:      "security-group-longer-test-service-name-with-more-than-255-byte-this-test-should-be-longer-than-255-i-need-that-ijiojohoo-afhwefkbfk-jwebfwbifwbewifobiu-efbiobfoiqwebi-the-end-e-end-pardon-the-long-string-i-really-apologize-if-this-is-a-bad-thing-to-do",
				},
			},
			expected: "lb-sg-12345678-90ab-cdef-0123-456789abcdef-security-group-longer-test-namespace-security-group-longer-test-service-name-with-more-than-255-byte-this-test-should-be-longer-than-255-i-need-that-ijiojohoo-afhwefkbfk-jwebfwbifwbewifobiu-efbiobfoiqwebi-the-end",
		},
		{
			name: "test the security group name with all empty param",
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{},
			},
			expected: "lb-sg---",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := getSecurityGroupName(test.service)

			assert.Equal(t, test.expected, got)
		})
	}
}

func Test_getBoolFromServiceAnnotation(t *testing.T) {
	type testargs struct {
		service        *corev1.Service
		annotationKey  string
		defaultSetting bool
	}
	tests := []struct {
		name     string
		testargs testargs
		want     bool
	}{
		{
			name: "Return default setting if no service annotation",
			testargs: testargs{
				annotationKey:  "bar",
				defaultSetting: true,
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"foo": "false"},
					},
				},
			},
			want: true,
		},
		{
			name: "Return annotation key if it exists in service annotation (true)",
			testargs: testargs{
				annotationKey:  "foo",
				defaultSetting: false,
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"foo": "true"},
					},
				},
			},
			want: true,
		},
		{
			name: "Return annotation key if it exists in service annotation (false)",
			testargs: testargs{
				annotationKey:  "foo",
				defaultSetting: true,
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"foo": "false"},
					},
				},
			},
			want: false,
		},
		{
			name: "Return default setting if key isn't a valid boolean value",
			testargs: testargs{
				annotationKey:  "foo",
				defaultSetting: true,
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"foo": "invalid"},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBoolFromServiceAnnotation(tt.testargs.service, tt.testargs.annotationKey, tt.testargs.defaultSetting)
			if got != tt.want {
				t.Errorf("getBoolFromServiceAnnotation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLbaasV2_updateServiceAnnotations(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Annotations: nil,
		},
	}

	annotations := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	lbaas := LbaasV2{}
	lbaas.updateServiceAnnotations(service, annotations)

	serviceAnnotations := make([]map[string]string, 0)
	for key, value := range service.ObjectMeta.Annotations {
		serviceAnnotations = append(serviceAnnotations, map[string]string{key: value})
	}

	expectedAnnotations := []map[string]string{
		{"key1": "value1"},
		{"key2": "value2"},
	}

	assert.ElementsMatch(t, expectedAnnotations, serviceAnnotations)
}
