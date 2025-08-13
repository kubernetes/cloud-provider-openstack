package openstack

import (
	"context"
	"fmt"
	"k8s.io/utils/ptr"
	"reflect"
	"sort"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/listeners"
	v2monitors "github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/monitors"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
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

func TestLbaasV2_checkListenerPorts(t *testing.T) {
	type args struct {
		service            *corev1.Service
		curListenerMapping map[listenerKey]*listeners.Listener
		isLBOwner          bool
		lbName             string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "error is not thrown if loadbalancer matches & if port is already in use by a lb",
			args: args{
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     "service",
								Protocol: "https",
								Port:     9090,
							},
						},
					},
				},
				curListenerMapping: map[listenerKey]*listeners.Listener{
					{
						Protocol: "https",
						Port:     9090,
					}: {
						ID:   "listenerid",
						Tags: []string{"test-lb"},
					},
				},
				isLBOwner: false,
				lbName:    "test-lb",
			},
			wantErr: false,
		},
		{
			name: "error is thrown if loadbalancer doesn't matches & if port is already in use by a service",
			args: args{
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     "service",
								Protocol: "https",
								Port:     9090,
							},
						},
					},
				},
				curListenerMapping: map[listenerKey]*listeners.Listener{
					{
						Protocol: "https",
						Port:     9090,
					}: {
						ID:   "listenerid",
						Tags: []string{"test-lb", "test-lb1"},
					},
				},
				isLBOwner: false,
				lbName:    "test-lb2",
			},
			wantErr: true,
		},
		{
			name: "error is not thrown if lbOwner is present & no tags on service",
			args: args{
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     "service",
								Protocol: "https",
								Port:     9090,
							},
						},
					},
				},
				curListenerMapping: map[listenerKey]*listeners.Listener{
					{
						Protocol: "https",
						Port:     9090,
					}: {
						ID: "listenerid",
					},
				},
				isLBOwner: true,
				lbName:    "test-lb",
			},
			wantErr: false,
		},
		{
			name: "error is not thrown if lbOwner is true & there are tags on service",
			args: args{
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     "service",
								Protocol: "http",
								Port:     9091,
							},
						},
					},
				},
				curListenerMapping: map[listenerKey]*listeners.Listener{
					{
						Protocol: "https",
						Port:     9090,
					}: {
						ID:   "listenerid",
						Tags: []string{"test-lb"},
					},
				},
				isLBOwner: true,
				lbName:    "test-lb",
			},
			wantErr: false,
		},
		{
			name: "error is not thrown if listener key doesn't match port & protocol",
			args: args{
				service: &corev1.Service{
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     "service",
								Protocol: "http",
								Port:     9091,
							},
						},
					},
				},
				curListenerMapping: map[listenerKey]*listeners.Listener{
					{
						Protocol: "https",
						Port:     9090,
					}: {
						ID:   "listenerid",
						Tags: []string{"test-lb"},
					},
				},
				isLBOwner: false,
				lbName:    "test-lb",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lbaas := &LbaasV2{
				LoadBalancer: LoadBalancer{},
			}
			err := lbaas.checkListenerPorts(tt.args.service, tt.args.curListenerMapping, tt.args.isLBOwner, tt.args.lbName)
			if tt.wantErr == true {
				assert.ErrorContains(t, err, "already exists")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
func TestLbaasV2_createLoadBalancerStatus(t *testing.T) {
	ipmodeProxy := corev1.LoadBalancerIPModeProxy
	ipmodeVIP := corev1.LoadBalancerIPModeVIP
	type fields struct {
		LoadBalancer LoadBalancer
	}
	type result struct {
		HostName  string
		IPAddress string
		IPMode    *corev1.LoadBalancerIPMode
	}
	type args struct {
		service *corev1.Service
		svcConf *serviceConfig
		addr    string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   result
	}{
		{
			name: "it should return hostname from service annotation",
			fields: fields{
				LoadBalancer: LoadBalancer{
					opts: LoadBalancerOpts{
						EnableIngressHostname: false,
						IngressHostnameSuffix: "test",
					},
				},
			},
			args: args{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"loadbalancer.openstack.org/hostname": "testHostName"},
					},
				},
				svcConf: &serviceConfig{
					proxyProtocolVersion: nil,
				},
				addr: "10.10.0.6",
			},
			want: result{
				HostName: "testHostName",
			},
		},
		{
			name: "it should return fakehostname if proxyProtocol & IngressHostName is enabled without svc annotation",
			fields: fields{
				LoadBalancer: LoadBalancer{
					opts: LoadBalancerOpts{
						EnableIngressHostname: true,
						IngressHostnameSuffix: "ingress-suffix",
					},
				},
			},
			args: args{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"test": "key"},
					},
				},
				svcConf: &serviceConfig{
					proxyProtocolVersion: ptr.To(pools.ProtocolPROXY),
				},
				addr: "10.10.0.6",
			},
			want: result{
				HostName: "10.10.0.6.ingress-suffix",
			},
		},
		{
			name: "it should default to ip address if not hostname can be found from svc or proxyProtocol",
			fields: fields{
				LoadBalancer: LoadBalancer{
					opts: LoadBalancerOpts{
						EnableIngressHostname: false,
						IngressHostnameSuffix: "ingress-suffix",
					},
				},
			},
			args: args{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"test": "key"},
					},
				},
				svcConf: &serviceConfig{
					proxyProtocolVersion: nil,
				},
				addr: "10.10.0.6",
			},
			want: result{
				IPAddress: "10.10.0.6",
				IPMode:    &ipmodeVIP,
			},
		},
		{
			name: "it should return ipMode proxy if using proxyProtocol and not EnableIngressHostname",
			fields: fields{
				LoadBalancer: LoadBalancer{
					opts: LoadBalancerOpts{
						EnableIngressHostname: false,
						IngressHostnameSuffix: "ingress-suffix",
					},
				},
			},
			args: args{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"test": "key"},
					},
				},
				svcConf: &serviceConfig{
					proxyProtocolVersion: ptr.To(pools.ProtocolPROXY),
				},
				addr: "10.10.0.6",
			},
			want: result{
				IPAddress: "10.10.0.6",
				IPMode:    &ipmodeProxy,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lbaas := &LbaasV2{
				LoadBalancer: tt.fields.LoadBalancer,
			}

			result := lbaas.createLoadBalancerStatus(tt.args.service, tt.args.svcConf, tt.args.addr)
			assert.Equal(t, tt.want.HostName, result.Ingress[0].Hostname)
			assert.Equal(t, tt.want.IPAddress, result.Ingress[0].IP)
			assert.Equal(t, tt.want.IPMode, result.Ingress[0].IPMode)
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
					keepClientIP:         true,
					tlsContainerRef:      "tls-container-ref",
					proxyProtocolVersion: ptr.To(pools.ProtocolPROXY),
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
					keepClientIP:         true,
					tlsContainerRef:      "tls-container-ref",
					proxyProtocolVersion: nil,
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
					keepClientIP:         true,
					tlsContainerRef:      "tls-container-ref",
					proxyProtocolVersion: nil,
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
		{
			name: "test for proxy protocol v2 enabled",
			args: args{
				protocol: "TCP",
				svcConf: &serviceConfig{
					keepClientIP:         true,
					tlsContainerRef:      "tls-container-ref",
					proxyProtocolVersion: ptr.To(pools.ProtocolPROXYV2),
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
				Name:        "test for proxy protocol v2 enabled",
				Protocol:    pools.ProtocolPROXYV2,
				LBMethod:    "SOURCE_IP_PORT",
				Persistence: &pools.SessionPersistence{Type: "SOURCE_IP"},
			},
		},
		{
			name: "test for loadbalancing method",
			args: args{
				protocol: "TCP",
				svcConf: &serviceConfig{
					poolLbMethod: "ROUND_ROBIN",
				},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
							LBMethod:   "SOURCE_IP_PORT",
						},
					},
				},
				service: &corev1.Service{},
			},
			want: pools.CreateOpts{
				Name:     "test for loadbalancing method",
				Protocol: pools.ProtocolTCP,
				LBMethod: "ROUND_ROBIN",
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

	lbaas := LbaasV2{}
	lbaas.updateServiceAnnotation(service, "key1", "value1")

	serviceAnnotations := make([]map[string]string, 0)
	for key, value := range service.Annotations {
		serviceAnnotations = append(serviceAnnotations, map[string]string{key: value})
	}

	expectedAnnotations := []map[string]string{
		{"key1": "value1"},
	}

	assert.ElementsMatch(t, expectedAnnotations, serviceAnnotations)
}

