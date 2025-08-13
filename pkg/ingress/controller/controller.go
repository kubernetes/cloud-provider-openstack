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
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/l7policies"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	nwv1 "k8s.io/api/networking/v1"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	nwlisters "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	klog "k8s.io/klog/v2"
	pkcs12 "software.sslmate.com/src/go-pkcs12"

	"k8s.io/cloud-provider-openstack/pkg/ingress/config"
	"k8s.io/cloud-provider-openstack/pkg/ingress/controller/openstack"
	"k8s.io/cloud-provider-openstack/pkg/ingress/utils"
	cpoerrors "k8s.io/cloud-provider-openstack/pkg/util/errors"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

const (
	// High enough QPS to fit all expected use cases. QPS=0 is not set here, because
	// client code is overriding it.
	defaultQPS = 1e6
	// High enough Burst to fit all expected use cases. Burst=0 is not set here, because
	// client code is overriding it.
	defaultBurst = 1e6

	maxRetries = 5

	// CreateEvent event associated with new objects in an informer
	CreateEvent EventType = "CREATE"
	// UpdateEvent event associated with an object update in an informer
	UpdateEvent EventType = "UPDATE"
	// DeleteEvent event associated when an object is removed from an informer
	DeleteEvent EventType = "DELETE"

	// IngressKey picks a specific "class" for the Ingress.
	// The controller only processes Ingresses with this annotation either
	// unset, or set to either the configured value or the empty string.
	IngressKey = "kubernetes.io/ingress.class"

	// IngressClass specifies which Ingress class we accept
	IngressClass = "openstack"

	// LabelNodeExcludeLB specifies that a node should not be used to create a Loadbalancer on
	// https://github.com/kubernetes/cloud-provider/blob/25867882d509131a6fdeaf812ceebfd0f19015dd/controllers/service/controller.go#L673
	LabelNodeExcludeLB = "node.kubernetes.io/exclude-from-external-load-balancers"

	// DeprecatedLabelNodeRoleMaster specifies that a node is a master
	// It's copied over to kubeadm until it's merged in core: https://github.com/kubernetes/kubernetes/pull/39112
	// Deprecated in favor of LabelNodeExcludeLB
	DeprecatedLabelNodeRoleMaster = "node-role.kubernetes.io/master"

	// IngressAnnotationInternal is the annotation used on the Ingress
	// to indicate that we want an internal loadbalancer service so that octavia-ingress-controller won't associate
	// floating ip to the load balancer VIP.
	// Default to true.
	IngressAnnotationInternal = "octavia.ingress.kubernetes.io/internal"

	// IngressAnnotationLoadBalancerKeepFloatingIP is the annotation used on the Ingress
	// to indicate that we want to keep the floatingIP after the ingress deletion. The Octavia LoadBalancer will be deleted
	// but not the floatingIP. That mean this floatingIP can be reused on another ingress without editing the dns area or update the whitelist.
	// Default to false.
	IngressAnnotationLoadBalancerKeepFloatingIP = "octavia.ingress.kubernetes.io/keep-floatingip"

	// IngressAnnotationFloatingIp is the key of the annotation on an ingress to set floating IP that will be associated to LoadBalancers.
	// If the floatingIP is not available, an error will be returned.
	IngressAnnotationFloatingIP = "octavia.ingress.kubernetes.io/floatingip"

	// IngressAnnotationSourceRangesKey is the key of the annotation on an ingress to set allowed IP ranges on their LoadBalancers.
	// It should be a comma-separated list of CIDRs.
	IngressAnnotationSourceRangesKey = "octavia.ingress.kubernetes.io/whitelist-source-range"

	// IngressControllerTag is added to the related resources.
	IngressControllerTag = "octavia.ingress.kubernetes.io"

	// IngressAnnotationTimeoutClientData is the timeout for frontend client inactivity.
	// If not set, this value defaults to the Octavia configuration key `timeout_client_data`.
	// Refer to https://docs.openstack.org/octavia/latest/configuration/configref.html#haproxy_amphora.timeout_client_data
	IngressAnnotationTimeoutClientData = "octavia.ingress.kubernetes.io/timeout-client-data"

	// IngressAnnotationTimeoutMemberData is the timeout for backend member inactivity.
	// If not set, this value defaults to the Octavia configuration key `timeout_member_data`.
	// Refer to https://docs.openstack.org/octavia/latest/configuration/configref.html#haproxy_amphora.timeout_member_data
	IngressAnnotationTimeoutMemberData = "octavia.ingress.kubernetes.io/timeout-member-data"

	// IngressAnnotationTimeoutMemberConnect is the timeout for backend member connection.
	// If not set, this value defaults to the Octavia configuration key `timeout_member_connect`.
	// Refer to https://docs.openstack.org/octavia/latest/configuration/configref.html#haproxy_amphora.timeout_member_connect
	IngressAnnotationTimeoutMemberConnect = "octavia.ingress.kubernetes.io/timeout-member-connect"

	// IngressAnnotationTimeoutTCPInspect is the time to wait for TCP packets for content inspection.
	// If not set, this value defaults to the Octavia configuration key `timeout_tcp_inspect`.
	// Refer to https://docs.openstack.org/octavia/latest/configuration/configref.html#haproxy_amphora.timeout_tcp_inspect
	IngressAnnotationTimeoutTCPInspect = "octavia.ingress.kubernetes.io/timeout-tcp-inspect"

	// IngressSecretCertName is certificate key name defined in the secret data.
	IngressSecretCertName = "tls.crt"
	// IngressSecretKeyName is private key name defined in the secret data.
	IngressSecretKeyName = "tls.key"

	// BarbicanSecretNameTemplate is the name format string to create Barbican secret.
	BarbicanSecretNameTemplate = "kube_ingress_%s_%s_%s_%s"
)

