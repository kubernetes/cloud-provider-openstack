package cloudprovider

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/loadbalancers"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/external"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/gophercloud/utils/v2/client"
	"github.com/gophercloud/utils/v2/openstack/clientconfig"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/test/e2e/framework"
	"sigs.k8s.io/yaml"
)

const (
	defaultTimeout  = 5 * time.Minute
	pollInterval    = 10 * time.Second
	namespace       = "octavia-lb-test"
	echoserverImage = "gcr.io/google-containers/echoserver:1.10"
	occmTestTag     = "occm-test"
)

var (
	floatingIP      = os.Getenv("FLOATING_IP")
	gatewayIP       = os.Getenv("GATEWAY_IP")
	lbSubnetName    = getEnvOrDefault("LB_SUBNET_NAME", "private-subnet")
	autoCleanup     = getEnvOrDefault("AUTO_CLEAN_UP", "true") == "true"
	octaviaProvider = os.Getenv("OCTAVIA_PROVIDER")

	// API debug logger
	apiLogger *log.Logger
)

// testContext holds clients and state for LoadBalancer e2e tests
type testContext struct {
	k8sClient       *kubernetes.Clientset
	lbClient        *gophercloud.ServiceClient
	networkClient   *gophercloud.ServiceClient
	provider        *gophercloud.ProviderClient
	createdServices []string
	createdFIPs     []string
	createdLBs      []string
}

// gophercloudLogger implements the logger interface for gophercloud API debugging
type gophercloudLogger struct{}

func (gl *gophercloudLogger) Printf(format string, args ...any) {
	if apiLogger != nil {
		msg := fmt.Sprintf(format, args...)
		// Write detailed API logs to file
		for _, line := range strings.Split(msg, "\n") {
			if line != "" {
				apiLogger.Println(line)
			}
		}
	}
}

// getEnvOrDefault returns environment variable value or default if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// initAPILogger initializes the API debug logger to write to a file
func initAPILogger() error {
	dir := filepath.Join(os.TempDir(), "lb-e2e-logs")
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	fileName := time.Now().Format("20060102-150405") + "-api-debug.log"
	logFile, err := os.Create(filepath.Join(dir, fileName))
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	// Create symlink to latest log
	symLink := filepath.Join(dir, "latest-api-debug.log")
	if _, err := os.Lstat(symLink); err == nil {
		os.Remove(symLink)
	}

	err = os.Symlink(fileName, symLink)
	if err != nil {
		framework.Logf("Warning: Failed to create log symlink: %v", err)
	}

	// Initialize logger with file output only
	apiLogger = log.New(logFile, "", log.LstdFlags)

	framework.Logf("API debug logs will be written to: %s", filepath.Join(dir, fileName))
	framework.Logf("Symlink to latest: %s", symLink)

	return nil
}

