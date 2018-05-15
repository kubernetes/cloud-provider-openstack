/*
Copyright 2018 The Kubernetes Authors.

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
	"fmt"
	"reflect"
	"time"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	ext_v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	ext_listers "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"

	"k8s.io/cloud-provider-openstack/pkg/ingress/config"
	"k8s.io/cloud-provider-openstack/pkg/ingress/controller/openstack"
)

const (
	// High enough QPS to fit all expected use cases. QPS=0 is not set here, because
	// client code is overriding it.
	defaultQPS = 1e6
	// High enough Burst to fit all expected use cases. Burst=0 is not set here, because
	// client code is overriding it.
	defaultBurst = 1e6

	maxRetries = 5

	// IngressKey picks a specific "class" for the Ingress.
	// The controller only processes Ingresses with this annotation either
	// unset, or set to either the configured value or the empty string.
	IngressKey = "kubernetes.io/ingress.class"

	// IngressClass menas accept ingresses with the annotation
	IngressClass = "openstack"

	// LabelNodeRoleMaster specifies that a node is a master
	// It's copied over to kubeadm until it's merged in core: https://github.com/kubernetes/kubernetes/pull/39112
	LabelNodeRoleMaster = "node-role.kubernetes.io/master"
)

var (
	nodePort int
)

// Controller ...
type Controller struct {
	stopCh              chan struct{}
	knownNodes          []*apiv1.Node
	queue               workqueue.RateLimitingInterface
	informer            informers.SharedInformerFactory
	ingressLister       ext_listers.IngressLister
	ingressListerSynced cache.InformerSynced
	serviceLister       corelisters.ServiceLister
	serviceListerSynced cache.InformerSynced
	nodeLister          corelisters.NodeLister
	nodeListerSynced    cache.InformerSynced
	osClient            *openstack.OpenStack
	kubeClient          kubernetes.Interface
	config              config.Config
}

// IsValid returns true if the given Ingress either doesn't specify
// the ingress.class annotation, or it's set to the configured in the
// ingress controller.
func IsValid(ing *ext_v1beta1.Ingress) bool {
	ingress, ok := ing.GetAnnotations()[IngressKey]
	if !ok {
		log.WithFields(log.Fields{
			"ingress_name": ing.Name, "ingress_ns": ing.Namespace,
		}).Info("annotation not present in ingress")
		return false
	}

	return ingress == IngressClass
}

func createApiserverClient(apiserverHost string, kubeConfig string) (*kubernetes.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(apiserverHost, kubeConfig)
	if err != nil {
		return nil, err
	}

	cfg.QPS = defaultQPS
	cfg.Burst = defaultBurst
	cfg.ContentType = "application/vnd.kubernetes.protobuf"

	log.Info("creating kubernetes API client")

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	v, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}
	log.WithFields(log.Fields{
		"version": fmt.Sprintf("v%v.%v", v.Major, v.Minor),
	}).Info("kubernetes API client created")

	return client, nil
}

func getNodeConditionPredicate() corelisters.NodeConditionPredicate {
	return func(node *apiv1.Node) bool {
		// We add the master to the node list, but its unschedulable.  So we use this to filter
		// the master.
		if node.Spec.Unschedulable {
			return false
		}

		// As of 1.6, we will taint the master, but not necessarily mark it unschedulable.
		// Recognize nodes labeled as master, and filter them also, as we were doing previously.
		if _, hasMasterRoleLabel := node.Labels[LabelNodeRoleMaster]; hasMasterRoleLabel {
			return false
		}

		// If we have no info, don't accept
		if len(node.Status.Conditions) == 0 {
			return false
		}
		for _, cond := range node.Status.Conditions {
			// We consider the node for load balancing only when its NodeReady condition status
			// is ConditionTrue
			if cond.Type == apiv1.NodeReady && cond.Status != apiv1.ConditionTrue {
				log.WithFields(log.Fields{"name": node.Name, "status": cond.Status}).Info("ignoring node")
				return false
			}
		}
		return true
	}
}

// NewController creates a new OpenStack Ingress controller.
func NewController(conf config.Config) *Controller {
	// initialize k8s client
	kubeClient, err := createApiserverClient(conf.Kubernetes.ApiserverHost, conf.Kubernetes.KubeConfig)
	if err != nil {
		log.WithFields(log.Fields{
			"api_server":  conf.Kubernetes.ApiserverHost,
			"kuberconfig": conf.Kubernetes.KubeConfig,
			"error":       err,
		}).Fatal("failed to initialize kubernetes client")
	}

	// initialize openstack client
	var osClient *openstack.OpenStack
	osClient, err = openstack.NewOpenStack(conf)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("failed to initialize openstack client")
	}

	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, time.Second*30)
	serviceInformer := kubeInformerFactory.Core().V1().Services()
	nodeInformer := kubeInformerFactory.Core().V1().Nodes()
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		config:              conf,
		queue:               queue,
		stopCh:              make(chan struct{}),
		informer:            kubeInformerFactory,
		serviceLister:       serviceInformer.Lister(),
		serviceListerSynced: serviceInformer.Informer().HasSynced,
		nodeLister:          nodeInformer.Lister(),
		nodeListerSynced:    nodeInformer.Informer().HasSynced,
		knownNodes:          []*apiv1.Node{},
		osClient:            osClient,
		kubeClient:          kubeClient,
	}

	ingInformer := kubeInformerFactory.Extensions().V1beta1().Ingresses()
	ingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueIng,
		UpdateFunc: func(old, new interface{}) {
			newIng := new.(*ext_v1beta1.Ingress)
			oldIng := old.(*ext_v1beta1.Ingress)
			if newIng.ResourceVersion == oldIng.ResourceVersion {
				// Periodic resync will send update events for all known Ingresses.
				// Two different versions of the same Ingress will always have different RVs.
				return
			}
			if reflect.DeepEqual(newIng.Spec, oldIng.Spec) {
				return
			}
			controller.enqueueIng(new)
		},
		DeleteFunc: controller.enqueueIng,
	})

	controller.ingressLister = ingInformer.Lister()
	controller.ingressListerSynced = ingInformer.Informer().HasSynced

	return controller
}

// Start starts the openstack ingress controller.
func (c *Controller) Start() {
	defer close(c.stopCh)
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("starting Ingress controller")
	go c.informer.Start(c.stopCh)

	// wait for the caches to synchronize before starting the worker
	if !cache.WaitForCacheSync(c.stopCh, c.ingressListerSynced, c.serviceListerSynced, c.nodeListerSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	log.Info("ingress controller synced and ready")

	newNodes, err := c.nodeLister.ListWithPredicate(getNodeConditionPredicate())
	if err != nil {
		log.Errorf("Failed to retrieve current set of nodes from node lister: %v", err)
		return
	}
	c.knownNodes = newNodes

	go wait.Until(c.runWorker, time.Second, c.stopCh)
	go wait.Until(c.nodeSyncLoop, 60*time.Second, c.stopCh)

	<-c.stopCh
}

// obj could be an *v1.Ingress, or a DeletionFinalStateUnknown marker item.
func (c *Controller) enqueueIng(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		log.WithFields(log.Fields{"error": err, "object": obj}).Error("Couldn't get key for object")
		return
	}

	c.queue.Add(key)
}

// nodeSyncLoop handles updating the hosts pointed to by all load
// balancers whenever the set of nodes in the cluster changes.
func (c *Controller) nodeSyncLoop() {
	newNodes, err := c.nodeLister.ListWithPredicate(getNodeConditionPredicate())
	if err != nil {
		log.Errorf("Failed to retrieve current set of nodes from node lister: %v", err)
		return
	}
	if nodeSlicesEqualForLB(newNodes, c.knownNodes) {
		return
	}

	log.Infof("Detected change in list of current cluster nodes. New node set: %v", nodeNames(newNodes))

	ings := new(ext_v1beta1.IngressList)
	// TODO: only take ingresses without ip address into consideration
	opts := apimeta_v1.ListOptions{}
	if ings, err = c.kubeClient.ExtensionsV1beta1().Ingresses("").List(opts); err != nil {
		log.Errorf("Failed to retrieve current set of ingresses: %v", err)
		return
	}

	// Update each ingress
	for _, ing := range ings.Items {
		log.WithFields(log.Fields{"ingress": ing.ObjectMeta.Name}).Info("Starting to handle ingress")

		lbName := getResourceName(&ing, "lb")
		loadbalancer, err := c.osClient.GetLoadbalancerByName(lbName)
		if err != nil {
			if err != openstack.ErrNotFound {
				log.WithFields(log.Fields{"name": lbName}).Errorf("Failed to retrieve loadbalancer from OpenStack: %v", err)
			}

			// If lb doesn't exist or error occurred, continue
			continue
		}

		if err = c.osClient.UpdateLoadbalancerMembers(loadbalancer.ID, newNodes); err != nil {
			log.WithFields(log.Fields{"ingress": ing.ObjectMeta.Name}).Errorf("Failed to handle ingress")
			continue
		}

		log.WithFields(log.Fields{"ingress": ing.ObjectMeta.Name}).Info("Finished to handle ingress")
	}

	c.knownNodes = newNodes
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *Controller) processNextItem() bool {
	key, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.processItem(key.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		log.WithFields(log.Fields{"key": key, "error": err}).Error("Failed to process key (will retry)")
		c.queue.AddRateLimited(key)
	} else {
		// err != nil and too many retries
		log.WithFields(log.Fields{"key": key, "error": err}).Error("Failed to process key (giving up)")
		c.queue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) processItem(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	ing, err := c.ingressLister.Ingresses(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		log.WithFields(log.Fields{"key": key}).Info("ingress has been deleted, will delete octavia resources")
		err = c.deleteIngress(namespace, name)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to delete openstack resources for ingress %s: %v", key, err))
		}
	case err != nil:
		return fmt.Errorf("error fetching object with key %s from store: %v", key, err)
	default:
		if !IsValid(ing) {
			log.WithFields(log.Fields{"key": key}).Info("ignore ingress")
			return nil
		}

		log.WithFields(log.Fields{"key": key, "ingress": ing}).Info("ingress created or updated, will create or update octavia resources")

		err = c.ensureIngress(ing)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to create openstack resources for ingress %s: %v", key, err))
		}
	}

	return nil
}

func (c *Controller) deleteIngress(namespace, name string) error {
	lbName := fmt.Sprintf("k8s-%s-%s-lb", namespace, name)
	loadbalancer, err := c.osClient.GetLoadbalancerByName(lbName)
	if err != nil {
		if err != openstack.ErrNotFound {
			return fmt.Errorf("error getting loadbalancer %s: %v", name, err)
		}
		log.WithFields(log.Fields{"lbName": lbName, "ingressName": name, "ingressNamespace": namespace}).Info("loadbalancer for ingress deleted")
		return nil
	}

	if err = c.osClient.DeleteFloatingIP(loadbalancer.VipPortID); err != nil {
		return err
	}

	return c.osClient.DeleteLoadbalancer(loadbalancer.ID)
}

func (c *Controller) ensureIngress(ing *ext_v1beta1.Ingress) error {
	var lbName = getResourceName(ing, "lb")
	lb, err := c.osClient.EnsureLoadBalancer(lbName, c.config.Octavia.SubnetID)
	if err != nil {
		return err
	}

	var listenerName = getResourceName(ing, "listener")
	listener, err := c.osClient.EnsureListener(listenerName, lb.ID)
	if err != nil {
		return err
	}

	// get nodes information
	nodeObjs, err := c.nodeLister.ListWithPredicate(getNodeConditionPredicate())
	if err != nil {
		return err
	}

	// Add default pool for the listener if 'backend' is defined
	defaultPoolName := listenerName + "-pool"
	if ing.Spec.Backend != nil {
		serviceName := fmt.Sprintf("%s/%s", ing.ObjectMeta.Namespace, ing.Spec.Backend.ServiceName)
		nodePort, err := c.getServiceNodePort(serviceName, ing.Spec.Backend.ServicePort.IntValue())
		if err != nil {
			return err
		}

		if _, err = c.osClient.EnsurePoolMembers(false, defaultPoolName, "", listener.ID, &nodePort, nodeObjs); err != nil {
			return err
		}
	} else {
		// Delete default pool and its members
		if _, err = c.osClient.EnsurePoolMembers(true, defaultPoolName, lb.ID, listener.ID, nil, nil); err != nil {
			return err
		}
	}

	// Delete all existing policies
	existingPolicies, err := c.osClient.GetL7policies(listener.ID)
	if err != nil {
		return err
	}
	for _, p := range existingPolicies {
		c.osClient.DeleteL7policy(p.ID, lb.ID)
	}

	// Delete all existing shared pools
	existingSharedPools, err := c.osClient.GetPools(lb.ID, true)
	if err != nil {
		return err
	}
	for _, sp := range existingSharedPools {
		c.osClient.DeletePool(sp.ID, lb.ID)
	}

	// Add l7 load balancing rules. Each host and path combination is mapped to a l7 policy in octavia,
	// which contains two rules(with type 'HOST_NAME' and 'PATH' respectively)
	for _, rule := range ing.Spec.Rules {
		host := rule.Host

		for _, path := range rule.HTTP.Paths {
			// make the pool name unique
			poolName := hash(fmt.Sprintf("%s+%s", path.Backend.ServiceName, path.Backend.ServicePort.String()))
			serviceName := fmt.Sprintf("%s/%s", ing.ObjectMeta.Namespace, path.Backend.ServiceName)
			nodePort, err := c.getServiceNodePort(serviceName, path.Backend.ServicePort.IntValue())
			if err != nil {
				return err
			}

			poolID, err := c.osClient.EnsurePoolMembers(false, poolName, lb.ID, "", &nodePort, nodeObjs)
			if err != nil {
				return err
			}

			// make the policy name unique
			policyName := hash(fmt.Sprintf("%s+%s", host, path.Path))
			if err = c.osClient.EnsurePolicyRules(false, policyName, lb.ID, listener.ID, *poolID, host, path.Path); err != nil {
				return err
			}
		}
	}

	var address string
	address = lb.VipAddress
	if c.config.Octavia.FloatingIPNetwork != "" {
		// Allocate floating ip for loadbalancer vip.
		if address, err = c.osClient.EnsureFloatingIP(lb.VipPortID, c.config.Octavia.FloatingIPNetwork); err != nil {
			return err
		}
	}

	// Update ingress status
	if err = c.updateIngressStatus(ing, address); err != nil {
		return err
	}

	log.Info("openstack resources for ingress created")
	return nil
}

func (c *Controller) updateIngressStatus(ing *ext_v1beta1.Ingress, vip string) error {
	previousState := loadBalancerStatusDeepCopy(&ing.Status.LoadBalancer)

	newState := new(apiv1.LoadBalancerStatus)
	newState.Ingress = []apiv1.LoadBalancerIngress{{IP: vip}}
	newIng := ing.DeepCopy()
	newIng.Status.LoadBalancer = *newState

	// This is to make sure we don't send duplicate update event.
	if !loadBalancerStatusEqual(previousState, newState) {
		if _, err := c.kubeClient.ExtensionsV1beta1().Ingresses(newIng.Namespace).UpdateStatus(newIng); err != nil {
			return err
		}
		log.Info("ingress status updated")
	}

	return nil
}

func (c *Controller) getService(key string) (*apiv1.Service, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}

	service, err := c.serviceLister.Services(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	return service, nil
}

func (c *Controller) getServiceNodePort(name string, port int) (int, error) {
	svc, err := c.getService(name)
	if err != nil {
		return 0, err
	}

	var nodePort int
	ports := svc.Spec.Ports
	for _, p := range ports {
		if int(p.Port) == port {
			nodePort = int(p.NodePort)
		}
	}

	if nodePort == 0 {
		return 0, fmt.Errorf("failed to find service node port")
	}

	return nodePort, nil
}