func Test_getStringFromServiceAnnotation(t *testing.T) {
	type testArgs struct {
		service        *corev1.Service
		annotationKey  string
		defaultSetting string
	}

	tests := []struct {
		name     string
		testArgs testArgs
		expected string
	}{
		{
			name: "enter empty arguments",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{},
				},
				annotationKey:  "",
				defaultSetting: "",
			},
			expected: "",
		},
		{
			name: "enter valid arguments with annotations",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace:   "service-namespace",
						Name:        "service-name",
						Annotations: map[string]string{"annotationKey": "annotation-Value"},
					},
				},
				annotationKey:  "annotationKey",
				defaultSetting: "default-setting",
			},
			expected: "annotation-Value",
		},
		{
			name: "valid arguments without annotations",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "service-namespace",
						Name:      "service-name",
					},
				},
				annotationKey:  "annotationKey",
				defaultSetting: "default-setting",
			},
			expected: "default-setting",
		},
		{
			name: "enter argument without default-setting",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace:   "service-namespace",
						Name:        "service-name",
						Annotations: map[string]string{"annotationKey": "annotation-Value"},
					},
				},
				annotationKey:  "annotationKey",
				defaultSetting: "",
			},
			expected: "annotation-Value",
		},
		{
			name: "enter argument without annotation and default-setting",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "service-namespace",
						Name:      "service-name",
					},
				},
				annotationKey:  "annotationKey",
				defaultSetting: "",
			},
			expected: "",
		},
		{
			name: "enter argument with a non-existing annotationKey with default setting",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace:   "service-namespace",
						Name:        "service-name",
						Annotations: map[string]string{"annotationKey": "annotation-Value"},
					},
				},
				annotationKey:  "invalid-annotationKey",
				defaultSetting: "default-setting",
			},
			expected: "default-setting",
		},
		{
			name: "enter argument with a non-existing annotationKey without a default setting",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Namespace:   "service-namespace",
						Name:        "service-name",
						Annotations: map[string]string{"annotationKey": "annotation-Value"},
					},
				},
				annotationKey:  "invalid-annotationKey",
				defaultSetting: "",
			},
			expected: "",
		},
		{
			name: "no name-space and service name but valid annotations",
			testArgs: testArgs{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"annotationKey": "annotation-Value"},
					},
				},
				annotationKey:  "annotationKey",
				defaultSetting: "default-setting",
			},
			expected: "annotation-Value",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := getStringFromServiceAnnotation(test.testArgs.service, test.testArgs.annotationKey, test.testArgs.defaultSetting)

			assert.Equal(t, test.expected, got)
		})
	}
}

