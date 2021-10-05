/*
Copyright 2014 The Kubernetes Authors.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	cpoutil "k8s.io/cloud-provider-openstack/pkg/util"
)

func TestGetTlsServicePorts(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myservice",
			Namespace: "a-namespace",
		},
	}

	var ports []int32

	service.ObjectMeta.Annotations = map[string]string{
	}
	ports = GetTlsServicePorts(service)
	if len(ports) != 0 {
		t.Errorf("no annotation should give empty ports: ports = %v", ports)
	}

	service.ObjectMeta.Annotations = map[string]string{
		"loadbalancer.openstack.org/service-ssl-ports": "xxx 443",
	}
	ports = GetTlsServicePorts(service)
	if len(ports) != 0 {
		t.Errorf("wrong annotation format should give empty ports: ports = %v", ports)
	}

	service.ObjectMeta.Annotations = map[string]string{
		"loadbalancer.openstack.org/service-ssl-ports": "443",
	}
	ports = GetTlsServicePorts(service)
	if len(ports) != 1 {
		t.Errorf("ports should not be empty: ports = %v", ports)
	}
	if ports[0] != 443 {
		t.Errorf("ports should be [443]: ports = %v", ports)
	}

	service.ObjectMeta.Annotations = map[string]string{
		"loadbalancer.openstack.org/service-ssl-ports": "443,8443",
	}
	ports = GetTlsServicePorts(service)
	if len(ports) != 2 {
		t.Errorf("ports should not be empty: %v", ports)
	}

	if !cpoutil.ContainsInt(ports, 443) {
		t.Errorf("port 443 should be excluded: %v", ports)
	}

	if !cpoutil.ContainsInt(ports, 8443) {
		t.Errorf("port 8443 should be excluded: %v", ports)
	}

	if cpoutil.ContainsInt(ports, 1002) {
		t.Errorf("port 1002 should not be excluded: %v", ports)
	}

}
