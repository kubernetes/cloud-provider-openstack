/*
Copyright 2019 The Kubernetes Authors.

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

package controller

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	log "k8s.io/klog/v2"

	"k8s.io/cloud-provider-openstack/pkg/autohealing/cloudprovider"
	_ "k8s.io/cloud-provider-openstack/pkg/autohealing/cloudprovider/register"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/config"
	"k8s.io/cloud-provider-openstack/pkg/autohealing/healthcheck"
)

// EventType type of event associated with an informer
type EventType string

const (
	// High enough QPS to fit all expected use cases. QPS=0 is not set here, because
	// client code is overriding it.
	defaultQPS = 1e6
	// High enough Burst to fit all expected use cases. Burst=0 is not set here, because
	// client code is overriding it.
	defaultBurst = 1e6

	// CreateEvent event associated with new objects in an informer
	CreateEvent EventType = "CREATE"
	// UpdateEvent event associated with an object update in an informer
	UpdateEvent EventType = "UPDATE"
	// DeleteEvent event associated when an object is removed from an informer
	DeleteEvent EventType = "DELETE"

	// LabelNodeRoleMaster specifies that a node is a master
	// Related discussion: https://github.com/kubernetes/kubernetes/pull/39112
	LabelNodeRoleMaster = "node-role.kubernetes.io/master"
)

var (
	masterUnhealthyNodes []healthcheck.NodeInfo
	workerUnhealthyNodes []healthcheck.NodeInfo
)

// Event holds the context of an event
type Event struct {
	Type EventType
	Obj  interface{}
}

func createKubeClients(apiserverHost string, kubeConfig string) (*kubernetes.Clientset, *kubernetes.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(apiserverHost, kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	cfg.QPS = defaultQPS
	cfg.Burst = defaultBurst
	cfg.ContentType = "application/vnd.kubernetes.protobuf"
	cfg.Timeout = 5 * time.Second

	log.V(4).Info("Creating kubernetes API clients")

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	leaderElectionClient, err := kubernetes.NewForConfig(restclient.AddUserAgent(cfg, "leader-election"))
	if err != nil {
		return nil, nil, err
	}

	v, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, nil, err
	}
	log.V(4).Infof("Kubernetes API client created, server version: %s", fmt.Sprintf("v%v.%v", v.Major, v.Minor))

	return client, leaderElectionClient, nil
}

// NewController creates a new autohealer controller.
func NewController(conf config.Config) *Controller {
	// initialize k8s clients
	kubeClient, leaderElectionClient, err := createKubeClients(conf.Kubernetes.ApiserverHost, conf.Kubernetes.KubeConfig)
	if err != nil {
		log.Fatalf("failed to initialize kubernetes client, error: %v", err)
	}

	// initialize cloud provider
	provider, err := cloudprovider.GetCloudProvider(conf.CloudProvider, conf, kubeClient)
	if err != nil {
		log.Fatalf("Failed to get the cloud provider %s: %v", conf.CloudProvider, err)
	}

	log.Infof("Using cloud provider: %s", provider.GetName())

	// event
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.V(4).Infof)
	eventBroadcaster.StartRecordingToSink(&typev1.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "openstack-ingress-controller"})

	// Initialize the configured health checkers
	var workerCheckers []healthcheck.HealthCheck
	var masterCheckers []healthcheck.HealthCheck
	for _, item := range conf.HealthCheck.Worker {
		checker, err := healthcheck.GetHealthChecker(item.Type, item.Params)
		if err != nil {
			log.Fatalf("failed to get %s type health check for worker node, error: %v", item.Type, err)
		}
		if !checker.IsWorkerSupported() {
			log.Warningf("Plugin type %s does not support worker node health check, will skip", item.Type)
			continue
		}
		workerCheckers = append(workerCheckers, checker)
	}
	for _, item := range conf.HealthCheck.Master {
		checker, err := healthcheck.GetHealthChecker(item.Type, item.Params)
		if err != nil {
			log.Fatalf("failed to get %s type health check for master node, error: %v", item.Type, err)
		}
		if !checker.IsMasterSupported() {
			log.Warningf("Plugin type %s does not support master node health check, will skip", item.Type)
			continue
		}
		masterCheckers = append(masterCheckers, checker)
	}

	controller := &Controller{
		config:               conf,
		recorder:             recorder,
		provider:             provider,
		kubeClient:           kubeClient,
		leaderElectionClient: leaderElectionClient,
		masterCheckers:       masterCheckers,
		workerCheckers:       workerCheckers,
	}

	return controller
}

// Controller ...
type Controller struct {
	provider             cloudprovider.CloudProvider
	recorder             record.EventRecorder
	kubeClient           kubernetes.Interface
	leaderElectionClient kubernetes.Interface
	config               config.Config
	workerCheckers       []healthcheck.HealthCheck
	masterCheckers       []healthcheck.HealthCheck
}

// UpdateNodeAnnotation updates the specified node annotation, if value equals empty string, the annotation will be
// removed. This implements the interface healthcheck.NodeController
func (c *Controller) UpdateNodeAnnotation(node healthcheck.NodeInfo, annotation string, value string) error {
	n, err := c.kubeClient.CoreV1().Nodes().Get(context.TODO(), node.KubeNode.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if value == "" {
		delete(n.Annotations, annotation)
	} else {
		n.Annotations[annotation] = value
	}

	if _, err := c.kubeClient.CoreV1().Nodes().Update(context.TODO(), n, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return nil
}

func (c *Controller) GetLeaderElectionLock() (resourcelock.Interface, error) {
	// Identity used to distinguish between multiple cloud controller manager instances
	id, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id = id + "_" + string(uuid.NewUUID())

	rl, err := resourcelock.New(
		"configmaps",
		"kube-system",
		"magnum-auto-healer",
		c.leaderElectionClient.CoreV1(),
		c.leaderElectionClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: c.recorder,
		})
	if err != nil {
		return nil, err
	}

	return rl, nil
}

// getUnhealthyMasterNodes returns the master nodes that need to be repaired.
func (c *Controller) getUnhealthyMasterNodes() ([]healthcheck.NodeInfo, error) {
	var nodes []healthcheck.NodeInfo

	// If no checkers defined, skip
	if len(c.masterCheckers) == 0 {
		log.V(3).Info("No health check defined for master node, skip.")
		return nodes, nil
	}

	// Get all the master nodes need to check
	nodeList, err := c.kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, node := range nodeList.Items {
		if _, hasMasterRoleLabel := node.Labels[LabelNodeRoleMaster]; hasMasterRoleLabel {
			if time.Now().Before(node.ObjectMeta.GetCreationTimestamp().Add(c.config.CheckDelayAfterAdd)) {
				log.V(4).Infof("The node %s is created less than the configured check delay, skip", node.Name)
				continue
			}

			nodes = append(nodes, healthcheck.NodeInfo{KubeNode: node, IsWorker: false})
		}
	}

	// Do health check
	unhealthyNodes := healthcheck.CheckNodes(c.masterCheckers, nodes, c)

	return unhealthyNodes, nil
}

// getUnhealthyWorkerNodes returns the nodes that need to be repaired.
func (c *Controller) getUnhealthyWorkerNodes() ([]healthcheck.NodeInfo, error) {
	var nodes []healthcheck.NodeInfo

	// If no checkers defined, skip.
	if len(c.workerCheckers) == 0 {
		log.V(3).Info("No health check defined for worker node, skip.")
		return nodes, nil
	}

	// Get all the worker nodes.
	nodeList, err := c.kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, node := range nodeList.Items {
		if _, hasMasterRoleLabel := node.Labels[LabelNodeRoleMaster]; hasMasterRoleLabel {
			continue
		}
		if len(node.Status.Conditions) == 0 {
			continue
		}
		if time.Now().Before(node.ObjectMeta.GetCreationTimestamp().Add(c.config.CheckDelayAfterAdd)) {
			log.V(4).Infof("The node %s is created less than the configured check delay, skip", node.Name)
			continue
		}
		nodes = append(nodes, healthcheck.NodeInfo{KubeNode: node, IsWorker: true})
	}

	// Do health check
	unhealthyNodes := healthcheck.CheckNodes(c.workerCheckers, nodes, c)

	return unhealthyNodes, nil
}

func (c *Controller) repairNodes(unhealthyNodes []healthcheck.NodeInfo) {
	unhealthyNodeNames := sets.NewString()
	for _, n := range unhealthyNodes {
		unhealthyNodeNames.Insert(n.KubeNode.Name)
	}

	// Trigger unhealthy nodes repair.
	if len(unhealthyNodes) > 0 {
		if !c.provider.Enabled() {
			// The cloud provider doesn't allow to trigger node repair.
			log.Infof("Auto healing is ignored for nodes %s", unhealthyNodeNames.List())
		} else {
			log.Infof("Starting to repair nodes %s, dryrun: %t", unhealthyNodeNames.List(), c.config.DryRun)

			if !c.config.DryRun {
				// Cordon the nodes before repair.
				for _, node := range unhealthyNodes {
					nodeName := node.KubeNode.Name
					newNode := node.KubeNode.DeepCopy()
					newNode.Spec.Unschedulable = true

					if _, err := c.kubeClient.CoreV1().Nodes().Update(context.TODO(), newNode, metav1.UpdateOptions{}); err != nil {
						log.Errorf("Failed to cordon node %s, error: %v", nodeName, err)
					} else {
						log.Infof("Node %s is cordoned", nodeName)
					}
				}

				// Start to repair all the unhealthy nodes.
				if err := c.provider.Repair(unhealthyNodes); err != nil {
					log.Errorf("Failed to repair the nodes %s, error: %v", unhealthyNodeNames.List(), err)
				}
			}
		}
	}
}

// startMasterMonitor checks if there are failed master nodes and triggers the repair action. This function is supposed
// to be running in a goroutine.
func (c *Controller) startMasterMonitor(wg *sync.WaitGroup) {
	log.V(3).Info("Starting to check master nodes.")
	defer wg.Done()

	// Get all the unhealthy master nodes.
	unhealthyNodes, err := c.getUnhealthyMasterNodes()
	if err != nil {
		log.Errorf("Failed to get unhealthy master nodes, error: %v", err)
		return
	}

	masterUnhealthyNodes = append(masterUnhealthyNodes, unhealthyNodes...)

	c.repairNodes(unhealthyNodes)

	if len(unhealthyNodes) == 0 {
		log.V(3).Info("Master nodes are healthy")
	}

	log.V(3).Info("Finished checking master nodes.")
}

// startWorkerMonitor checks if there are failed worker nodes and triggers the repair action. This function is supposed
// to be running in a goroutine.
func (c *Controller) startWorkerMonitor(wg *sync.WaitGroup) {
	log.V(3).Info("Starting to check worker nodes.")
	defer wg.Done()

	// Get all the unhealthy worker nodes.
	unhealthyNodes, err := c.getUnhealthyWorkerNodes()
	if err != nil {
		log.Errorf("Failed to get unhealthy worker nodes, error: %v", err)
		return
	}

	workerUnhealthyNodes = append(workerUnhealthyNodes, unhealthyNodes...)

	c.repairNodes(unhealthyNodes)

	if len(unhealthyNodes) == 0 {
		log.V(3).Info("Worker nodes are healthy")
	}

	log.V(3).Info("Finished checking worker nodes.")
}

// Start starts the autohealing controller.
func (c *Controller) Start(ctx context.Context) {
	log.Info("Starting autohealing controller")

	ticker := time.NewTicker(c.config.MonitorInterval)
	defer ticker.Stop()

	var wg sync.WaitGroup
	for {
		masterUnhealthyNodes = []healthcheck.NodeInfo{}
		workerUnhealthyNodes = []healthcheck.NodeInfo{}
		select {
		case <-ticker.C:
			if c.config.MasterMonitorEnabled {
				wg.Add(1)
				go c.startMasterMonitor(&wg)
			}
			if c.config.WorkerMonitorEnabled {
				wg.Add(1)
				go c.startWorkerMonitor(&wg)
			}

			wg.Wait()
			c.provider.UpdateHealthStatus(masterUnhealthyNodes, workerUnhealthyNodes)
		}
	}
}
