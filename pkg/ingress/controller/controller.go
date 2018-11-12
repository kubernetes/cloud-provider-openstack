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
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	extlisters "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/cloud-provider-openstack/pkg/ingress/config"
	"k8s.io/cloud-provider-openstack/pkg/ingress/controller/openstack"
	"k8s.io/klog"
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

// EventType type of event associated with an informer
type EventType string

const (
	// CreateEvent event associated with new objects in an informer
	CreateEvent EventType = "CREATE"
	// UpdateEvent event associated with an object update in an informer
	UpdateEvent EventType = "UPDATE"
	// DeleteEvent event associated when an object is removed from an informer
	DeleteEvent EventType = "DELETE"
)

// Event holds the context of an event
type Event struct {
	Type EventType
	Obj  interface{}
}

// Controller ...
type Controller struct {
	stopCh              chan struct{}
	knownNodes          []*apiv1.Node
	queue               workqueue.RateLimitingInterface
	informer            informers.SharedInformerFactory
	recorder            record.EventRecorder
	ingressLister       extlisters.IngressLister
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
func IsValid(ing *extv1beta1.Ingress) bool {
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

	log.Debug("creating kubernetes API client")

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
	}).Debug("kubernetes API client created")

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

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "openstack-ingress-controller"})

	controller := &Controller{
		config:              conf,
		queue:               queue,
		stopCh:              make(chan struct{}),
		informer:            kubeInformerFactory,
		recorder:            recorder,
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
		AddFunc: func(obj interface{}) {
			addIng := obj.(*extv1beta1.Ingress)
			key := fmt.Sprintf("%s/%s", addIng.Namespace, addIng.Name)

			if !IsValid(addIng) {
				log.Infof("ignore ingress %s", key)
				return
			}

			recorder.Event(addIng, apiv1.EventTypeNormal, "Creating", fmt.Sprintf("Ingress %s", key))
			controller.queue.AddRateLimited(Event{Obj: addIng, Type: CreateEvent})
		},
		UpdateFunc: func(old, new interface{}) {
			newIng := new.(*extv1beta1.Ingress)
			oldIng := old.(*extv1beta1.Ingress)
			if newIng.ResourceVersion == oldIng.ResourceVersion {
				// Periodic resync will send update events for all known Ingresses.
				// Two different versions of the same Ingress will always have different RVs.
				return
			}

			key := fmt.Sprintf("%s/%s", newIng.Namespace, newIng.Name)
			validOld := IsValid(oldIng)
			validCur := IsValid(newIng)
			if !validOld && validCur {
				recorder.Event(newIng, apiv1.EventTypeNormal, "Creating", fmt.Sprintf("Ingress %s", key))
				controller.queue.AddRateLimited(Event{Obj: newIng, Type: CreateEvent})
			} else if validOld && !validCur {
				recorder.Event(newIng, apiv1.EventTypeNormal, "Deleting", fmt.Sprintf("Ingress %s", key))
				controller.queue.AddRateLimited(Event{Obj: newIng, Type: DeleteEvent})
			} else if validCur && !reflect.DeepEqual(newIng.Spec, oldIng.Spec) {
				recorder.Event(newIng, apiv1.EventTypeNormal, "Updating", fmt.Sprintf("Ingress %s", key))
				controller.queue.AddRateLimited(Event{Obj: newIng, Type: UpdateEvent})
			} else {
				return
			}
		},
		DeleteFunc: func(obj interface{}) {
			delIng, ok := obj.(*extv1beta1.Ingress)
			if !ok {
				// If we reached here it means the ingress was deleted but its final state is unrecorded.
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					log.Errorf("couldn't get object from tombstone %#v", obj)
					return
				}
				delIng, ok = tombstone.Obj.(*extv1beta1.Ingress)
				if !ok {
					log.Errorf("Tombstone contained object that is not an Ingress: %#v", obj)
					return
				}
			}

			key := fmt.Sprintf("%s/%s", delIng.Namespace, delIng.Name)
			if !IsValid(delIng) {
				log.Infof("ignore ingress %s", key)
				return
			}

			recorder.Event(delIng, apiv1.EventTypeNormal, "Deleting", fmt.Sprintf("Ingress %s", key))
			controller.queue.AddRateLimited(Event{Obj: delIng, Type: DeleteEvent})
		},
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

	log.Debug("starting Ingress controller")
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

	ings := new(extv1beta1.IngressList)
	// TODO: only take ingresses without ip address into consideration
	opts := apimetav1.ListOptions{}
	if ings, err = c.kubeClient.ExtensionsV1beta1().Ingresses("").List(opts); err != nil {
		log.Errorf("Failed to retrieve current set of ingresses: %v", err)
		return
	}

	// Update each valid ingress
	for _, ing := range ings.Items {
		if !IsValid(&ing) {
			continue
		}

		log.WithFields(log.Fields{"ingress": ing.Name, "namespace": ing.Namespace}).Debug("Starting to handle ingress")

		lbName := getResourceName(ing.Namespace, ing.Name, c.config.ClusterName)
		loadbalancer, err := c.osClient.GetLoadbalancerByName(lbName)
		if err != nil {
			if err != openstack.ErrNotFound {
				log.WithFields(log.Fields{"name": lbName}).Errorf("Failed to retrieve loadbalancer from OpenStack: %v", err)
			}

			// If lb doesn't exist or error occurred, continue
			continue
		}

		if err = c.osClient.UpdateLoadbalancerMembers(loadbalancer.ID, newNodes); err != nil {
			log.WithFields(log.Fields{"ingress": ing.Name}).Error("Failed to handle ingress")
			continue
		}

		log.WithFields(log.Fields{"ingress": ing.Name, "namespace": ing.Namespace}).Info("Finished to handle ingress")
	}

	c.knownNodes = newNodes

	log.Info("Finished to handle node change")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *Controller) processNextItem() bool {
	obj, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(obj)

	err := c.processItem(obj.(Event))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(obj)
	} else if c.queue.NumRequeues(obj) < maxRetries {
		log.WithFields(log.Fields{"obj": obj, "error": err}).Error("Failed to process obj (will retry)")
		c.queue.AddRateLimited(obj)
	} else {
		// err != nil and too many retries
		log.WithFields(log.Fields{"obj": obj, "error": err}).Error("Failed to process obj (giving up)")
		c.queue.Forget(obj)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) processItem(event Event) error {
	ing := event.Obj.(*extv1beta1.Ingress)
	key := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)

	switch event.Type {
	case CreateEvent:
		log.WithFields(log.Fields{"ingress": key}).Info("ingress created, will create openstack resources")

		if err := c.ensureIngress(ing); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to create openstack resources for ingress %s: %v", key, err))
			c.recorder.Event(ing, apiv1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to create openstack resources for ingress %s: %v", key, err))
		} else {
			c.recorder.Event(ing, apiv1.EventTypeNormal, "Created", fmt.Sprintf("Ingress %s", key))
		}
	case UpdateEvent:
		log.WithFields(log.Fields{"ingress": key}).Info("ingress updated, will update openstack resources")

		if err := c.ensureIngress(ing); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to update openstack resources for ingress %s: %v", key, err))
			c.recorder.Event(ing, apiv1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to update openstack resources for ingress %s: %v", key, err))
		} else {
			c.recorder.Event(ing, apiv1.EventTypeNormal, "Updated", fmt.Sprintf("Ingress %s", key))
		}
	case DeleteEvent:
		log.WithFields(log.Fields{"ingress": key}).Info("ingress has been deleted, will delete openstack resources")

		if err := c.deleteIngress(ing.Namespace, ing.Name); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to delete openstack resources for ingress %s: %v", key, err))
			c.recorder.Event(ing, apiv1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to delete openstack resources for ingress %s: %v", key, err))
		} else {
			c.recorder.Event(ing, apiv1.EventTypeNormal, "Deleted", fmt.Sprintf("Ingress %s", key))
		}
	}

	return nil
}