func Test_nodeAddressForLB(t *testing.T) {
	type testArgs struct {
		node              *corev1.Node
		preferredIPFamily corev1.IPFamily
	}

	tests := []struct {
		name        string
		testArgs    testArgs
		expect      string
		expectedErr error
	}{
		{
			name: "Empty Address with IPv4 protocol family ",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{},
					},
				},
				preferredIPFamily: corev1.IPv4Protocol,
			},
			expect:      "",
			expectedErr: cpoerrors.ErrNoAddressFound,
		},
		{
			name: "Empty Address with IPv6 protocol family ",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{},
					},
				},
				preferredIPFamily: corev1.IPv6Protocol,
			},
			expect:      "",
			expectedErr: cpoerrors.ErrNoAddressFound,
		},
		{
			name: "valid address with IPv4 protocol family",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.1.1",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv4Protocol,
			},
			expect:      "192.168.1.1",
			expectedErr: nil,
		},
		{
			name: "valid address with IPv6 protocol family",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv6Protocol,
			},
			expect:      "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			expectedErr: nil,
		},
		{
			name: "multiple IPv4 address",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeExternalIP,
								Address: "192.168.1.2",
							},
							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.1.1",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv4Protocol,
			},
			expect:      "192.168.1.1",
			expectedErr: nil,
		},
		{
			name: "multiple IPv6 address",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeExternalIP,
								Address: "2001:0db8:85a3:3333:1111:8a2e:9999:8888",
							},
							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv6Protocol,
			},
			expect:      "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			expectedErr: nil,
		},
		{
			name: "multiple mix addresses expecting IPv6 response",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.1.1",
							},
							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv6Protocol,
			},
			expect:      "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			expectedErr: nil,
		},
		{
			name: "multiple mix addresses expecting IPv4 response",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeExternalIP,
								Address: "2009:0db8:85a3:0003:0001:8a2e:0370:9999",
							},

							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},

							{
								Type:    corev1.NodeExternalIP,
								Address: "2001:0db8:85a3:0000:1111:8a2e:9798:7334",
							},

							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.1.1",
							},

							{
								Type:    corev1.NodeExternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv4Protocol,
			},
			expect:      "192.168.1.1",
			expectedErr: nil,
		},
		{
			name: "single valid IPv4 address without preferred valid specification",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.1.1",
							},
						},
					},
				},
			},
			expect:      "192.168.1.1",
			expectedErr: nil,
		},
		{
			name: "single valid IPv6 address without preferred valid specification",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},
						},
					},
				},
			},
			expect:      "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			expectedErr: nil,
		},
		{
			name: "multiple valid IPv6 address without preferred valid specification",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},
							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.0.1",
							},
							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:1111:2222:8a2e:6869:7334",
							},
						},
					},
				},
			},
			expect:      "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			expectedErr: nil,
		},
		{
			name: "invalid IPv4 address specification",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv4Protocol,
			},
			expect:      "",
			expectedErr: cpoerrors.ErrNoAddressFound,
		},
		{
			name: "invalid IPv6 address specification",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "192.168.1.1",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv6Protocol,
			},
			expect:      "",
			expectedErr: cpoerrors.ErrNoAddressFound,
		},
		{
			name: "Ignore NodeExternalDNS address with IPv4 protocol family",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeExternalDNS,
								Address: "example.com",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv4Protocol,
			},
			expect:      "",
			expectedErr: cpoerrors.ErrNoAddressFound,
		},
		{
			name: "Ignore NodeExternalDNS address with IPv6 protocol family",
			testArgs: testArgs{
				node: &corev1.Node{
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeExternalDNS,
								Address: "example.com",
							},
						},
					},
				},
				preferredIPFamily: corev1.IPv6Protocol,
			},
			expect:      "",
			expectedErr: cpoerrors.ErrNoAddressFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := nodeAddressForLB(test.testArgs.node, test.testArgs.preferredIPFamily)
			if test.expectedErr != nil {
				assert.EqualError(t, err, test.expectedErr.Error())
			} else {
				assert.NoError(t, test.expectedErr, err)
			}

			assert.Equal(t, test.expect, got)
		})
	}
}