// EventType type of event associated with an informer
type EventType string

// Event holds the context of an event
type Event struct {
	Type EventType
	Obj  interface{}
}

// Controller ...
type Controller struct {
	knownNodes          []*apiv1.Node
	queue               workqueue.TypedRateLimitingInterface[any]
	informer            informers.SharedInformerFactory
	recorder            record.EventRecorder
	ingressLister       nwlisters.IngressLister
	ingressListerSynced cache.InformerSynced
	serviceLister       corelisters.ServiceLister
	serviceListerSynced cache.InformerSynced
	nodeLister          corelisters.NodeLister
	nodeListerSynced    cache.InformerSynced
	osClient            *openstack.OpenStack
	kubeClient          kubernetes.Interface
	config              config.Config
	subnetCIDR          string
}

// IsValid returns true if the given Ingress either doesn't specify
// the ingress.class annotation, or it's set to the configured in the
// ingress controller.
func IsValid(ing *nwv1.Ingress) bool {
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

type NodeConditionPredicate func(node *apiv1.Node) bool

// listWithPredicate gets nodes that matches predicate function.
func listWithPredicate(nodeLister corelisters.NodeLister, predicate NodeConditionPredicate) ([]*apiv1.Node, error) {
	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var filtered []*apiv1.Node
	for i := range nodes {
		if predicate(nodes[i]) {
			filtered = append(filtered, nodes[i])
		}
	}

	return filtered, nil
}

func getNodeConditionPredicate() NodeConditionPredicate {
	return func(node *apiv1.Node) bool {
		// We add the master to the node list, but its unschedulable.  So we use this to filter
		// the master.
		if node.Spec.Unschedulable {
			return false
		}

		// Recognize nodes labeled as not suitable for LB, and filter them also, as we were doing previously.
		if _, hasExcludeLBRoleLabel := node.Labels[LabelNodeExcludeLB]; hasExcludeLBRoleLabel {
			return false
		}

		// Deprecated in favor of LabelNodeExcludeLB, kept for consistency and will be removed later
		if _, hasNodeRoleMasterLabel := node.Labels[DeprecatedLabelNodeRoleMaster]; hasNodeRoleMasterLabel {
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

func getNodeAddressForLB(node *apiv1.Node) (string, error) {
	addrs := node.Status.Addresses
	if len(addrs) == 0 {
		return "", errors.New("no address found for host")
	}

	for _, addr := range addrs {
		if addr.Type == apiv1.NodeInternalIP {
			return addr.Address, nil
		}
	}

	return addrs[0].Address, nil
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
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "openstack-ingress-controller"})

	controller := &Controller{
		config:              conf,
		queue:               queue,
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

	ingInformer := kubeInformerFactory.Networking().V1().Ingresses()
	_, err = ingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*nwv1.Ingress)
			key := fmt.Sprintf("%s/%s", addIng.Namespace, addIng.Name)

			if !IsValid(addIng) {
				log.Infof("ignore ingress %s", key)
				return
			}

			recorder.Event(addIng, apiv1.EventTypeNormal, "Creating", fmt.Sprintf("Ingress %s", key))
			controller.queue.AddRateLimited(Event{Obj: addIng, Type: CreateEvent})
		},
		UpdateFunc: func(old, new interface{}) {
			newIng := new.(*nwv1.Ingress)
			oldIng := old.(*nwv1.Ingress)
			if newIng.ResourceVersion == oldIng.ResourceVersion {
				// Periodic resync will send update events for all known Ingresses.
				// Two different versions of the same Ingress will always have different RVs.
				return
			}
			newAnnotations := newIng.Annotations
			oldAnnotations := oldIng.Annotations
			delete(newAnnotations, "kubectl.kubernetes.io/last-applied-configuration")
			delete(oldAnnotations, "kubectl.kubernetes.io/last-applied-configuration")

			key := fmt.Sprintf("%s/%s", newIng.Namespace, newIng.Name)
			validOld := IsValid(oldIng)
			validCur := IsValid(newIng)
			if !validOld && validCur {
				recorder.Event(newIng, apiv1.EventTypeNormal, "Creating", fmt.Sprintf("Ingress %s", key))
				controller.queue.AddRateLimited(Event{Obj: newIng, Type: CreateEvent})
			} else if validOld && !validCur {
				recorder.Event(newIng, apiv1.EventTypeNormal, "Deleting", fmt.Sprintf("Ingress %s", key))
				controller.queue.AddRateLimited(Event{Obj: newIng, Type: DeleteEvent})
			} else if validCur && (!reflect.DeepEqual(newIng.Spec, oldIng.Spec) || !reflect.DeepEqual(newAnnotations, oldAnnotations)) {
				recorder.Event(newIng, apiv1.EventTypeNormal, "Updating", fmt.Sprintf("Ingress %s", key))
				controller.queue.AddRateLimited(Event{Obj: newIng, Type: UpdateEvent})
			} else {
				return
			}
		},
		DeleteFunc: func(obj interface{}) {
			delIng, ok := obj.(*nwv1.Ingress)
			if !ok {
				// If we reached here it means the ingress was deleted but its final state is unrecorded.
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					log.Errorf("couldn't get object from tombstone %#v", obj)
					return
				}
				delIng, ok = tombstone.Obj.(*nwv1.Ingress)
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

	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("failed to initialize ingress")
	}

	controller.ingressLister = ingInformer.Lister()
	controller.ingressListerSynced = ingInformer.Informer().HasSynced

	return controller
}

// Start starts the openstack ingress controller.
func (c *Controller) Start(ctx context.Context) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Debug("starting Ingress controller")
	go c.informer.Start(ctx.Done())

	// wait for the caches to synchronize before starting the worker
	if !cache.WaitForCacheSync(ctx.Done(), c.ingressListerSynced, c.serviceListerSynced, c.nodeListerSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	log.Info("ingress controller synced and ready")

	readyWorkerNodes, err := listWithPredicate(c.nodeLister, getNodeConditionPredicate())
	if err != nil {
		log.Errorf("Failed to retrieve current set of nodes from node lister: %v", err)
		return
	}
	c.knownNodes = readyWorkerNodes

	// Get subnet CIDR. The subnet CIDR will be used as source IP range for related security group rules.
	subnet, err := c.osClient.GetSubnet(ctx, c.config.Octavia.SubnetID)
	if err != nil {
		log.Errorf("Failed to retrieve the subnet %s: %v", c.config.Octavia.SubnetID, err)
		return
	}
	c.subnetCIDR = subnet.CIDR

	go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	go wait.UntilWithContext(ctx, c.nodeSyncLoop, 60*time.Second)

	<-ctx.Done()
}

// nodeSyncLoop handles updating the hosts pointed to by all load
// balancers whenever the set of nodes in the cluster changes.
func (c *Controller) nodeSyncLoop(ctx context.Context) {
	readyWorkerNodes, err := listWithPredicate(c.nodeLister, getNodeConditionPredicate())
	if err != nil {
		log.Errorf("Failed to retrieve current set of nodes from node lister: %v", err)
		return
	}
	if utils.NodeSlicesEqual(readyWorkerNodes, c.knownNodes) {
		return
	}

	log.Infof("Detected change in list of current cluster nodes. New node set: %v", utils.NodeNames(readyWorkerNodes))

	// if no new nodes, then avoid update member
	if len(readyWorkerNodes) == 0 {
		c.knownNodes = readyWorkerNodes
		log.Info("Finished to handle node change, it's [] now")
		return
	}

	var ings *nwv1.IngressList
	// NOTE(lingxiankong): only take ingresses without ip address into consideration
	opts := apimetav1.ListOptions{}
	if ings, err = c.kubeClient.NetworkingV1().Ingresses("").List(ctx, opts); err != nil {
		log.Errorf("Failed to retrieve current set of ingresses: %v", err)
		return
	}

	// Update each valid ingress
	for _, ing := range ings.Items {
		if !IsValid(&ing) {
			continue
		}

		log.WithFields(log.Fields{"ingress": ing.Name, "namespace": ing.Namespace}).Debug("Starting to handle ingress")

		lbName := utils.GetResourceName(ing.Namespace, ing.Name, c.config.ClusterName)
		loadbalancer, err := openstackutil.GetLoadbalancerByName(ctx, c.osClient.Octavia, lbName)
		if err != nil {
			if err != cpoerrors.ErrNotFound {
				log.WithFields(log.Fields{"name": lbName}).Errorf("Failed to retrieve loadbalancer from OpenStack: %v", err)
			}

			// If lb doesn't exist or error occurred, continue
			continue
		}

		if err = c.osClient.UpdateLoadbalancerMembers(ctx, loadbalancer.ID, readyWorkerNodes); err != nil {
			log.WithFields(log.Fields{"ingress": ing.Name}).Error("Failed to handle ingress")
			continue
		}

		log.WithFields(log.Fields{"ingress": ing.Name, "namespace": ing.Namespace}).Info("Finished to handle ingress")
	}

	c.knownNodes = readyWorkerNodes

	log.Info("Finished to handle node change")
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
		// continue looping
	}
}