func (c *Controller) deleteIngress(namespace, name string) error {
	lbName := getResourceName(namespace, name, c.config.ClusterName)
	loadbalancer, err := c.osClient.GetLoadbalancerByName(lbName)
	if err != nil {
		if err != openstack.ErrNotFound {
			return fmt.Errorf("error getting loadbalancer %s: %v", name, err)
		}
		log.WithFields(log.Fields{"lbName": lbName, "ingressName": name, "namespace": namespace}).Info("loadbalancer for ingress deleted")
		return nil
	}

	if err = c.osClient.DeleteFloatingIP(loadbalancer.VipPortID); err != nil {
		return err
	}

	return c.osClient.DeleteLoadbalancer(loadbalancer.ID)
}

func (c *Controller) ensureIngress(ing *extv1beta1.Ingress) error {
	key := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
	name := getResourceName(ing.ObjectMeta.Namespace, ing.ObjectMeta.Name, c.config.ClusterName)

	lb, err := c.osClient.EnsureLoadBalancer(name, c.config.Octavia.SubnetID)
	if err != nil {
		return err
	}

	if strings.Contains(lb.Description, ing.ResourceVersion) {
		log.WithFields(log.Fields{"ingress": ing.Name}).Info("ingress not change")
		return nil
	}

	listener, err := c.osClient.EnsureListener(name, lb.ID)
	if err != nil {
		return err
	}

	// get nodes information
	nodeObjs, err := c.nodeLister.ListWithPredicate(getNodeConditionPredicate())
	if err != nil {
		return err
	}

	// Add default pool for the listener if 'backend' is defined
	if ing.Spec.Backend != nil {
		serviceName := fmt.Sprintf("%s/%s", ing.ObjectMeta.Namespace, ing.Spec.Backend.ServiceName)
		nodePort, err := c.getServiceNodePort(serviceName, ing.Spec.Backend.ServicePort)
		if err != nil {
			return err
		}

		if _, err = c.osClient.EnsurePoolMembers(false, name, lb.ID, listener.ID, &nodePort, nodeObjs); err != nil {
			return err
		}
	} else {
		// Delete default pool and its members
		if _, err = c.osClient.EnsurePoolMembers(true, name, lb.ID, "", nil, nil); err != nil {
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
			// make the pool name unique in the load balancer
			poolName := hash(fmt.Sprintf("%s+%s", path.Backend.ServiceName, path.Backend.ServicePort.String()))

			serviceName := fmt.Sprintf("%s/%s", ing.ObjectMeta.Namespace, path.Backend.ServiceName)
			nodePort, err := c.getServiceNodePort(serviceName, path.Backend.ServicePort)
			if err != nil {
				return err
			}

			poolID, err := c.osClient.EnsurePoolMembers(false, poolName, lb.ID, "", &nodePort, nodeObjs)
			if err != nil {
				return err
			}

			if err = c.osClient.CreatePolicyRules(lb.ID, listener.ID, *poolID, host, path.Path); err != nil {
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
	newIng, err := c.updateIngressStatus(ing, address)
	if err != nil {
		return err
	}
	c.recorder.Event(ing, apiv1.EventTypeNormal, "Updated", fmt.Sprintf("Successfully associated IP address %s to ingress %s", address, key))

	// Add ingress resource version to the load balancer description
	newDes := fmt.Sprintf("Created by Kubernetes ingress %s, version: %s", newIng.Name, newIng.ResourceVersion)
	if err = c.osClient.UpdateLoadBalancerDescription(lb.ID, newDes); err != nil {
		return err
	}

	log.WithFields(log.Fields{"ingress": newIng.Name, "lbID": lb.ID}).Info("openstack resources for ingress created")

	return nil
}

func (c *Controller) updateIngressStatus(ing *extv1beta1.Ingress, vip string) (*extv1beta1.Ingress, error) {
	newState := new(apiv1.LoadBalancerStatus)
	newState.Ingress = []apiv1.LoadBalancerIngress{{IP: vip}}
	newIng := ing.DeepCopy()
	newIng.Status.LoadBalancer = *newState

	newObj, err := c.kubeClient.ExtensionsV1beta1().Ingresses(newIng.Namespace).UpdateStatus(newIng)
	if err != nil {
		return nil, err
	}

	return newObj, nil
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

func (c *Controller) getServiceNodePort(name string, port intstr.IntOrString) (int, error) {
	svc, err := c.getService(name)
	if err != nil {
		return 0, err
	}

	var nodePort int
	ports := svc.Spec.Ports
	for _, p := range ports {
		if port.Type == intstr.Int && int(p.Port) == port.IntValue() {
			nodePort = int(p.NodePort)
			break
		}
		if port.Type == intstr.String && p.Name == port.StrVal {
			nodePort = int(p.NodePort)
			break
		}
	}

	if nodePort == 0 {
		return 0, fmt.Errorf("failed to find service node port")
	}

	return nodePort, nil
}