func TestLbaasV2_getMemberSubnetID(t *testing.T) {
	lbaasOpts := LoadBalancerOpts{
		LBClasses: map[string]*LBClass{
			"lbclassKey": {
				MemberSubnetID: "lb-class-member-subnet-id-5678",
			},
		},
		MemberSubnetID: "default-memberSubnetId",
	}

	tests := []struct {
		name    string
		opts    LoadBalancerOpts
		service *corev1.Service
		want    string
		wantErr string
	}{
		{
			name: "get member subnet id from service annotation",
			opts: LoadBalancerOpts{},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerMemberSubnetID: "member-subnet-id",
						ServiceAnnotationLoadBalancerClass:          "svc-annotation-loadbalance-class",
					},
				},
			},
			want:    "member-subnet-id",
			wantErr: "",
		},
		{
			name: "get member subnet id from config class",
			opts: lbaasOpts,
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerClass: "lbclassKey",
					},
				},
			},
			want:    "lb-class-member-subnet-id-5678",
			wantErr: "",
		},
		{
			name:    "get member subnet id from default config",
			opts:    lbaasOpts,
			service: &corev1.Service{},
			want:    "default-memberSubnetId",
			wantErr: "",
		},
		{
			name: "error when loadbalancer class not found",
			opts: LoadBalancerOpts{},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						ServiceAnnotationLoadBalancerClass: "invalid-lb-class",
					},
				},
			},
			want:    "",
			wantErr: "invalid loadbalancer class \"invalid-lb-class\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lbaas := LbaasV2{
				LoadBalancer: LoadBalancer{
					opts: tt.opts,
				},
			}

			got, err := lbaas.getMemberSubnetID(tt.service)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildBatchUpdateMemberOpts(t *testing.T) {
	// Sample Nodes
	node1 := &corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: "node-1",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "192.168.1.1",
				},
			},
		},
	}
	node2 := &corev1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: "node-2",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "192.168.1.2",
				},
			},
		},
	}
	testCases := []struct {
		name                    string
		nodes                   []*corev1.Node
		port                    corev1.ServicePort
		svcConf                 *serviceConfig
		expectedLen             int
		expectedNewMembersCount int
	}{
		{
			name:  "NodePortequalszero",
			nodes: []*corev1.Node{node1, node2},
			port:  corev1.ServicePort{NodePort: 0},
			svcConf: &serviceConfig{
				preferredIPFamily:   corev1.IPv4Protocol,
				lbMemberSubnetID:    "subnet-12345-test",
				healthCheckNodePort: 8081,
			},
			expectedLen:             0,
			expectedNewMembersCount: 0,
		},
		{
			name:  "Valid nodes, canUseHTTPMonitor=false",
			nodes: []*corev1.Node{node1, node2},
			port:  corev1.ServicePort{NodePort: 8080},
			svcConf: &serviceConfig{
				preferredIPFamily:   corev1.IPv4Protocol,
				lbMemberSubnetID:    "subnet-12345-test",
				healthCheckNodePort: 8081,
				enableMonitor:       false,
			},
			expectedLen:             2,
			expectedNewMembersCount: 2,
		},
		{
			name:  "Valid nodes, canUseHTTPMonitor=true",
			nodes: []*corev1.Node{node1, node2},
			port:  corev1.ServicePort{NodePort: 8080},
			svcConf: &serviceConfig{
				preferredIPFamily:   corev1.IPv4Protocol,
				lbMemberSubnetID:    "subnet-12345-test",
				healthCheckNodePort: 8081,
				enableMonitor:       true,
			},
			expectedLen:             2,
			expectedNewMembersCount: 2,
		},
		{
			name:  "Invalid preferred IP family, fallback to default",
			nodes: []*corev1.Node{node1, node2},
			port:  corev1.ServicePort{NodePort: 0},
			svcConf: &serviceConfig{
				preferredIPFamily:   "invalid-family",
				lbMemberSubnetID:    "subnet-12345-test",
				healthCheckNodePort: 8081,
			},
			expectedLen:             0,
			expectedNewMembersCount: 0,
		},
		{
			name: "ErrNoAddressFound happens and no member is created",
			nodes: []*corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{Name: "node-1"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{},
					},
				},
			},
			port: corev1.ServicePort{NodePort: 8080},
			svcConf: &serviceConfig{
				preferredIPFamily:   corev1.IPv4Protocol,
				lbMemberSubnetID:    "subnet-12345-test",
				healthCheckNodePort: 8081,
				enableMonitor:       false,
			},
			expectedLen:             0,
			expectedNewMembersCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lbaas := &LbaasV2{}
			members, newMembers, err := lbaas.buildBatchUpdateMemberOpts(context.TODO(), tc.port, tc.nodes, tc.svcConf)
			assert.Len(t, members, tc.expectedLen)
			assert.NoError(t, err)

			if tc.expectedNewMembersCount == 0 {
				assert.Empty(t, newMembers)
			} else {
				assert.Len(t, newMembers, tc.expectedNewMembersCount)
			}
		})
	}
}