// setupTestContext initializes Kubernetes and OpenStack clients with a single auth token
func setupTestContext(ctx context.Context) (*testContext, error) {
	// Initialize API debug logger
	if err := initAPILogger(); err != nil {
		return nil, fmt.Errorf("failed to initialize API logger: %w", err)
	}

	// Setup Kubernetes client
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	// Setup OpenStack clients using clientconfig for proper auth handling
	ao, err := clientconfig.AuthOptions(&clientconfig.ClientOpts{
		EnvPrefix: "OS_", // Standard OpenStack environment variable prefix
		AuthInfo: &clientconfig.AuthInfo{
			AuthURL:     os.Getenv("OS_AUTH_URL"),
			Username:    os.Getenv("OS_USERNAME"),
			Password:    os.Getenv("OS_PASSWORD"),
			ProjectName: os.Getenv("OS_PROJECT_NAME"),
			ProjectID:   os.Getenv("OS_PROJECT_ID"),
			DomainName:  os.Getenv("OS_USER_DOMAIN_NAME"),
			// Support for application credentials
			ApplicationCredentialID:     os.Getenv("OS_APPLICATION_CREDENTIAL_ID"),
			ApplicationCredentialName:   os.Getenv("OS_APPLICATION_CREDENTIAL_NAME"),
			ApplicationCredentialSecret: os.Getenv("OS_APPLICATION_CREDENTIAL_SECRET"),
			// Support for token auth
			Token: os.Getenv("OS_TOKEN"),
		},
		RegionName: os.Getenv("OS_REGION_NAME"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get auth options: %w", err)
	}

	// Long-running tests, need to be able to renew token
	ao.AllowReauth = true

	provider, err := openstack.NewClient(ao.IdentityEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider client: %w", err)
	}

	// Set user agent
	provider.UserAgent.Prepend("lb-e2e-test/1.0")

	// Configure TLS (support insecure endpoints for dev/test environments)
	insecure := getEnvOrDefault("OS_INSECURE", "false") == "true"
	tlsConfig := &tls.Config{
		InsecureSkipVerify: insecure,
	}

	// Enable API debug logging with custom round tripper
	provider.HTTPClient = http.Client{
		Transport: &client.RoundTripper{
			MaxRetries: 5, // Retry on transient failures
			Rt: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
			Logger: &gophercloudLogger{},
		},
	}

	framework.Logf("Authenticating with OpenStack...")
	if insecure {
		framework.Logf("Warning: TLS certificate verification is disabled (OS_INSECURE=true)")
	}

	err = openstack.Authenticate(ctx, provider, *ao)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	framework.Logf("Successfully authenticated with OpenStack")

	// Create endpoint options
	eo := gophercloud.EndpointOpts{
		Region: os.Getenv("OS_REGION_NAME"),
	}

	// Create load balancer client
	lbClient, err := openstack.NewLoadBalancerV2(provider, eo)
	if err != nil {
		return nil, fmt.Errorf("failed to create load balancer client: %w", err)
	}

	// Create network client
	networkClient, err := openstack.NewNetworkV2(provider, eo)
	if err != nil {
		return nil, fmt.Errorf("failed to create network client: %w", err)
	}

	return &testContext{
		k8sClient:       k8sClient,
		lbClient:        lbClient,
		networkClient:   networkClient,
		provider:        provider,
		createdServices: []string{},
		createdFIPs:     []string{},
		createdLBs:      []string{},
	}, nil
}

// createNamespace creates the test namespace
func createNamespace(ctx context.Context, tstCtx *testContext) error {
	framework.Logf("Creating namespace %s", namespace)

	namespaceYAML := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespace)

	ns, err := parseYAMLNamespace(namespaceYAML)
	if err != nil {
		return err
	}

	_, err = tstCtx.k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

// createDeployment creates the echoserver deployment
func createDeployment(ctx context.Context, tstCtx *testContext) error {
	framework.Logf("Creating echoserver deployment")

	deploymentYAML := fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echoserver
  namespace: %s
  labels:
    run: echoserver
spec:
  replicas: 1
  selector:
    matchLabels:
      run: echoserver
  template:
    metadata:
      labels:
        run: echoserver
    spec:
      containers:
      - name: echoserver
        image: %s
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 8080
`, namespace, echoserverImage)

	deployment, err := parseYAMLDeployment(deploymentYAML)
	if err != nil {
		return err
	}

	_, err = tstCtx.k8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

// cleanupResources deletes all created resources
func cleanupResources(ctx context.Context, tstCtx *testContext) {
	framework.Logf("Cleaning up resources")

	// Delete services
	for _, svcName := range tstCtx.createdServices {
		framework.Logf("Deleting service %s", svcName)
		err := tstCtx.k8sClient.CoreV1().Services(namespace).Delete(ctx, svcName, metav1.DeleteOptions{})
		if err != nil {
			framework.Logf("Error deleting service %s: %v", svcName, err)
		}
	}

	// Delete floating IPs
	for _, fipID := range tstCtx.createdFIPs {
		framework.Logf("Deleting floating IP %s", fipID)
		err := floatingips.Delete(ctx, tstCtx.networkClient, fipID).ExtractErr()
		if err != nil {
			framework.Logf("Error deleting floating IP %s: %v", fipID, err)
		}
	}

	// Delete load balancers
	for _, lbID := range tstCtx.createdLBs {
		framework.Logf("Deleting load balancer %s", lbID)
		err := loadbalancers.Delete(ctx, tstCtx.lbClient, lbID, loadbalancers.DeleteOpts{Cascade: true}).ExtractErr()
		if err != nil {
			framework.Logf("Error deleting load balancer %s: %v", lbID, err)
		}
	}

	// Delete deployment
	framework.Logf("Deleting deployment")
	err := tstCtx.k8sClient.AppsV1().Deployments(namespace).Delete(ctx, "echoserver", metav1.DeleteOptions{})
	if err != nil {
		framework.Logf("Error deleting deployment: %v", err)
	}
}

// createService creates a Kubernetes service and tracks it for cleanup
func createService(ctx context.Context, tstCtx *testContext, svc *corev1.Service) (*corev1.Service, error) {
	framework.Logf("Creating service %s", svc.Name)
	createdSvc, err := tstCtx.k8sClient.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	tstCtx.createdServices = append(tstCtx.createdServices, svc.Name)
	return createdSvc, nil
}

// deleteService deletes a Kubernetes service
func deleteService(ctx context.Context, tstCtx *testContext, name string) error {
	framework.Logf("Deleting service %s", name)
	return tstCtx.k8sClient.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// waitForServiceAddress waits for a LoadBalancer service to get an external IP
func waitForServiceAddress(ctx context.Context, tstCtx *testContext, serviceName string) (string, error) {
	framework.Logf("Waiting for service %s to get LoadBalancer IP", serviceName)
	var ipAddr string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true, func(ctx context.Context) (bool, error) {
		svc, err := tstCtx.k8sClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			ipAddr = svc.Status.LoadBalancer.Ingress[0].IP
			if ipAddr != "" {
				framework.Logf("Service %s got IP: %s", serviceName, ipAddr)
				return true, nil
			}
		}
		return false, nil
	})
	return ipAddr, err
}

// waitForAddressAccessible waits for an IP address to be accessible on port 80
func waitForAddressAccessible(ctx context.Context, ipAddr string) error {
	framework.Logf("Waiting for IP %s to be accessible", ipAddr)
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, defaultTimeout, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s", ipAddr), nil)
		if err != nil {
			return false, nil
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK, nil
	})
}

// waitForLoadBalancer waits for a load balancer to reach ACTIVE provisioning status
func waitForLoadBalancer(ctx context.Context, tstCtx *testContext, lbID string) error {
	framework.Logf("Waiting for load balancer %s to be ACTIVE", lbID)
	consecutiveActive := 0
	return wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true, func(ctx context.Context) (bool, error) {
		lb, err := loadbalancers.Get(ctx, tstCtx.lbClient, lbID).Extract()
		if err != nil {
			return false, err
		}

		if lb.ProvisioningStatus == "ACTIVE" {
			consecutiveActive++
			if consecutiveActive >= 2 {
				framework.Logf("Load balancer %s is ACTIVE", lbID)
				return true, nil
			}
		} else {
			consecutiveActive = 0
		}
		return false, nil
	})
}

// waitForServiceDeleted waits for a service to be deleted
func waitForServiceDeleted(ctx context.Context, tstCtx *testContext, serviceName string) error {
	framework.Logf("Waiting for service %s to be deleted", serviceName)
	return wait.PollUntilContextTimeout(ctx, 3*time.Second, defaultTimeout, true, func(ctx context.Context) (bool, error) {
		_, err := tstCtx.k8sClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil && strings.Contains(err.Error(), "not found") {
			framework.Logf("Service %s deleted", serviceName)
			return true, nil
		}
		return false, nil
	})
}

// getServiceLBAnnotation gets the load balancer ID from service annotations
func getServiceLBAnnotation(ctx context.Context, tstCtx *testContext, serviceName string) (string, error) {
	var lbID string
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 15*time.Second, true, func(ctx context.Context) (bool, error) {
		svc, err := tstCtx.k8sClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if id, ok := svc.Annotations["loadbalancer.openstack.org/load-balancer-id"]; ok && id != "" {
			lbID = id
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return "", fmt.Errorf("service annotation loadbalancer.openstack.org/load-balancer-id not found for service %s: %w", serviceName, err)
	}
	return lbID, nil
}

// checkLBTags checks if a service tag exists in load balancer tags
func checkLBTags(ctx context.Context, tstCtx *testContext, lbID, svcName string) (bool, error) {
	lb, err := loadbalancers.Get(ctx, tstCtx.lbClient, lbID).Extract()
	if err != nil {
		return false, err
	}

	pattern := regexp.MustCompile(fmt.Sprintf(`(^|\s)kube_service_.+?%s($|\s)`, regexp.QuoteMeta(svcName)))
	for _, tag := range lb.Tags {
		if pattern.MatchString(tag) {
			return true, nil
		}
	}
	return false, nil
}

// getLoadBalancerStatus gets load balancer status tree using GetStatuses API
func getLoadBalancerStatus(ctx context.Context, tstCtx *testContext, lbID string) (*loadbalancers.StatusTree, error) {
	statusTree, err := loadbalancers.GetStatuses(ctx, tstCtx.lbClient, lbID).Extract()
	if err != nil {
		return nil, err
	}
	return statusTree, nil
}

// getExternalNetworkID finds an external network ID
func getExternalNetworkID(ctx context.Context, tstCtx *testContext) (string, error) {
	type NetworkWithExternalExt struct {
		networks.Network
		external.NetworkExternalExt
	}
	var allNetworks []NetworkWithExternalExt

	networkPages, err := networks.List(tstCtx.networkClient, networks.ListOpts{}).AllPages(ctx)
	if err != nil {
		return "", err
	}

	err = networks.ExtractNetworksInto(networkPages, &allNetworks)
	if err != nil {
		return "", err
	}

	for _, net := range allNetworks {
		if net.External && len(net.Subnets) > 0 {
			return net.ID, nil
		}
	}

	return "", fmt.Errorf("no external network found")
}

// getSubnetID gets subnet ID by name
func getSubnetID(ctx context.Context, tstCtx *testContext, subnetName string) (string, error) {
	subnetPages, err := subnets.List(tstCtx.networkClient, subnets.ListOpts{Name: subnetName}).AllPages(ctx)
	if err != nil {
		return "", err
	}

	subnetList, err := subnets.ExtractSubnets(subnetPages)
	if err != nil {
		return "", err
	}

	if len(subnetList) == 0 {
		return "", fmt.Errorf("subnet %s not found", subnetName)
	}

	return subnetList[0].ID, nil
}

// parseYAMLService parses a YAML service definition into a Service object
func parseYAMLService(yamlStr string) (*corev1.Service, error) {
	svc := &corev1.Service{}
	err := yaml.Unmarshal([]byte(yamlStr), svc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML service: %w", err)
	}
	return svc, nil
}

// parseYAMLNamespace parses a YAML namespace definition into a Namespace object
func parseYAMLNamespace(yamlStr string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := yaml.Unmarshal([]byte(yamlStr), ns)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML namespace: %w", err)
	}
	return ns, nil
}

// parseYAMLDeployment parses a YAML deployment definition into a Deployment object
func parseYAMLDeployment(yamlStr string) (*appsv1.Deployment, error) {
	deploy := &appsv1.Deployment{}
	err := yaml.Unmarshal([]byte(yamlStr), deploy)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML deployment: %w", err)
	}
	return deploy, nil
}

// getMemberPorts gets all member protocol ports from a load balancer's pools
func getMemberPorts(ctx context.Context, tstCtx *testContext, lbID string) ([]int, error) {
	poolPages, err := pools.List(tstCtx.lbClient, pools.ListOpts{LoadbalancerID: lbID}).AllPages(ctx)
	if err != nil {
		return nil, err
	}
	poolList, err := pools.ExtractPools(poolPages)
	if err != nil {
		return nil, err
	}

	var memberPorts []int
	for _, pool := range poolList {
		// List all members for this pool using pools.ListMembers
		memberPages, err := pools.ListMembers(tstCtx.lbClient, pool.ID, nil).AllPages(ctx)
		if err != nil {
			framework.Logf("Warning: failed to list members for pool %s: %v", pool.ID, err)
			continue
		}
		memberList, err := pools.ExtractMembers(memberPages)
		if err != nil {
			framework.Logf("Warning: failed to extract members for pool %s: %v", pool.ID, err)
			continue
		}

		for _, member := range memberList {
			memberPorts = append(memberPorts, member.ProtocolPort)
		}
	}
	return memberPorts, nil
}

// getServiceNodePorts gets all NodePort values from a service
func getServiceNodePorts(ctx context.Context, tstCtx *testContext, serviceName string) ([]int32, error) {
	svc, err := tstCtx.k8sClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	var nodePorts []int32
	for _, port := range svc.Spec.Ports {
		if port.NodePort != 0 {
			nodePorts = append(nodePorts, port.NodePort)
		}
	}
	return nodePorts, nil
}