func (c *Controller) processNextItem(ctx context.Context) bool {
	obj, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(obj)

	err := c.processItem(ctx, obj.(Event))
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

func (c *Controller) processItem(ctx context.Context, event Event) error {
	ing := event.Obj.(*nwv1.Ingress)
	key := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
	logger := log.WithFields(log.Fields{"ingress": key})

	switch event.Type {
	case CreateEvent:
		logger.Info("creating ingress")

		if err := c.ensureIngress(ctx, ing); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to create openstack resources for ingress %s: %v", key, err))
			c.recorder.Event(ing, apiv1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to create openstack resources for ingress %s: %v", key, err))
		} else {
			c.recorder.Event(ing, apiv1.EventTypeNormal, "Created", fmt.Sprintf("Ingress %s", key))
		}
	case UpdateEvent:
		logger.Info("updating ingress")

		if err := c.ensureIngress(ctx, ing); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to update openstack resources for ingress %s: %v", key, err))
			c.recorder.Event(ing, apiv1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to update openstack resources for ingress %s: %v", key, err))
		} else {
			c.recorder.Event(ing, apiv1.EventTypeNormal, "Updated", fmt.Sprintf("Ingress %s", key))
		}
	case DeleteEvent:
		logger.Info("deleting ingress")

		if err := c.deleteIngress(ctx, ing); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to delete openstack resources for ingress %s: %v", key, err))
			c.recorder.Event(ing, apiv1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to delete openstack resources for ingress %s: %v", key, err))
		} else {
			c.recorder.Event(ing, apiv1.EventTypeNormal, "Deleted", fmt.Sprintf("Ingress %s", key))
		}
	}

	return nil
}

func (c *Controller) deleteIngress(ctx context.Context, ing *nwv1.Ingress) error {
	key := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
	lbName := utils.GetResourceName(ing.Namespace, ing.Name, c.config.ClusterName)
	logger := log.WithFields(log.Fields{"ingress": key})

	// If load balancer doesn't exist, assume it's already deleted.
	loadbalancer, err := openstackutil.GetLoadbalancerByName(ctx, c.osClient.Octavia, lbName)
	if err != nil {
		if err != cpoerrors.ErrNotFound {
			return fmt.Errorf("error getting loadbalancer %s: %v", ing.Name, err)
		}

		logger.WithFields(log.Fields{"lbName": lbName}).Info("loadbalancer for ingress deleted")
		return nil
	}

	// Manage the floatingIP
	keepFloatingSetting := getStringFromIngressAnnotation(ing, IngressAnnotationLoadBalancerKeepFloatingIP, "false")
	keepFloating, err := strconv.ParseBool(keepFloatingSetting)
	if err != nil {
		return fmt.Errorf("unknown annotation %s: %v", IngressAnnotationLoadBalancerKeepFloatingIP, err)
	}

	if !keepFloating {
		// Delete the floating IP for the load balancer VIP. We don't check if the Ingress is internal or not, just delete
		// any floating IPs associated with the load balancer VIP port.
		logger.WithFields(log.Fields{"lbID": loadbalancer.ID, "VIP": loadbalancer.VipAddress}).Info("deleting floating IPs associated with the load balancer VIP port")

		if _, err = c.osClient.EnsureFloatingIP(ctx, true, loadbalancer.VipPortID, "", "", ""); err != nil {
			return fmt.Errorf("failed to delete floating IP: %v", err)
		}

		logger.WithFields(log.Fields{"lbID": loadbalancer.ID}).Info("VIP or floating IP deleted")
	}

	// Delete security group managed for the Ingress backend service
	if c.config.Octavia.ManageSecurityGroups {
		sgTags := []string{IngressControllerTag, fmt.Sprintf("%s_%s", ing.Namespace, ing.Name)}
		tagString := strings.Join(sgTags, ",")
		opts := groups.ListOpts{Tags: tagString}
		sgs, err := c.osClient.GetSecurityGroups(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to get security groups for ingress %s: %v", key, err)
		}

		nodes, err := listWithPredicate(c.nodeLister, getNodeConditionPredicate())
		if err != nil {
			return fmt.Errorf("failed to get nodes: %v", err)
		}

		for _, sg := range sgs {
			if err = c.osClient.EnsurePortSecurityGroup(ctx, true, sg.ID, nodes); err != nil {
				return fmt.Errorf("failed to operate on the port security groups for ingress %s: %v", key, err)
			}
			if _, err = c.osClient.EnsureSecurityGroup(ctx, true, "", "", sgTags); err != nil {
				return fmt.Errorf("failed to delete the security groups for ingress %s: %v", key, err)
			}
		}

		logger.WithFields(log.Fields{"lbID": loadbalancer.ID}).Info("security group deleted")
	}

	err = openstackutil.DeleteLoadbalancer(ctx, c.osClient.Octavia, loadbalancer.ID, true)
	if err != nil {
		logger.WithFields(log.Fields{"lbID": loadbalancer.ID}).Infof("loadbalancer delete failed: %v", err)
	} else {
		logger.WithFields(log.Fields{"lbID": loadbalancer.ID}).Info("loadbalancer deleted")
	}

	// Delete Barbican secrets
	if c.osClient.Barbican != nil && ing.Spec.TLS != nil {
		nameFilter := fmt.Sprintf("kube_ingress_%s_%s_%s", c.config.ClusterName, ing.Namespace, ing.Name)
		if err := openstackutil.DeleteSecrets(ctx, c.osClient.Barbican, nameFilter); err != nil {
			return fmt.Errorf("failed to remove Barbican secrets: %v", err)
		}

		logger.Info("Barbican secrets deleted")
	}

	return err
}

func (c *Controller) toBarbicanSecret(ctx context.Context, name string, namespace string, toSecretName string) (string, error) {
	secret, err := c.kubeClient.CoreV1().Secrets(namespace).Get(ctx, name, apimetav1.GetOptions{})
	if err != nil {
		// TODO(lingxiankong): Creating secret on the fly not supported yet.
		return "", err
	}

	var pk crypto.PrivateKey
	if keyBytes, isPresent := secret.Data[IngressSecretKeyName]; isPresent {
		pk, err = privateKeyFromPEM(keyBytes)
		if err != nil {
			return "", err
		}
	} else {
		return "", fmt.Errorf("%s key doesn't exist in the secret %s", IngressSecretKeyName, name)
	}

	var cb []*x509.Certificate
	if certBytes, isPresent := secret.Data[IngressSecretCertName]; isPresent {
		cb, err = parsePEMBundle(certBytes)
		if err != nil {
			return "", err
		}
	} else {
		return "", fmt.Errorf("%s key doesn't exist in the secret %s", IngressSecretCertName, name)
	}

	var caCerts []*x509.Certificate
	// We assume that the rest of the PEM bundle contains the CA certificate.
	if len(cb) > 1 {
		caCerts = append(caCerts, cb[1:]...)
	}

	pfxData, err := pkcs12.LegacyRC2.WithRand(rand.Reader).Encode(pk, cb[0], caCerts, "")
	if err != nil {
		return "", fmt.Errorf("failed to create PKCS#12 bundle: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(pfxData)

	return openstackutil.EnsureSecret(ctx, c.osClient.Barbican, toSecretName, "application/octet-stream", encoded)
}

func (c *Controller) ensureIngress(ctx context.Context, ing *nwv1.Ingress) error {
	ingName := ing.Name
	ingNamespace := ing.Namespace
	clusterName := c.config.ClusterName

	ingfullName := fmt.Sprintf("%s/%s", ingNamespace, ingName)
	resName := utils.GetResourceName(ingNamespace, ingName, clusterName)

	if len(ing.Spec.TLS) > 0 && c.osClient.Barbican == nil {
		return fmt.Errorf("TLS Ingress not supported because of Key Manager service unavailable")
	}

	lb, err := c.osClient.EnsureLoadBalancer(ctx, resName, c.config.Octavia.SubnetID, ingNamespace, ingName, clusterName, c.config.Octavia.FlavorID)
	if err != nil {
		return err
	}

	logger := log.WithFields(log.Fields{"ingress": ingfullName, "lbID": lb.ID})

	if strings.Contains(lb.Description, ing.ResourceVersion) {
		logger.Info("ingress not changed")
		return nil
	}

	var nodePorts []int
	var sgID string

	if c.config.Octavia.ManageSecurityGroups {
		logger.Info("ensuring security group")

		sgDescription := fmt.Sprintf("Security group created for Ingress %s from cluster %s", ingfullName, clusterName)
		sgTags := []string{IngressControllerTag, fmt.Sprintf("%s_%s", ingNamespace, ingName)}
		sgID, err = c.osClient.EnsureSecurityGroup(ctx, false, resName, sgDescription, sgTags)
		if err != nil {
			return fmt.Errorf("failed to prepare the security group for the ingress %s: %v", ingfullName, err)
		}

		logger.WithFields(log.Fields{"sgID": sgID}).Info("ensured security group")
	}

	// Convert kubernetes secrets to barbican ones
	var secretRefs []string
	for _, tls := range ing.Spec.TLS {
		secretName := fmt.Sprintf(BarbicanSecretNameTemplate, clusterName, ingNamespace, ingName, tls.SecretName)
		secretRef, err := c.toBarbicanSecret(ctx, tls.SecretName, ingNamespace, secretName)
		if err != nil {
			return fmt.Errorf("failed to create Barbican secret: %v", err)
		}

		logger.WithFields(log.Fields{"secretName": secretName, "secretRef": secretRef}).Info("secret created in Barbican")

		secretRefs = append(secretRefs, secretRef)
	}
	port := 80
	if len(secretRefs) > 0 {
		port = 443
	}

	// Create listener
	sourceRanges := getStringFromIngressAnnotation(ing, IngressAnnotationSourceRangesKey, "0.0.0.0/0")
	timeoutClientData := maybeGetIntFromIngressAnnotation(ing, IngressAnnotationTimeoutClientData)
	timeoutMemberConnect := maybeGetIntFromIngressAnnotation(ing, IngressAnnotationTimeoutMemberConnect)
	timeoutMemberData := maybeGetIntFromIngressAnnotation(ing, IngressAnnotationTimeoutMemberData)
	timeoutTCPInspect := maybeGetIntFromIngressAnnotation(ing, IngressAnnotationTimeoutTCPInspect)

	listenerAllowedCIDRs := strings.Split(sourceRanges, ",")
	listener, err := c.osClient.EnsureListener(ctx, resName, lb.ID, secretRefs, listenerAllowedCIDRs, timeoutClientData, timeoutMemberData, timeoutTCPInspect, timeoutMemberConnect)
	if err != nil {
		return err
	}

	// get nodes information and prepare update member params.
	nodeObjs, err := listWithPredicate(c.nodeLister, getNodeConditionPredicate())
	if err != nil {
		return err
	}
	var updateMemberOpts []pools.BatchUpdateMemberOpts
	for _, node := range nodeObjs {
		addr, err := getNodeAddressForLB(node)
		if err != nil {
			// Node failure, do not create member
			logger.WithFields(log.Fields{"node": node.Name, "error": err}).Warn("failed to get node address")
			continue
		}
		nodeName := node.Name
		member := pools.BatchUpdateMemberOpts{
			Name:    &nodeName,
			Address: addr,
		}
		updateMemberOpts = append(updateMemberOpts, member)
	}
	// only allow >= 1 members or it will lead to openstack octavia issue
	if len(updateMemberOpts) == 0 {
		return fmt.Errorf("no available nodes")
	}

	// Get all the existing pools and l7 policies
	var newPools []openstack.IngPool
	var newPolicies []openstack.IngPolicy
	var oldPolicies []openstack.ExistingPolicy

	existingPolicies, err := openstackutil.GetL7policies(ctx, c.osClient.Octavia, listener.ID)
	if err != nil {
		return fmt.Errorf("failed to get l7 policies for listener %s", listener.ID)
	}
	for _, policy := range existingPolicies {
		rules, err := openstackutil.GetL7Rules(ctx, c.osClient.Octavia, policy.ID)
		if err != nil {
			return fmt.Errorf("failed to get l7 rules for policy %s", policy.ID)
		}
		oldPolicies = append(oldPolicies, openstack.ExistingPolicy{
			Policy: policy,
			Rules:  rules,
		})
	}

	existingPools, err := openstackutil.GetPools(ctx, c.osClient.Octavia, lb.ID)
	if err != nil {
		return fmt.Errorf("failed to get pools from load balancer %s, error: %v", lb.ID, err)
	}

	// Add default pool for the listener if 'backend' is defined
	if ing.Spec.DefaultBackend != nil {
		poolName := utils.Hash(fmt.Sprintf("%s+%s", ing.Spec.DefaultBackend.Service.Name, ing.Spec.DefaultBackend.Service.Port.String()))

		serviceName := fmt.Sprintf("%s/%s", ingNamespace, ing.Spec.DefaultBackend.Service.Name)
		nodePort, err := c.getServiceNodePort(serviceName, ing.Spec.DefaultBackend.Service)
		if err != nil {
			return err
		}
		nodePorts = append(nodePorts, nodePort)

		var members = make([]pools.BatchUpdateMemberOpts, len(updateMemberOpts))
		copy(members, updateMemberOpts)
		for index := range members {
			members[index].ProtocolPort = nodePort
		}

		// This pool is the default pool of the listener.
		newPools = append(newPools, openstack.IngPool{
			Name: poolName,
			Opts: pools.CreateOpts{
				Name:        poolName,
				Protocol:    "HTTP",
				LBMethod:    pools.LBMethodRoundRobin,
				ListenerID:  listener.ID,
				Persistence: nil,
			},
			PoolMembers: members,
		})
	}

	// Add l7 load balancing rules. Each host and path pair is mapped to a l7 policy in octavia,
	// which contains two rules(with type 'HOST_NAME' and 'PATH' respectively)
	for _, rule := range ing.Spec.Rules {
		host := rule.Host

		for _, path := range rule.HTTP.Paths {
			var policyRules []l7policies.CreateRuleOpts

			if host != "" {
				policyRules = append(policyRules, l7policies.CreateRuleOpts{
					RuleType:    l7policies.TypeHostName,
					CompareType: l7policies.CompareTypeRegex,
					Value:       fmt.Sprintf("^%s(:%d)?$", strings.ReplaceAll(host, ".", "\\."), port)})
			}

			// make the pool name unique in the load balancer
			poolName := utils.Hash(fmt.Sprintf("%s+%s", path.Backend.Service.Name, path.Backend.Service.Port.String()))

			serviceName := fmt.Sprintf("%s/%s", ingNamespace, path.Backend.Service.Name)
			nodePort, err := c.getServiceNodePort(serviceName, path.Backend.Service)
			if err != nil {
				return err
			}
			nodePorts = append(nodePorts, nodePort)

			var members = make([]pools.BatchUpdateMemberOpts, len(updateMemberOpts))
			copy(members, updateMemberOpts)
			for index := range members {
				members[index].ProtocolPort = nodePort
			}

			// The pool is a shared pool in a load balancer.
			newPools = append(newPools, openstack.IngPool{
				Name: poolName,
				Opts: pools.CreateOpts{
					Name:           poolName,
					Protocol:       "HTTP",
					LBMethod:       pools.LBMethodRoundRobin,
					LoadbalancerID: lb.ID,
					Persistence:    nil,
				},
				PoolMembers: members,
			})

			policyRules = append(policyRules, l7policies.CreateRuleOpts{
				RuleType:    l7policies.TypePath,
				CompareType: l7policies.CompareTypeStartWith,
				Value:       path.Path,
			})

			newPolicies = append(newPolicies, openstack.IngPolicy{
				RedirectPoolName: poolName,
				Opts: l7policies.CreateOpts{
					ListenerID:  listener.ID,
					Action:      l7policies.ActionRedirectToPool,
					Description: "Created by kubernetes ingress",
				},
				RulesOpts: policyRules,
			})
		}
	}

	// Reconcile octavia resources.
	rt := openstack.NewResourceTracker(ingfullName, c.osClient.Octavia, lb.ID, listener.ID, newPools, newPolicies, existingPools, oldPolicies)
	if err := rt.CreateResources(ctx); err != nil {
		return err
	}
	if err := rt.CleanupResources(ctx); err != nil {
		return err
	}

	if c.config.Octavia.ManageSecurityGroups {
		logger.WithFields(log.Fields{"sgID": sgID}).Info("ensuring security group rules")

		if err := c.osClient.EnsureSecurityGroupRules(ctx, sgID, c.subnetCIDR, nodePorts); err != nil {
			return fmt.Errorf("failed to ensure security group rules for Ingress %s: %v", ingName, err)
		}

		if err := c.osClient.EnsurePortSecurityGroup(ctx, false, sgID, nodeObjs); err != nil {
			return fmt.Errorf("failed to operate port security group for Ingress %s: %v", ingName, err)
		}

		logger.WithFields(log.Fields{"sgID": sgID}).Info("ensured security group rules")
	}

	internalSetting := getStringFromIngressAnnotation(ing, IngressAnnotationInternal, "true")
	isInternal, err := strconv.ParseBool(internalSetting)
	if err != nil {
		return fmt.Errorf("unknown annotation %s: %v", IngressAnnotationInternal, err)
	}

	address := lb.VipAddress
	// Allocate floating ip for loadbalancer vip if the external network is configured and the Ingress is not internal.
	if !isInternal && c.config.Octavia.FloatingIPNetwork != "" {

		floatingIPSetting := getStringFromIngressAnnotation(ing, IngressAnnotationFloatingIP, "")
		if err != nil {
			return fmt.Errorf("unknown annotation %s: %v", IngressAnnotationFloatingIP, err)
		}

		description := fmt.Sprintf("Floating IP for Kubernetes ingress %s in namespace %s from cluster %s", ingName, ingNamespace, clusterName)

		if floatingIPSetting != "" {
			logger.Info("try to use floating IP: ", floatingIPSetting)
		} else {
			logger.Info("creating new floating IP")
		}
		address, err = c.osClient.EnsureFloatingIP(ctx, false, lb.VipPortID, floatingIPSetting, c.config.Octavia.FloatingIPNetwork, description)
		if err != nil {
			return fmt.Errorf("failed to use provided floating IP %s : %v", floatingIPSetting, err)
		}
		logger.Info("floating IP ", address, " configured")
	}

	// Update ingress status
	newIng, err := c.updateIngressStatus(ctx, ing, address)
	if err != nil {
		return err
	}
	c.recorder.Event(ing, apiv1.EventTypeNormal, "Updated", fmt.Sprintf("Successfully associated IP address %s to ingress %s", address, ingfullName))

	// Add ingress resource version to the load balancer description
	newDes := fmt.Sprintf("Kubernetes Ingress %s in namespace %s from cluster %s, version: %s", ingName, ingNamespace, clusterName, newIng.ResourceVersion)
	if err = c.osClient.UpdateLoadBalancerDescription(ctx, lb.ID, newDes); err != nil {
		return err
	}

	logger.Info("openstack resources for ingress created")

	return nil
}

func (c *Controller) updateIngressStatus(ctx context.Context, ing *nwv1.Ingress, vip string) (*nwv1.Ingress, error) {
	newState := new(nwv1.IngressLoadBalancerStatus)
	newState.Ingress = []nwv1.IngressLoadBalancerIngress{{IP: vip}}
	newIng := ing.DeepCopy()
	newIng.Status.LoadBalancer = *newState

	newObj, err := c.kubeClient.NetworkingV1().Ingresses(newIng.Namespace).UpdateStatus(ctx, newIng, apimetav1.UpdateOptions{})
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

func (c *Controller) getServiceNodePort(name string, serviceBackend *nwv1.IngressServiceBackend) (int, error) {
	var portInfo intstr.IntOrString
	if serviceBackend.Port.Name != "" {
		portInfo.Type = intstr.String
		portInfo.StrVal = serviceBackend.Port.Name
	} else {
		portInfo.Type = intstr.Int
		portInfo.IntVal = serviceBackend.Port.Number
	}

	logger := log.WithFields(log.Fields{"service": name, "port": portInfo.String()})

	logger.Debug("getting service nodeport")

	svc, err := c.getService(name)
	if err != nil {
		return 0, err
	}

	var nodePort int
	ports := svc.Spec.Ports
	for _, p := range ports {
		if portInfo.Type == intstr.Int && int(p.Port) == portInfo.IntValue() {
			nodePort = int(p.NodePort)
			break
		}
		if portInfo.Type == intstr.String && p.Name == portInfo.StrVal {
			nodePort = int(p.NodePort)
			break
		}
	}

	if nodePort == 0 {
		return 0, fmt.Errorf("failed to find nodeport for service %s", name)
	}

	logger.Debug("found service nodeport")

	return nodePort, nil
}

// getStringFromIngressAnnotation searches a given Ingress for a specific annotationKey and either returns the
// annotation's value or a specified defaultSetting
func getStringFromIngressAnnotation(ingress *nwv1.Ingress, annotationKey string, defaultValue string) string {
	if annotationValue, ok := ingress.Annotations[annotationKey]; ok {
		return annotationValue
	}

	return defaultValue
}

// maybeGetIntFromIngressAnnotation searches a given Ingress for a specific annotationKey and either returns the
// annotation's value
func maybeGetIntFromIngressAnnotation(ingress *nwv1.Ingress, annotationKey string) *int {
	klog.V(4).Infof("maybeGetIntFromIngressAnnotation(%s/%s, %v33)", ingress.Namespace, ingress.Name, annotationKey)
	if annotationValue, ok := ingress.Annotations[annotationKey]; ok {
		klog.V(4).Infof("Found a Service Annotation for key: %v", annotationKey)
		returnValue, err := strconv.Atoi(annotationValue)
		if err != nil {
			klog.V(4).Infof("Invalid integer found on Service Annotation: %v = %v", annotationKey, annotationValue)
			return nil
		}
		return &returnValue
	}
	klog.V(4).Infof("Could not find a Service Annotation; falling back to default setting for annotation %v", annotationKey)
	return nil
}

// privateKeyFromPEM converts a PEM block into a crypto.PrivateKey.
func privateKeyFromPEM(pemData []byte) (crypto.PrivateKey, error) {
	var result *pem.Block
	rest := pemData
	for {
		result, rest = pem.Decode(rest)
		if result == nil {
			return nil, fmt.Errorf("cannot decode supplied PEM data")
		}

		switch result.Type {
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(result.Bytes)
		case "EC PRIVATE KEY":
			return x509.ParseECPrivateKey(result.Bytes)
		case "PRIVATE KEY":
			return x509.ParsePKCS8PrivateKey(result.Bytes)
		}
	}
}

// parsePEMBundle parses a certificate bundle from top to bottom and returns
// a slice of x509 certificates. This function will error if no certificates are found.
func parsePEMBundle(bundle []byte) ([]*x509.Certificate, error) {
	var certificates []*x509.Certificate
	var certDERBlock *pem.Block

	for {
		certDERBlock, bundle = pem.Decode(bundle)
		if certDERBlock == nil {
			break
		}

		if certDERBlock.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(certDERBlock.Bytes)
			if err != nil {
				return nil, err
			}
			certificates = append(certificates, cert)
		}
	}

	if len(certificates) == 0 {
		return nil, fmt.Errorf("no certificates were found while parsing the bundle")
	}

	return certificates, nil
}