func Test_getSubnetID(t *testing.T) {
	type args struct {
		svcConf *serviceConfig
		service *corev1.Service
		lbaasV2 *LbaasV2
	}
	tests := []struct {
		name        string
		args        args
		want        string
		expectedErr string
	}{
		{
			name: "test get subnet from service annotation",
			args: args{
				svcConf: &serviceConfig{},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBClasses: map[string]*LBClass{
								"test-class": {
									SubnetID: "test-class-subnet-id",
								},
							},
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							"loadbalancer.openstack.org/subnet-id": "annotation-test-id",
							"loadbalancer.openstack.org/class":     "test-class",
						},
					},
				},
			},
			want: "annotation-test-id",
		},
		{
			name: "test get subnet from config class",
			args: args{
				svcConf: &serviceConfig{},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBClasses: map[string]*LBClass{
								"test-class": {
									SubnetID: "test-class-subnet-id",
								},
							},
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							"loadbalancer.openstack.org/class": "test-class",
						},
					},
				},
			},
			want: "test-class-subnet-id",
		},
		{
			name: "test get subnet from config class with invalid loadbalancer class",
			args: args{
				svcConf: &serviceConfig{},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBClasses: map[string]*LBClass{
								"decoy-class": {
									SubnetID: "test-id",
								},
							},
							SubnetID: "test-subnet-id",
						},
					},
				},
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							"loadbalancer.openstack.org/class": "test-class",
						},
					},
				},
			},
			want:        "",
			expectedErr: fmt.Sprintf("invalid loadbalancer class %q", "test-class"),
		},
		{
			name: "test get subnet from default config",
			args: args{
				svcConf: &serviceConfig{},
				lbaasV2: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBClasses: map[string]*LBClass{
								"test-config-class-subnet-id": {
									SubnetID: "test-id",
								},
							},
							SubnetID: "test-default-subnet-id",
						},
					},
				},
				service: &corev1.Service{},
			},
			want: "test-default-subnet-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.args.lbaasV2.getSubnetID(tt.args.service, tt.args.svcConf)
			if tt.expectedErr != "" {
				assert.EqualError(t, err, tt.expectedErr)
			}
			if tt.expectedErr == "" {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLbaasV2_getNetworkID(t *testing.T) {
	lbaas := LbaasV2{
		LoadBalancer: LoadBalancer{
			opts: LoadBalancerOpts{
				LBClasses: map[string]*LBClass{
					"lbclassKey": {
						NetworkID: "lb-class-network-id-1234",
					},
				},
				NetworkID: "default-lb-class-networkId",
			},
		},
	}

	type testArg struct {
		service *corev1.Service
	}
	tests := []struct {
		name    string
		lbaas   LbaasV2
		arg     testArg
		want    string
		wantErr string
	}{
		{
			name:  "get network id from service annotation",
			lbaas: LbaasV2{},
			arg: testArg{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							ServiceAnnotationLoadBalancerNetworkID: "subnet-id",
						},
					},
				},
			},
			want:    "subnet-id",
			wantErr: "",
		},
		{
			name:  "get network id from config class",
			lbaas: lbaas,
			arg: testArg{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							ServiceAnnotationLoadBalancerClass: "lbclassKey",
						},
					},
				},
			},
			want:    "lb-class-network-id-1234",
			wantErr: "",
		},
		{
			name:  "get network id from default config",
			lbaas: lbaas,
			arg: testArg{
				service: &corev1.Service{},
			},
			want:    "default-lb-class-networkId",
			wantErr: "",
		},
		{
			name:  "error when loadbalancer class not found",
			lbaas: LbaasV2{},
			arg: testArg{
				service: &corev1.Service{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{
							ServiceAnnotationLoadBalancerClass: "invalid-lb-class",
						},
					},
				},
			},
			want:    "",
			wantErr: "invalid loadbalancer class \"invalid-lb-class\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.lbaas.getNetworkID(tt.arg.service, &serviceConfig{})

			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_buildMonitorCreateOpts(t *testing.T) {
	type testArg struct {
		lbaas   *LbaasV2
		svcConf *serviceConfig
		port    corev1.ServicePort
	}
	tests := []struct {
		name    string
		testArg testArg
		want    v2monitors.CreateOpts
	}{
		{
			name: "test for port protocol udp with ovn provider",
			testArg: testArg{
				lbaas: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
						},
						lb: &gophercloud.ServiceClient{},
					},
				},
				svcConf: &serviceConfig{
					healthMonitorDelay:          6,
					healthMonitorTimeout:        5,
					healthMonitorMaxRetries:     4,
					healthMonitorMaxRetriesDown: 3,
					healthCheckNodePort:         32100,
				},
				port: corev1.ServicePort{
					Protocol: corev1.ProtocolUDP,
				},
			},
			want: v2monitors.CreateOpts{
				Name:           "test for port protocol udp with ovn provider",
				Type:           "UDP-CONNECT",
				Delay:          6,
				Timeout:        5,
				MaxRetries:     4,
				MaxRetriesDown: 3,
			},
		},
		{
			name: "using tcp with ovn provider",
			testArg: testArg{
				lbaas: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
						},
					},
				},
				svcConf: &serviceConfig{
					healthMonitorDelay:          3,
					healthMonitorTimeout:        8,
					healthMonitorMaxRetries:     6,
					healthMonitorMaxRetriesDown: 2,
					healthCheckNodePort:         31200,
				},
				port: corev1.ServicePort{
					Protocol: corev1.ProtocolTCP,
				},
			},
			want: v2monitors.CreateOpts{
				Name:           "using tcp with ovn provider",
				Type:           "TCP",
				Delay:          3,
				Timeout:        8,
				MaxRetries:     6,
				MaxRetriesDown: 2,
			},
		},
		{
			name: "using node port zero",
			testArg: testArg{
				lbaas: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "ovn",
						},
					},
				},
				svcConf: &serviceConfig{
					healthMonitorDelay:          3,
					healthMonitorTimeout:        5,
					healthMonitorMaxRetries:     1,
					healthMonitorMaxRetriesDown: 2,
					healthCheckNodePort:         0,
				},
				port: corev1.ServicePort{
					Protocol: corev1.ProtocolTCP,
				},
			},
			want: v2monitors.CreateOpts{
				Name:           "using node port zero",
				Type:           "TCP",
				Delay:          3,
				Timeout:        5,
				MaxRetries:     1,
				MaxRetriesDown: 2,
			},
		},
		{
			name: "using tcp protocol with not-ovn provider",
			testArg: testArg{
				lbaas: &LbaasV2{
					LoadBalancer{
						opts: LoadBalancerOpts{
							LBProvider: "amphora",
						},
						lb: &gophercloud.ServiceClient{},
					},
				},
				svcConf: &serviceConfig{
					healthMonitorDelay:          3,
					healthMonitorTimeout:        4,
					healthMonitorMaxRetries:     1,
					healthMonitorMaxRetriesDown: 5,
					healthCheckNodePort:         310000,
				},
				port: corev1.ServicePort{
					Protocol: corev1.ProtocolTCP,
				},
			},
			want: v2monitors.CreateOpts{
				Name:           "using tcp protocol with not-ovn provider",
				Type:           "HTTP",
				Delay:          3,
				Timeout:        4,
				MaxRetries:     1,
				MaxRetriesDown: 5,

				URLPath:       "/healthz",
				HTTPMethod:    "GET",
				ExpectedCodes: "200",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.testArg.lbaas.buildMonitorCreateOpts(context.TODO(), tt.testArg.svcConf, tt.testArg.port, tt.name)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestBuildListenerCreateOpt(t *testing.T) {
	svcConf := &serviceConfig{
		connLimit: 100,
		lbName:    "my-lb",
	}
	testCases := []struct {
		name              string
		port              corev1.ServicePort
		svcConf           *serviceConfig
		expectedCreateOpt listeners.CreateOpts
	}{
		{
			name: "Test with basic configuration",
			port: corev1.ServicePort{
				Protocol: "TCP",
				Port:     80,
			},
			svcConf: &serviceConfig{
				connLimit: 100,
				lbName:    "my-lb",
			},
			expectedCreateOpt: listeners.CreateOpts{
				Name:         "Test with basic configuration",
				Protocol:     listeners.ProtocolTCP,
				ProtocolPort: 80,
				ConnLimit:    &svcConf.connLimit,
				Tags:         nil,
			},
		},
		{
			name: "Test with TLSContainerRef and X-Forwarded-For",
			port: corev1.ServicePort{
				Protocol: "TCP",
				Port:     443,
			},
			svcConf: &serviceConfig{
				connLimit:       100,
				lbName:          "my-lb",
				tlsContainerRef: "tls-container-ref",
				keepClientIP:    true,
			},
			expectedCreateOpt: listeners.CreateOpts{
				Name:                   "Test with TLSContainerRef and X-Forwarded-For",
				Protocol:               listeners.ProtocolTerminatedHTTPS,
				ProtocolPort:           443,
				ConnLimit:              &svcConf.connLimit,
				DefaultTlsContainerRef: "tls-container-ref",
				InsertHeaders:          map[string]string{"X-Forwarded-For": "true"},
				Tags:                   nil,
			},
		},
		{
			name: "Test with TLSContainerRef but without X-Forwarded-For",
			port: corev1.ServicePort{
				Protocol: "TCP",
				Port:     443,
			},
			svcConf: &serviceConfig{
				connLimit:       100,
				lbName:          "my-lb",
				tlsContainerRef: "tls-container-ref",
				keepClientIP:    false,
			},
			expectedCreateOpt: listeners.CreateOpts{
				Name:                   "Test with TLSContainerRef but without X-Forwarded-For",
				Protocol:               listeners.ProtocolTerminatedHTTPS,
				ProtocolPort:           443,
				ConnLimit:              &svcConf.connLimit,
				DefaultTlsContainerRef: "tls-container-ref",
				Tags:                   nil,
			},
		},
		{
			name: "Test with supported CIDRs",
			port: corev1.ServicePort{
				Protocol: "TCP",
				Port:     8080,
			},
			svcConf: &serviceConfig{
				connLimit:       100,
				lbName:          "my-lb",
				tlsContainerRef: "tls-container-ref",
				keepClientIP:    true,
				allowedCIDR:     []string{"192.168.1.0/24", "10.0.0.0/8"},
			},
			expectedCreateOpt: listeners.CreateOpts{
				Name:                   "Test with supported CIDRs",
				Protocol:               listeners.ProtocolTerminatedHTTPS,
				ProtocolPort:           8080,
				ConnLimit:              &svcConf.connLimit,
				DefaultTlsContainerRef: "tls-container-ref",
				InsertHeaders:          map[string]string{"X-Forwarded-For": "true"},
				AllowedCIDRs:           svcConf.allowedCIDR,
				Tags:                   nil,
			},
		},
		{
			name: "Test with Protocol forced to HTTP",
			port: corev1.ServicePort{
				Protocol: "TCP",
				Port:     80,
			},
			svcConf: &serviceConfig{
				connLimit:       100,
				lbName:          "my-lb",
				keepClientIP:    true,
				tlsContainerRef: "",
			},
			expectedCreateOpt: listeners.CreateOpts{
				Name:          "Test with Protocol forced to HTTP",
				Protocol:      listeners.ProtocolHTTP,
				ProtocolPort:  80,
				ConnLimit:     &svcConf.connLimit,
				InsertHeaders: map[string]string{"X-Forwarded-For": "true"},
				Tags:          nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lbaas := &LbaasV2{
				LoadBalancer{
					opts: LoadBalancerOpts{
						LBProvider: "not-ovn",
					},
					lb: &gophercloud.ServiceClient{
						ProviderClient: &gophercloud.ProviderClient{},
						Endpoint:       "",
					},
				},
			}
			createOpt := lbaas.buildListenerCreateOpt(context.TODO(), tc.port, tc.svcConf, tc.name)
			assert.Equal(t, tc.expectedCreateOpt, createOpt)

		})
	}
}

func TestFilterNodes(t *testing.T) {
	tests := []struct {
		name           string
		nodeLabels     map[string]string
		service        *corev1.Service
		annotationKey  string
		defaultSetting map[string]string
		nodeFiltered   bool
	}{
		{
			name:       "when no filter is provided, node should be filtered",
			nodeLabels: map[string]string{"k1": "v1"},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{},
			},
			annotationKey:  ServiceAnnotationLoadBalancerNodeSelector,
			defaultSetting: make(map[string]string),
			nodeFiltered:   true,
		},
		{
			name:       "when all key-value filters match, node should be filtered",
			nodeLabels: map[string]string{"k1": "v1", "k2": "v2"},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerNodeSelector: "k1=v1,k2=v2"},
				},
			},
			annotationKey:  ServiceAnnotationLoadBalancerNodeSelector,
			defaultSetting: make(map[string]string),
			nodeFiltered:   true,
		},
		{
			name:       "when all key-value filters match and a key value contains equals sign, node should be filtered",
			nodeLabels: map[string]string{"k1": "v1", "k2": "v2=true"},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerNodeSelector: "k1=v1,k2=v2=true"},
				},
			},
			annotationKey:  ServiceAnnotationLoadBalancerNodeSelector,
			defaultSetting: make(map[string]string),
			nodeFiltered:   true,
		},
		{
			name:       "when all just-key filter match, node should be filtered",
			nodeLabels: map[string]string{"k1": "v1", "k2": "v2"},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerNodeSelector: "k1,k2"},
				},
			},
			annotationKey:  ServiceAnnotationLoadBalancerNodeSelector,
			defaultSetting: make(map[string]string),
			nodeFiltered:   true,
		},
		{
			name:       "when some filters do not match, node should not be filtered",
			nodeLabels: map[string]string{"k1": "v1"},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerNodeSelector: " k1=v1, k2 "},
				},
			},
			annotationKey:  ServiceAnnotationLoadBalancerNodeSelector,
			defaultSetting: make(map[string]string),
			nodeFiltered:   false,
		},
		{
			name:       "when no filter matches, node should not be filtered",
			nodeLabels: map[string]string{"k1": "v1", "k2": "v2"},
			service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerNodeSelector: "k3=v3"},
				},
			},
			annotationKey:  ServiceAnnotationLoadBalancerNodeSelector,
			defaultSetting: make(map[string]string),
			nodeFiltered:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := &corev1.Node{}
			node.Labels = test.nodeLabels

			// TODO: add testArgs
			targetNodeLabels := getKeyValueFromServiceAnnotation(test.service, ServiceAnnotationLoadBalancerNodeSelector, "")

			nodes := []*corev1.Node{node}
			filteredNodes := filterNodes(nodes, targetNodeLabels)

			if test.nodeFiltered {
				assert.Equal(t, nodes, filteredNodes)
			} else {
				assert.Empty(t, filteredNodes)
			}
		})
	}
}

