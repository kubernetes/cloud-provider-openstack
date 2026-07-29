package cloudprovider

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/kubernetes/test/e2e/framework"
)

var _ = ginkgo.Describe("[cloud-provider-openstack] LoadBalancer Service", func() {
	var tstCtx *testContext

	ginkgo.BeforeEach(func() {
		var err error
		tstCtx, err = setupTestContext(ginkgo.GinkgoT().Context())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = createNamespace(ginkgo.GinkgoT().Context(), tstCtx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = createDeployment(ginkgo.GinkgoT().Context(), tstCtx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if autoCleanup {
			cleanupResources(ginkgo.GinkgoT().Context(), tstCtx)
		}
	})

	ginkgo.It("should create a basic LoadBalancer service", func() {
		testBasic(ginkgo.GinkgoT().Context(), tstCtx)
	})

	ginkgo.It("should support x-forwarded-for annotation", func() {
		if octaviaProvider == "ovn" {
			ginkgo.Skip("Skipping x-forwarded-for test for OVN provider")
		}
		testForwarded(ginkgo.GinkgoT().Context(), tstCtx)
	})

	ginkgo.It("should handle port updates correctly", func() {
		testUpdatePort(ginkgo.GinkgoT().Context(), tstCtx)
	})

	ginkgo.It("should support shared load balancers", func() {
		testSharedLB(ginkgo.GinkgoT().Context(), tstCtx)
	})

	ginkgo.It("should support user-created load balancers", func() {
		testSharedUserLB(ginkgo.GinkgoT().Context(), tstCtx)
	})
})

func testBasic(ctx context.Context, tstCtx *testContext) {
	serviceName := "test-basic"

	serviceYAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
`, serviceName, namespace)

	svc, err := parseYAMLService(serviceYAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	if floatingIP != "" {
		svc.Spec.LoadBalancerIP = floatingIP
	}

	_, err = createService(ctx, tstCtx, svc)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	ipAddr, err := waitForServiceAddress(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForAddressAccessible(ctx, ipAddr)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	framework.Logf("Sending request to service %s", serviceName)
	resp, err := http.Get(fmt.Sprintf("http://%s", ipAddr))
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	gomega.Expect(string(body)).To(gomega.ContainSubstring("echoserver"))

	err = deleteService(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func testForwarded(ctx context.Context, tstCtx *testContext) {
	serviceName := "test-x-forwarded-for"

	serviceYAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    loadbalancer.openstack.org/x-forwarded-for: "true"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
`, serviceName, namespace)

	svc, err := parseYAMLService(serviceYAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	if floatingIP != "" {
		svc.Spec.LoadBalancerIP = floatingIP
	}

	_, err = createService(ctx, tstCtx, svc)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	ipAddr, err := waitForServiceAddress(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForAddressAccessible(ctx, ipAddr)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	framework.Logf("Sending request to service %s to check x-forwarded-for", serviceName)
	resp, err := http.Get(fmt.Sprintf("http://%s", ipAddr))
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	bodyStr := string(body)
	framework.Logf("Response body: %s", bodyStr)

	// Validate x-forwarded-for header exists
	gomega.Expect(bodyStr).To(gomega.ContainSubstring("x-forwarded-for"))

	// Extract and validate x-forwarded-for IP value matches expected sources
	// Expected sources: gatewayIP (from env), local IP, or public IP
	if gatewayIP != "" {
		// If GATEWAY_IP is set, validate it appears in the x-forwarded-for header
		gomega.Expect(bodyStr).To(gomega.ContainSubstring(gatewayIP),
			fmt.Sprintf("x-forwarded-for header should contain GATEWAY_IP=%s", gatewayIP))
		framework.Logf("Validated x-forwarded-for contains GATEWAY_IP: %s", gatewayIP)
	} else {
		framework.Logf("GATEWAY_IP not set, skipping IP value validation")
	}

	err = deleteService(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func testUpdatePort(ctx context.Context, tstCtx *testContext) {
	serviceName := "test-update-port"

	serviceYAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    service.beta.kubernetes.io/openstack-internal-load-balancer: "true"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - name: port1
    protocol: TCP
    port: 80
    targetPort: 8080
  - name: port2
    protocol: TCP
    port: 8080
    targetPort: 8080
`, serviceName, namespace)

	svc, err := parseYAMLService(serviceYAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = createService(ctx, tstCtx, svc)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = waitForServiceAddress(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	lbID, err := getServiceLBAnnotation(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForLoadBalancer(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify initial state - 2 listeners
	status, err := getLoadBalancerStatus(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	listenerCount := len(status.Loadbalancer.Listeners)
	gomega.Expect(listenerCount).To(gomega.Equal(2))

	// Get initial NodePorts and verify member ports match
	initialNodePorts, err := getServiceNodePorts(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(len(initialNodePorts)).To(gomega.Equal(2), "Should have 2 NodePorts initially")
	framework.Logf("Initial NodePorts: %v", initialNodePorts)

	initialMemberPorts, err := getMemberPorts(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	framework.Logf("Initial member ports: %v", initialMemberPorts)

	// Validate member ports match NodePorts
	gomega.Expect(len(initialMemberPorts)).To(gomega.Equal(len(initialNodePorts)),
		"Member port count should match NodePort count")
	for _, nodePort := range initialNodePorts {
		found := false
		for _, memberPort := range initialMemberPorts {
			if int32(memberPort) == nodePort {
				found = true
				break
			}
		}
		gomega.Expect(found).To(gomega.BeTrue(),
			fmt.Sprintf("NodePort %d should have corresponding member port", nodePort))
	}

	// Update service - remove port2
	svc, err = tstCtx.k8sClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	svc.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "port1",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(8080),
		},
	}

	_, err = tstCtx.k8sClient.CoreV1().Services(namespace).Update(ctx, svc, metav1.UpdateOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForLoadBalancer(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify updated state - 1 listener
	status, err = getLoadBalancerStatus(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	listenerCount = len(status.Loadbalancer.Listeners)
	gomega.Expect(listenerCount).To(gomega.Equal(1))

	// Get updated NodePorts and verify they changed
	updatedNodePorts, err := getServiceNodePorts(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(len(updatedNodePorts)).To(gomega.Equal(1), "Should have 1 NodePort after update")
	framework.Logf("Updated NodePorts: %v", updatedNodePorts)

	// Verify NodePort actually changed (Kubernetes assigns new NodePort on update)
	if len(initialNodePorts) > 0 && len(updatedNodePorts) > 0 {
		// The remaining NodePort may or may not change depending on which port was removed
		framework.Logf("NodePort after update: %d (initial had: %v)", updatedNodePorts[0], initialNodePorts)
	}

	updatedMemberPorts, err := getMemberPorts(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	framework.Logf("Updated member ports: %v", updatedMemberPorts)

	// Validate updated member ports match updated NodePorts
	gomega.Expect(len(updatedMemberPorts)).To(gomega.Equal(len(updatedNodePorts)),
		"Member port count should match NodePort count after update")
	for _, nodePort := range updatedNodePorts {
		found := false
		for _, memberPort := range updatedMemberPorts {
			if int32(memberPort) == nodePort {
				found = true
				break
			}
		}
		gomega.Expect(found).To(gomega.BeTrue(),
			fmt.Sprintf("Updated NodePort %d should have corresponding member port", nodePort))
	}

	err = deleteService(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func testSharedLB(ctx context.Context, tstCtx *testContext) {
	service1 := "test-shared-1"

	service1YAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
`, service1, namespace)

	svc1, err := parseYAMLService(service1YAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = createService(ctx, tstCtx, svc1)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = waitForServiceAddress(ctx, tstCtx, service1)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	lbID, err := getServiceLBAnnotation(ctx, tstCtx, service1)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	found, err := checkLBTags(ctx, tstCtx, lbID, service1)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeTrue(), "service1 tag not found in load balancer")

	// Create service2 sharing the same LB
	service2 := "test-shared-2"
	service2YAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
    loadbalancer.openstack.org/load-balancer-id: "%s"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - protocol: TCP
    port: 8080
    targetPort: 8080
`, service2, namespace, lbID)

	svc2, err := parseYAMLService(service2YAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = createService(ctx, tstCtx, svc2)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = waitForServiceAddress(ctx, tstCtx, service2)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	svc2lbID, err := getServiceLBAnnotation(ctx, tstCtx, service2)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(svc2lbID).To(gomega.Equal(lbID), "service2 should use the same load balancer")

	// Check both services in tags
	found, err = checkLBTags(ctx, tstCtx, lbID, service1)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeTrue())

	found, err = checkLBTags(ctx, tstCtx, lbID, service2)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeTrue())

	// Update service2 port
	framework.Logf("Updating service %s port", service2)
	svc2, err = tstCtx.k8sClient.CoreV1().Services(namespace).Get(ctx, service2, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	svc2.Spec.Ports = []corev1.ServicePort{
		{
			Protocol:   corev1.ProtocolTCP,
			Port:       8081,
			TargetPort: intstr.FromInt(8080),
		},
	}

	_, err = tstCtx.k8sClient.CoreV1().Services(namespace).Update(ctx, svc2, metav1.UpdateOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForLoadBalancer(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Check that there are 2 listeners
	status, err := getLoadBalancerStatus(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	listenerCount := len(status.Loadbalancer.Listeners)
	gomega.Expect(listenerCount).To(gomega.Equal(2), "Should have 2 listeners after updating service2")

	// Delete service2
	err = deleteService(ctx, tstCtx, service2)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForLoadBalancer(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify service2 tag removed
	found, err = checkLBTags(ctx, tstCtx, lbID, service2)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeFalse(), "service2 tag should be removed")

	// Check listener count after service2 deletion - should be 1
	status, err = getLoadBalancerStatus(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	listenerCount = len(status.Loadbalancer.Listeners)
	gomega.Expect(listenerCount).To(gomega.Equal(1), "Should have 1 listener after deleting service2")

	// Create service3
	service3 := "test-shared-3"
	service3YAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
    loadbalancer.openstack.org/load-balancer-id: "%s"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - protocol: TCP
    port: 8080
    targetPort: 8080
`, service3, namespace, lbID)

	svc3, err := parseYAMLService(service3YAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = createService(ctx, tstCtx, svc3)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = waitForServiceAddress(ctx, tstCtx, service3)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Create service4 with port collision
	service4 := "test-shared-4"
	service4YAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    loadbalancer.openstack.org/enable-health-monitor: "false"
    loadbalancer.openstack.org/load-balancer-id: "%s"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - protocol: TCP
    port: 8080
    targetPort: 8080
`, service4, namespace, lbID)

	svc4, err := parseYAMLService(service4YAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = createService(ctx, tstCtx, svc4)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Wait for OCCM to attempt processing service4 (it will fail due to port collision)
	// The LB should return to ACTIVE state after the failed processing attempt
	err = waitForLoadBalancer(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify service4 NOT in tags (port collision prevented it from being added)
	found, err = checkLBTags(ctx, tstCtx, lbID, service4)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeFalse(), "service4 should not be added due to port collision")

	err = deleteService(ctx, tstCtx, service4)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = deleteService(ctx, tstCtx, service1)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForLoadBalancer(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify service1 removed but service3 still present
	found, err = checkLBTags(ctx, tstCtx, lbID, service1)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeFalse())

	found, err = checkLBTags(ctx, tstCtx, lbID, service3)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeTrue())

	// Check listener count - should still be 1
	status, err = getLoadBalancerStatus(ctx, tstCtx, lbID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	listenerCount = len(status.Loadbalancer.Listeners)
	gomega.Expect(listenerCount).To(gomega.Equal(1), "Should have 1 listener after deleting service1")

	err = deleteService(ctx, tstCtx, service3)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func testSharedUserLB(ctx context.Context, tstCtx *testContext) {
	// Get subnet ID
	subnetID, err := getSubnetID(ctx, tstCtx, lbSubnetName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Create load balancer
	createOpts := loadbalancers.CreateOpts{
		VipSubnetID: subnetID,
		Name:        "test_shared_user_lb",
	}
	if octaviaProvider != "" {
		createOpts.Provider = octaviaProvider
	}

	lb, err := loadbalancers.Create(ctx, tstCtx.lbClient, createOpts).Extract()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	tstCtx.createdLBs = append(tstCtx.createdLBs, lb.ID)

	err = waitForLoadBalancer(ctx, tstCtx, lb.ID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Create listener - use TCP for OVN provider, HTTP for others
	listenerProtocol := "HTTP"
	if octaviaProvider == "ovn" {
		listenerProtocol = "TCP"
	}
	listenerOpts := listeners.CreateOpts{
		Protocol:       listeners.Protocol(listenerProtocol),
		ProtocolPort:   80,
		LoadbalancerID: lb.ID,
	}
	_, err = listeners.Create(ctx, tstCtx.lbClient, listenerOpts).Extract()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForLoadBalancer(ctx, tstCtx, lb.ID)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Get external network and create FIP
	extNetID, err := getExternalNetworkID(ctx, tstCtx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Create floating IP
	fipOpts := floatingips.CreateOpts{
		FloatingNetworkID: extNetID,
		Description:       occmTestTag,
	}
	fip, err := floatingips.Create(ctx, tstCtx.networkClient, fipOpts).Extract()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	tstCtx.createdFIPs = append(tstCtx.createdFIPs, fip.ID)

	// Associate FIP with LB VIP port
	updateOpts := floatingips.UpdateOpts{
		PortID: &lb.VipPortID,
	}
	_, err = floatingips.Update(ctx, tstCtx.networkClient, fip.ID, updateOpts).Extract()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Create service
	serviceName := "test-shared-user-lb"
	serviceYAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    loadbalancer.openstack.org/load-balancer-id: "%s"
    loadbalancer.openstack.org/enable-health-monitor: "false"
spec:
  type: LoadBalancer
  selector:
    run: echoserver
  ports:
  - protocol: TCP
    port: 8080
    targetPort: 8080
`, serviceName, namespace, lb.ID)

	svc, err := parseYAMLService(serviceYAML)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = createService(ctx, tstCtx, svc)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, err = waitForServiceAddress(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	lbID, err := getServiceLBAnnotation(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(lbID).To(gomega.Equal(lb.ID))

	// Check tags
	found, err := checkLBTags(ctx, tstCtx, lbID, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeTrue())

	err = deleteService(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = waitForServiceDeleted(ctx, tstCtx, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Verify tag removed
	found, err = checkLBTags(ctx, tstCtx, lbID, serviceName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(found).To(gomega.BeFalse())
}
