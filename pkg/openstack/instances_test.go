/*
Copyright 2022 The Kubernetes Authors.

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
	"net"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestBuildAddressSortOrderList(t *testing.T) {
	var emptyList []*net.IPNet

	_, cidrIPv4, _ := net.ParseCIDR("192.168.0.0/16")
	_, cidrIPv6, _ := net.ParseCIDR("2001:4800:790e::/64")

	emptyOption := ""
	multipleInvalidOptions := "InvalidOption, AnotherInvalidOption"
	multipleOptionsWithInvalidOption := fmt.Sprintf("%s, %s, %s", cidrIPv4, multipleInvalidOptions, cidrIPv6)

	tests := map[string][]*net.IPNet{
		emptyOption:                      emptyList,
		multipleInvalidOptions:           emptyList,
		multipleOptionsWithInvalidOption: {cidrIPv4, cidrIPv6},
	}

	for option, want := range tests {
		actual := buildAddressSortOrderList(option)
		if !reflect.DeepEqual(want, actual) {
			t.Errorf("assignSortOrderPriorities returned incorrect value for '%v', want %+v but got %+v", option, want, actual)
		}
	}
}

func TestGetSortPriority(t *testing.T) {
	_, cidrIPv4, _ := net.ParseCIDR("192.168.100.0/24")
	_, cidrIPv6, _ := net.ParseCIDR("2001:4800:790e::/64")

	list := []*net.IPNet{cidrIPv4, cidrIPv6}
	t.Log(list)
	tests := map[string]int{
		"":                     noSortPriority,
		"some-host.exam.ple":   noSortPriority,
		"2001:4800:790e::82a8": 1,
		"2001:cafe:babe::82a8": noSortPriority,
		"192.168.100.200":      2,
		"192.168.101.123":      noSortPriority,
	}

	for option, want := range tests {
		actual := getSortPriority(list, option)
		if !reflect.DeepEqual(want, actual) {
			t.Errorf("assignSortOrderPriorities returned incorrect value for '%v', want %+v but got %+v", option, want, actual)
		}
	}
}

func executeSortNodeAddressesTest(t *testing.T, addressSortOrder string, want []v1.NodeAddress) {
	addresses := []v1.NodeAddress{
		{Type: v1.NodeExternalIP, Address: "2001:4800:780e:510:be76:4eff:fe04:84a8"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:84a8"},
		{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.32"},
		{Type: v1.NodeInternalIP, Address: "172.16.0.1"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:790e:510:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.31"},
		{Type: v1.NodeInternalIP, Address: "50.56.176.37"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.36"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.novalocal"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.99"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.35"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.exam.ple"},
	}

	sortNodeAddresses(addresses, addressSortOrder)

	t.Logf("addresses are %v", addresses)
	if !reflect.DeepEqual(want, addresses) {
		t.Fatalf("sortNodeAddresses returned incorrect value, want %v", want)
	}
}

func TestSortNodeAddressesWithAnInvalidCIDR(t *testing.T) {
	addressSortOrder := "10.0.0.0/244"

	want := []v1.NodeAddress{
		{Type: v1.NodeExternalIP, Address: "2001:4800:780e:510:be76:4eff:fe04:84a8"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:84a8"},
		{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.32"},
		{Type: v1.NodeInternalIP, Address: "172.16.0.1"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:790e:510:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.31"},
		{Type: v1.NodeInternalIP, Address: "50.56.176.37"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.36"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.novalocal"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.99"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.35"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.exam.ple"},
	}

	executeSortNodeAddressesTest(t, addressSortOrder, want)
}

func TestSortNodeAddressesWithOneIPv4CIDR(t *testing.T) {
	addressSortOrder := "10.0.0.0/8"

	want := []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: "10.0.0.31"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.32"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:780e:510:be76:4eff:fe04:84a8"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:84a8"},
		{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "172.16.0.1"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:790e:510:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "50.56.176.37"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.36"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.novalocal"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.99"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.35"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.exam.ple"},
	}

	executeSortNodeAddressesTest(t, addressSortOrder, want)
}

func TestSortNodeAddressesWithOneIPv6CIDR(t *testing.T) {
	addressSortOrder := "fd08:1374:fcee:916b::/64"

	want := []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:84a8"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:780e:510:be76:4eff:fe04:84a8"},
		{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.32"},
		{Type: v1.NodeInternalIP, Address: "172.16.0.1"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:790e:510:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.31"},
		{Type: v1.NodeInternalIP, Address: "50.56.176.37"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.36"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.novalocal"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.99"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.35"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.exam.ple"},
	}

	executeSortNodeAddressesTest(t, addressSortOrder, want)
}

func TestSortNodeAddressesWithMultipleCIDRs(t *testing.T) {
	addressSortOrder := "10.0.0.0/8, 172.16.0.0/16, 192.168.0.0/24, fd08:1374:fcee:916b::/64, 50.56.176.0/24, 2001:cafe:babe::/64"

	want := []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: "10.0.0.31"},
		{Type: v1.NodeInternalIP, Address: "10.0.0.32"},
		{Type: v1.NodeInternalIP, Address: "172.16.0.1"},
		{Type: v1.NodeInternalIP, Address: "192.168.0.1"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:82a8"},
		{Type: v1.NodeInternalIP, Address: "fd08:1374:fcee:916b:be76:4eff:fe04:84a8"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.35"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.36"},
		{Type: v1.NodeInternalIP, Address: "50.56.176.37"},
		{Type: v1.NodeExternalIP, Address: "50.56.176.99"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:780e:510:be76:4eff:fe04:84a8"},
		{Type: v1.NodeExternalIP, Address: "2001:4800:790e:510:be76:4eff:fe04:82a8"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.novalocal"},
		{Type: v1.NodeHostName, Address: "a1-yinvcez57-0-bvynoyawrhcg-kube-minion-fg5i4jwcc2yy.exam.ple"},
	}

	executeSortNodeAddressesTest(t, addressSortOrder, want)
}
