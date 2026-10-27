/*
Copyright 2026 The Kubernetes Authors.

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

package cloudprovider

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

const (
	uninitializedTaint = "node.cloudprovider.kubernetes.io/uninitialized"
	occmName           = "openstack-cloud-controller-manager"
	occmSelector       = "k8s-app=" + occmName
	dnsSelector        = "k8s-app=kube-dns"
)

type bootstrapTest struct {
	t                    *testing.T
	ctx                  context.Context
	client               kubernetes.Interface
	hostname, address    string
	dynamic              dynamic.Interface
	mapper               meta.RESTMapper
	runtime              runtimeapi.RuntimeServiceClient
	artifacts, manifests string
	stage, clusterDNS    string
	nodeUID              types.UID
	timeout              time.Duration
}

// TestOCCMBootstrap requires the disposable DevStack/k3s environment prepared by
// test-occm-e2e.yaml. Ordinary go test runs must never access a cluster.
func TestOCCMBootstrap(t *testing.T) {
	if os.Getenv("OCCM_BOOTSTRAP_E2E") != "true" {
		t.Skip("set OCCM_BOOTSTRAP_E2E=true in a fresh DevStack/k3s environment")
	}
	required := func(name string) string {
		t.Helper()
		value := os.Getenv("OCCM_BOOTSTRAP_" + name)
		if value == "" {
			t.Fatalf("OCCM_BOOTSTRAP_%s is required", name)
		}
		return value
	}
	b := &bootstrapTest{
		t:        t,
		hostname: required("HOSTNAME"), address: required("ADDRESS"),
		artifacts: required("ARTIFACTS"),
		manifests: required("MANIFESTS"), stage: "preflight", timeout: 3 * time.Minute,
	}
	require.NoError(t, os.MkdirAll(b.artifacts, 0755))

	// Set up clients to apply manifests and check cluster state
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{},
	).ClientConfig()
	require.NoError(t, err)
	config.Timeout = 20 * time.Second
	b.client, err = kubernetes.NewForConfig(config)
	require.NoError(b.t, err)
	b.dynamic, err = dynamic.NewForConfig(config)
	require.NoError(t, err)
	b.mapper = restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(b.client.Discovery()))

	// Read container logs locally before kubelet log access is available.
	connection, err := grpc.NewClient("unix:///run/k3s/containerd/containerd.sock",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	b.runtime = runtimeapi.NewRuntimeServiceClient(connection)
	b.ctx = t.Context()
	t.Cleanup(b.dumpDiagnostics)

	b.preflight()
	b.negative()
	b.positive()
	t.Log("OCCM initialized the node and CoreDNS became Ready")
}

func (b *bootstrapTest) preflight() {
	// Check the starting state: no OCCM Pods and CoreDNS blocked by the node taint.
	pods, err := b.pods(b.ctx, occmSelector)
	require.NoError(b.t, err)
	require.Empty(b.t, pods.Items, "OCCM must not have run on this cluster")
	b.nodeUID = b.node().UID
	dns, err := b.client.CoreV1().Services(metav1.NamespaceSystem).Get(b.ctx, "kube-dns", metav1.GetOptions{})
	require.NoError(b.t, err)
	b.clusterDNS = dns.Spec.ClusterIP
	b.waitFor("CoreDNS is blocked by the uninitialized taint", b.dnsBlocked)

	// Verify node DNS resolves the endpoint without a hosts-file shortcut.
	data, err := os.ReadFile("/etc/hosts")
	require.NoError(b.t, err)
	require.NotContains(b.t, string(data), b.hostname, "the node has a hosts-file shortcut")
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupHost(ctx, b.hostname)
	require.NoError(b.t, err)
	require.Contains(b.t, addresses, b.address, "node DNS did not resolve the Keystone address")

	b.dumpDiagnostics()
}

func (b *bootstrapTest) negative() {
	// Change only dnsPolicy to make occm depend on the unavailable cluster DNS
	candidate := b.readManifest(occmName + "-ds.yaml")
	found := false
	for _, item := range candidate {
		if item.GetKind() != "DaemonSet" {
			continue
		}
		hostNetwork, _, err := unstructured.NestedBool(item.Object, "spec", "template", "spec", "hostNetwork")
		require.NoError(b.t, err)
		require.True(b.t, hostNetwork, "this test requires host networking")
		policy, _, err := unstructured.NestedString(item.Object, "spec", "template", "spec", "dnsPolicy")
		require.NoError(b.t, err)
		require.Equal(b.t, string(corev1.DNSClusterFirst), policy, "unexpected default DNS policy")
		if err := unstructured.SetNestedField(item.Object, string(corev1.DNSClusterFirstWithHostNet),
			"spec", "template", "spec", "dnsPolicy"); err != nil {
			b.t.Fatal(err)
		}
		found = true
	}
	require.True(b.t, found, "the manifest has no DaemonSet")

	// Install OCCM with the same RBAC used by the positive case.
	for _, name := range []string{"cloud-controller-manager-roles.yaml", "cloud-controller-manager-role-bindings.yaml"} {
		b.apply(b.readManifest(name))
	}
	b.stage = "negative"
	b.apply(candidate)

	// Keep CoreDNS blocked while checking OCCM's resolver and initialization logs.
	b.waitFor("OCCM authentication fails because cluster DNS is unavailable", func() bool {
		require.True(b.t, b.dnsBlocked(), "CoreDNS stopped being blocked during the negative test")
		logs, status, err := b.collectOCCMLogs(b.ctx)
		require.NoError(b.t, err)
		nameservers := b.resolver(status)
		if len(nameservers) == 0 {
			return false
		}
		require.Equal(b.t, []string{b.clusterDNS}, nameservers, "the negative case is not using cluster DNS")

		// Look for a DNS lookup failure during cloud provider initialization.
		for line := range strings.SplitSeq(logs, "\n") {
			if strings.Contains(line, "lookup "+b.hostname) && strings.Contains(line, b.clusterDNS) &&
				(strings.Contains(line, "could not init cloud provider") ||
					strings.Contains(line, "Cloud provider could not be initialized")) {
				return true
			}
		}
		return false
	})

	// Save failure evidence before removing the failed deployment and its pods
	b.dumpDiagnostics()
	propagation := metav1.DeletePropagationForeground
	err := b.client.AppsV1().DaemonSets(metav1.NamespaceSystem).Delete(b.ctx, occmName, metav1.DeleteOptions{PropagationPolicy: &propagation})
	require.NoError(b.t, err)
	b.waitFor("negative-case Pods are deleted", func() bool {
		pods, err := b.pods(b.ctx, occmSelector)
		require.NoError(b.t, err)
		_, err = b.client.AppsV1().DaemonSets(metav1.NamespaceSystem).Get(b.ctx, occmName, metav1.GetOptions{})
		require.True(b.t, err == nil || apierrors.IsNotFound(err), "could not check DaemonSet deletion: %v", err)
		return apierrors.IsNotFound(err) && len(pods.Items) == 0
	})

	// Leave the node uninitialized and CoreDNS blocked for the positive case.
	require.True(b.t, b.dnsBlocked(), "the negative case changed the initial state")
}

func (b *bootstrapTest) positive() {
	b.stage = "positive"
	// Apply the checked-in policy on the same node, without DNS overriding
	b.apply(b.readManifest(occmName + "-ds.yaml"))

	// Check that cloud initialization sets providerID and removes the taint.
	b.waitFor("OCCM initializes the node and removes its taint", func() bool {
		node := b.node()
		return !uninitialized(node) && strings.HasPrefix(node.Spec.ProviderID, "openstack://")
	})

	// Verify OCCM bootstrapped without using the cluster DNS address.
	_, status, err := b.collectOCCMLogs(b.ctx)
	require.NoError(b.t, err)
	nameservers := b.resolver(status)
	require.True(b.t, len(nameservers) > 0 && !slices.Contains(nameservers, b.clusterDNS), "OCCM still uses cluster DNS")

	// Wait for the applied DaemonSet generation to become Ready.
	b.waitFor("the candidate OCCM DaemonSet is Ready", func() bool {
		ds, err := b.client.AppsV1().DaemonSets(metav1.NamespaceSystem).Get(b.ctx, occmName, metav1.GetOptions{})
		require.NoError(b.t, err)
		status := ds.Status
		return status.DesiredNumberScheduled > 0 && status.ObservedGeneration >= ds.Generation &&
			status.UpdatedNumberScheduled == status.DesiredNumberScheduled && status.NumberReady == status.DesiredNumberScheduled
	})

	// Confirm CoreDNS can start after OCCM has initialized the node.
	b.waitFor("CoreDNS becomes Ready after node initialization", func() bool {
		pods, err := b.pods(b.ctx, dnsSelector)
		require.NoError(b.t, err)
		for _, pod := range pods.Items {
			if !slices.ContainsFunc(pod.Status.Conditions, func(c corev1.PodCondition) bool {
				return c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue
			}) {
				return false
			}
		}
		return len(pods.Items) > 0
	})
}

func (b *bootstrapTest) readManifest(name string) []*unstructured.Unstructured {
	file, err := os.Open(filepath.Join(b.manifests, name))
	require.NoError(b.t, err)
	defer file.Close()
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	var objects []*unstructured.Unstructured
	for {
		object := &unstructured.Unstructured{}
		err := decoder.Decode(object)
		if err == io.EOF {
			break
		}
		require.NoError(b.t, err)
		if len(object.Object) == 0 {
			continue
		}
		// Expand List objects so each resource can be applied separately
		if object.IsList() {
			list, err := object.ToList()
			require.NoError(b.t, err)
			for i := range list.Items {
				objects = append(objects, &list.Items[i])
			}
		} else {
			objects = append(objects, object)
		}
	}
	return objects
}

func (b *bootstrapTest) apply(objects []*unstructured.Unstructured) {
	for _, object := range objects {
		kind := object.GroupVersionKind()
		mapping, err := b.mapper.RESTMapping(kind.GroupKind(), kind.Version)
		require.NoError(b.t, err)
		var resource dynamic.ResourceInterface = b.dynamic.Resource(mapping.Resource)
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			resource = b.dynamic.Resource(mapping.Resource).Namespace(object.GetNamespace())
		}
		// These checked-in resources belong to the disposable test environment.
		// Take ownership of RBAC fields that k3s may have bootstrapped already.
		_, err = resource.Apply(b.ctx, object.GetName(), object, metav1.ApplyOptions{
			FieldManager: "occm-bootstrap-test", Force: true,
		})
		require.NoError(b.t, err)
	}
}

func (b *bootstrapTest) save(name string, data []byte) {
	b.t.Helper()
	if err := os.WriteFile(filepath.Join(b.artifacts, name), data, 0644); err != nil {
		b.t.Error(err)
	}
}

func (b *bootstrapTest) waitFor(description string, check func() bool) {
	b.t.Helper()
	b.t.Log(description)
	err := wait.PollUntilContextTimeout(b.ctx, 3*time.Second, b.timeout, true,
		func(context.Context) (bool, error) { return check(), nil })
	require.NoError(b.t, err, "waiting for %s", description)
}

func uninitialized(node corev1.Node) bool {
	return slices.ContainsFunc(node.Spec.Taints, func(taint corev1.Taint) bool {
		return taint.Key == uninitializedTaint && taint.Effect == corev1.TaintEffectNoSchedule
	})
}

func (b *bootstrapTest) node() corev1.Node {
	nodes, err := b.client.CoreV1().Nodes().List(b.ctx, metav1.ListOptions{})
	require.NoError(b.t, err)
	require.Len(b.t, nodes.Items, 1, "use a fresh, single-node CI cluster")
	node := nodes.Items[0]
	require.True(b.t, b.nodeUID == "" || node.UID == b.nodeUID, "the test node was replaced")
	return node
}

func (b *bootstrapTest) pods(ctx context.Context, selector string) (*corev1.PodList, error) {
	return b.client.CoreV1().Pods(metav1.NamespaceSystem).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
}

func (b *bootstrapTest) dnsBlocked() bool {
	node := b.node()
	require.True(b.t, uninitialized(node), "the node was initialized before the positive test")
	pods, err := b.pods(b.ctx, dnsSelector)
	require.NoError(b.t, err)
	for _, pod := range pods.Items {
		// Match the taint directly because scheduler messages may omit its name.
		_, untolerated := corev1helpers.FindMatchingUntoleratedTaint(klog.FromContext(b.ctx),
			node.Spec.Taints, pod.Spec.Tolerations, func(taint *corev1.Taint) bool {
				return taint.Key == uninitializedTaint && taint.Effect == corev1.TaintEffectNoSchedule
			}, false)
		blocked := slices.ContainsFunc(pod.Status.Conditions, func(c corev1.PodCondition) bool {
			return c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse &&
				c.Reason == "Unschedulable" && strings.Contains(c.Message, "untolerated taint")
		})
		if pod.Status.Phase != corev1.PodPending || !blocked || !untolerated {
			return false
		}
	}
	return len(pods.Items) > 0
}

// Read the runtime directly while the node may lack a kubelet address.
// Filter by current Pod UID so old negative-case logs cannot count as recovery.
func (b *bootstrapTest) collectOCCMLogs(ctx context.Context) (string, *runtimeapi.ContainerStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pods, err := b.pods(ctx, occmSelector)
	if err != nil {
		return "", nil, err
	}
	containers, err := b.runtime.ListContainers(ctx, &runtimeapi.ListContainersRequest{})
	if err != nil {
		return "", nil, err
	}
	var output strings.Builder
	var current *runtimeapi.ContainerStatusResponse
	for _, container := range containers.Containers {
		if container.GetMetadata().GetName() != occmName || !slices.ContainsFunc(pods.Items, func(pod corev1.Pod) bool {
			return string(pod.UID) == container.Labels["io.kubernetes.pod.uid"]
		}) {
			continue
		}
		status, err := b.runtime.ContainerStatus(ctx, &runtimeapi.ContainerStatusRequest{ContainerId: container.Id, Verbose: true})
		if err != nil {
			return output.String(), current, err
		}
		// Containers that have not started have no logs to collect yet.
		if status.GetStatus().GetState() == runtimeapi.ContainerState_CONTAINER_CREATED {
			continue
		}
		data, err := os.ReadFile(status.GetStatus().GetLogPath())
		if err != nil {
			return output.String(), current, err
		}
		b.save(b.stage+"-"+container.Id+".log", data)
		output.Write(data)
		output.WriteByte('\n')
		if current == nil {
			current = status
		}
	}
	return output.String(), current, nil
}

func (b *bootstrapTest) resolver(status *runtimeapi.ContainerStatusResponse) []string {
	if status == nil {
		return nil
	}

	// Inspect the files mounted into OCCM to check its actual resolver settings.
	var inspection struct {
		RuntimeSpec struct {
			Mounts []struct{ Destination, Source string }
		}
	}
	require.NoError(b.t, json.Unmarshal([]byte(status.Info["info"]), &inspection))
	mounts := map[string]string{}
	for _, mount := range inspection.RuntimeSpec.Mounts {
		mounts[mount.Destination] = mount.Source
	}
	require.True(b.t, mounts["/etc/hosts"] != "" && mounts["/etc/resolv.conf"] != "", "OCCM resolver mounts are missing")

	// Reject hosts file shortcuts, so that would bypass the DNS lookup
	data, err := os.ReadFile(mounts["/etc/hosts"])
	require.NoError(b.t, err)
	require.NotContains(b.t, string(data), b.hostname, "OCCM has a hosts-file shortcut")
	b.save(b.stage+"-hosts", data)

	// Save the resolver configuration and extract the DNS server addresses.
	data, err = os.ReadFile(mounts["/etc/resolv.conf"])
	require.NoError(b.t, err)
	b.save(b.stage+"-resolv.conf", data)
	var nameservers []string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			nameservers = append(nameservers, fields[1])
		}
	}
	return nameservers
}

func dumpResource[T runtime.Object](b *bootstrapTest, ctx context.Context, name string,
	list func(context.Context, metav1.ListOptions) (T, error),
) {
	resource, err := list(ctx, metav1.ListOptions{})
	if err != nil {
		b.t.Logf("collect %s: %v", name, err)
		return
	}
	data, err := json.MarshalIndent(resource, "", "  ")
	if err != nil {
		b.t.Logf("encode %s: %v", name, err)
		return
	}
	b.save(b.stage+"-"+name+".json", data)
}

func (b *bootstrapTest) dumpDiagnostics() {
	// Cleanup runs after the test context is cancelled. Give collection its own
	// deadline and keep collecting if an individual resource is unavailable.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b.t.Logf("Collecting %s diagnostics", b.stage)
	// Keep an explicit resource list so artifacts never include cloud Secrets.
	dumpResource(b, ctx, "nodes", b.client.CoreV1().Nodes().List)
	dumpResource(b, ctx, "pods", b.client.CoreV1().Pods(metav1.NamespaceAll).List)
	dumpResource(b, ctx, "events", b.client.CoreV1().Events(metav1.NamespaceAll).List)
	dumpResource(b, ctx, "daemonsets", b.client.AppsV1().DaemonSets(metav1.NamespaceAll).List)
	if _, _, err := b.collectOCCMLogs(ctx); err != nil {
		b.t.Logf("collect OCCM logs: %v", err)
	}
}