func Test_getProxyProtocolFromServiceAnnotation(t *testing.T) {
	type args struct {
		service *corev1.Service
	}
	tests := []struct {
		name string
		args args
		want *pools.Protocol
	}{
		{
			name: "no proxy protocol",
			args: args{service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerProxyEnabled: "false"},
				},
			}},
			want: nil,
		},
		{
			name: "proxy protocol true",
			args: args{service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerProxyEnabled: "true"},
				},
			}},
			want: ptr.To(pools.ProtocolPROXY),
		},
		{
			name: "proxy protocol v1",
			args: args{service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerProxyEnabled: "v1"},
				},
			}},
			want: ptr.To(pools.ProtocolPROXY),
		},
		{
			name: "proxy protocol v2",
			args: args{service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerProxyEnabled: "v2"},
				},
			}},
			want: ptr.To(pools.ProtocolPROXYV2),
		},
		{
			name: "no proxy protocol annotation",
			args: args{service: &corev1.Service{}},
			want: nil,
		},
		{
			name: "unknown proxy protocol annotation's value",
			args: args{service: &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{ServiceAnnotationLoadBalancerProxyEnabled: "not valid value"},
				},
			}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getProxyProtocolFromServiceAnnotation(tt.args.service)
			assert.Equalf(t, tt.want, got, "getProxyProtocolFromServiceAnnotation(%v)", tt.args.service)
		})
	}
}
