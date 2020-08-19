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
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	nwv1beta1 "k8s.io/api/networking/v1beta1"
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
	extlisters "k8s.io/client-go/listers/networking/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	pkcs12 "software.sslmate.com/src/go-pkcs12"

	"k8s.io/cloud-provider-openstack/pkg/ingress/config"
	"k8s.io/cloud-provider-openstack/pkg/ingress/controller/openstack"
	"k8s.io/cloud-provider-openstack/pkg/ingress/utils"
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

	// LabelNodeRoleMaster specifies that a node is a master
	// It's copied over to kubeadm until it's merged in core: https://github.com/kubernetes/kubernetes/pull/39112
	LabelNodeRoleMaster = "node-role.kubernetes.io/master"

	// IngressAnnotationInternal is the annotation used on the Ingress
	// to indicate that we want an internal loadbalancer service so that octavia-ingress-controller won't associate
	// floating ip to the load balancer VIP.
	// Default to true.
	IngressAnnotationInternal = "octavia.ingress.kubernetes.io/internal"

	// IngressControllerTag is added to the related resources.
	IngressControllerTag = "octavia.ingress.kubernetes.io"

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
	subnetCIDR          string
}

// IsValid returns true if the given Ingress either doesn't specify
// the ingress.class annotation, or it's set to the configured in the
// ingress controller.
func IsValid(ing *nwv1beta1.Ingress) bool {
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

	ingInformer := kubeInformerFactory.Networking().V1beta1().Ingresses()
	ingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*nwv1beta1.Ingress)
			key := fmt.Sprintf("%s/%s", addIng.Namespace, addIng.Name)

			if !IsValid(addIng) {
				log.Infof("ignore ingress %s", key)
				return
			}

			recorder.Event(addIng, apiv1.EventTypeNormal, "Creating", fmt.Sprintf("Ingress %s", key))
			controller.queue.AddRateLimited(Event{Obj: addIng, Type: CreateEvent})
		},
		UpdateFunc: func(old, new interface{}) {
			newIng := new.(*nwv1beta1.Ingress)
			oldIng := old.(*nwv1beta1.Ingress)
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
			delIng, ok := obj.(*nwv1beta1.Ingress)
			if !ok {
				// If we reached here it means the ingress was deleted but its final state is unrecorded.
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					log.Errorf("couldn't get object from tombstone %#v", obj)
					return
				}
				delIng, ok = tombstone.Obj.(*nwv1beta1.Ingress)
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

	readyWorkerNodes, err := listWithPredicate(c.nodeLister, getNodeConditionPredicate())
	if err != nil {
		log.Errorf("Failed to retrieve current set of nodes from node lister: %v", err)
		return
	}
	c.knownNodes = readyWorkerNodes

	// Get subnet CIDR. The subnet CIDR will be used as source IP range for related security group rules.
	subnet, err := c.osClient.GetSubnet(c.config.Octavia.SubnetID)
	if err != nil {
		log.Errorf("Failed to retrieve the subnet %s: %v", c.config.Octavia.SubnetID, err)
		return
	}
	c.subnetCIDR = subnet.CIDR

	go wait.Until(c.runWorker, time.Second, c.stopCh)
	go wait.Until(c.nodeSyncLoop, 60*time.Second, c.stopCh)

	<-c.stopCh
}

// nodeSyncLoop handles updating the hosts pointed to by all load
// balancers whenever the set of nodes in the cluster changes.
func (c *Controller) nodeSyncLoop() {
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

	ings := new(nwv1beta1.IngressList)
	// NOTE(lingxiankong): only take ingresses without ip address into consideration
	opts := apimetav1.ListOptions{}
	if ings, err = c.kubeClient.NetworkingV1beta1().Ingresses("").List(context.TODO(), opts); err != nil {
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
		loadbalancer, err := openstackutil.GetLoadbalancerByName(c.osClient.Octavia, lbName)
		if err != nil {
			if err != openstackutil.ErrNotFound {
				log.WithFields(log.Fields{"name": lbName}).Errorf("Failed to retrieve loadbalancer from OpenStack: %v", err)
			}

			// If lb doesn't exist or error occurred, continue
			continue
		}

		if err = c.osClient.UpdateLoadbalancerMembers(loadbalancer.ID, readyWorkerNodes); err != nil {
			log.WithFields(log.Fields{"ingress": ing.Name}).Error("Failed to handle ingress")
			continue
		}

		log.WithFields(log.Fields{"ingress": ing.Name, "namespace": ing.Namespace}).Info("Finished to handle ingress")
	}

	c.knownNodes = readyWorkerNodes

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
	ing := event.Obj.(*nwv1beta1.Ingress)
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

		if err := c.deleteIngress(ing); err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to delete openstack resources for ingress %s: %v", key, err))
			c.recorder.Event(ing, apiv1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to delete openstack resources for ingress %s: %v", key, err))
		} else {
			c.recorder.Event(ing, apiv1.EventTypeNormal, "Deleted", fmt.Sprintf("Ingress %s", key))
		}
	}

	return nil
}

func (c *Controller) deleteIngress(ing *nwv1beta1.Ingress) error {
	key := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
	lbName := utils.GetResourceName(ing.Namespace, ing.Name, c.config.ClusterName)

	// Delete Barbican secrets
	if c.osClient.Barbican != nil {
		nameFilter := fmt.Sprintf("kube_ingress_%s_%s_%s", c.config.ClusterName, ing.Namespace, ing.Name)
		if err := openstackutil.DeleteSecrets(c.osClient.Barbican, nameFilter); err != nil {
			return fmt.Errorf("failed to remove Barbican secrets: %v", err)
		}

		log.WithFields(log.Fields{"ingress": key}).Info("Barbican secrets deleted")
	}

	// If load balancer doesn't exist, assume it's already deleted.
	loadbalancer, err := openstackutil.GetLoadbalancerByName(c.osClient.Octavia, lbName)
	if err != nil {
		if err != openstackutil.ErrNotFound {
			return fmt.Errorf("error getting loadbalancer %s: %v", ing.Name, err)
		}

		log.WithFields(log.Fields{"lbName": lbName, "ingressName": ing.Name, "namespace": ing.Namespace}).Info("loadbalancer for ingress deleted")
		return nil
	}

	// Delete the floating IP for the load balancer VIP. We don't check if the Ingress is internal or not, just delete
	// any floating IPs associated with the load balancer VIP port.
	log.WithFields(log.Fields{"ingress": key}).Debug("deleting floating IP")

	if _, err = c.osClient.EnsureFloatingIP(true, loadbalancer.VipPortID, "", ""); err != nil {
		return fmt.Errorf("failed to delete floating IP: %v", err)
	}

	log.WithFields(log.Fields{"ingress": key}).Info("floating IP deleted")

	// Delete security group managed for the Ingress backend service
	if c.config.Octavia.ManageSecurityGroups {
		sgTags := []string{IngressControllerTag, fmt.Sprintf("%s_%s", ing.Namespace, ing.Name)}
		tagString := strings.Join(sgTags, ",")
		opts := groups.ListOpts{Tags: tagString}
		sgs, err := c.osClient.GetSecurityGroups(opts)
		if err != nil {
			return fmt.Errorf("failed to get security groups for ingress %s: %v", key, err)
		}

		nodes, err := listWithPredicate(c.nodeLister, getNodeConditionPredicate())
		if err != nil {
			return fmt.Errorf("failed to get nodes: %v", err)
		}

		for _, sg := range sgs {
			if err = c.osClient.EnsurePortSecurityGroup(true, sg.ID, nodes); err != nil {
				return fmt.Errorf("failed to operate on the port security groups for ingress %s: %v", key, err)
			}
			if _, err = c.osClient.EnsureSecurityGroup(true, "", "", sgTags); err != nil {
				return fmt.Errorf("failed to delete the security groups for ingress %s: %v", key, err)
			}
		}

		log.WithFields(log.Fields{"ingress": key}).Info("security group deleted")
	}

	err = openstackutil.DeleteLoadbalancer(c.osClient.Octavia, loadbalancer.ID)
	log.WithFields(log.Fields{"lbID": loadbalancer.ID}).Info("loadbalancer deleted")

	return err
}

func (c *Controller) toBarbicanSecret(name string, namespace string, toSecretName string) (string, error) {
	secret, err := c.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), name, apimetav1.GetOptions{})
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
	// TODO(lingxiankong): We assume the 'tls.cert' data only contains the end user certificate.
	pfxData, err := pkcs12.Encode(rand.Reader, pk, cb[0], caCerts, "")
	if err != nil {
		return "", fmt.Errorf("failed to create PKCS#12 bundle: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(pfxData)

	return openstackutil.EnsureSecret(c.osClient.Barbican, toSecretName, "application/octet-stream", encoded)
}

func (c *Controller) ensureIngress(ing *nwv1beta1.Ingress) error {
	ingName := ing.ObjectMeta.Name
	ingNamespace := ing.ObjectMeta.Namespace
	clusterName := c.config.ClusterName

	key := fmt.Sprintf("%s/%s", ingNamespace, ingName)
	name := utils.GetResourceName(ingNamespace, ingName, clusterName)

	if len(ing.Spec.TLS) > 0 && c.osClient.Barbican == nil {
		return fmt.Errorf("TLS Ingress not supported because of Key Manager service unavailable")
	}

	lb, err := c.osClient.EnsureLoadBalancer(name, c.config.Octavia.SubnetID, ingNamespace, ingName, clusterName)
	if err != nil {
		return err
	}

	if strings.Contains(lb.Description, ing.ResourceVersion) {
		log.WithFields(log.Fields{"ingress": key}).Info("ingress not changed")
		return nil
	}

	var nodePorts []int
	var sgID string

	if c.config.Octavia.ManageSecurityGroups {
		log.WithFields(log.Fields{"ingress": key}).Info("ensuring security group")

		sgDescription := fmt.Sprintf("Security group created for Ingress %s from cluster %s", key, clusterName)
		sgTags := []string{IngressControllerTag, fmt.Sprintf("%s_%s", ingNamespace, ingName)}
		sgID, err = c.osClient.EnsureSecurityGroup(false, name, sgDescription, sgTags)
		if err != nil {
			return fmt.Errorf("failed to prepare the security group for the ingress %s: %v", key, err)
		}

		log.WithFields(log.Fields{"sgID": sgID, "ingress": key}).Info("ensured security group")
	}

	// Convert kubernetes secrets to barbican ones
	var secretRefs []string
	for _, tls := range ing.Spec.TLS {
		secretName := fmt.Sprintf(BarbicanSecretNameTemplate, clusterName, ingNamespace, ingName, tls.SecretName)
		secretRef, err := c.toBarbicanSecret(tls.SecretName, ingNamespace, secretName)
		if err != nil {
			return fmt.Errorf("failed to create Barbican secret: %v", err)
		}

		log.WithFields(log.Fields{"secretName": secretName, "secretRef": secretRef, "ingress": key}).Info("secret created in Barbican")

		secretRefs = append(secretRefs, secretRef)
	}
	port := 80
	if len(secretRefs) > 0 {
		port = 443
	}

	// Create listener
	listener, err := c.osClient.EnsureListener(name, lb.ID, secretRefs)
	if err != nil {
		return err
	}

	// get nodes information
	nodeObjs, err := listWithPredicate(c.nodeLister, getNodeConditionPredicate())
	if err != nil {
		return err
	}

	// Add default pool for the listener if 'backend' is defined
	if ing.Spec.Backend != nil {
		serviceName := fmt.Sprintf("%s/%s", ingNamespace, ing.Spec.Backend.ServiceName)
		nodePort, err := c.getServiceNodePort(serviceName, ing.Spec.Backend.ServicePort)
		if err != nil {
			return err
		}

		nodePorts = append(nodePorts, nodePort)

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
		err = c.osClient.DeleteL7policy(p.ID, lb.ID)
		if err != nil {
			log.WithFields(log.Fields{"policyID": p.ID, "lbID": lb.ID}).Errorf("could not delete L7 policy: %v", err)
		}
	}

	// Delete all existing shared pools
	existingSharedPools, err := c.osClient.GetPools(lb.ID, true)
	if err != nil {
		return err
	}
	for _, sp := range existingSharedPools {
		err = c.osClient.DeletePool(sp.ID, lb.ID)
		if err != nil {
			log.WithFields(log.Fields{"poolID": sp.ID, "lbID": lb.ID}).Errorf("could not delete shared pool: %v", err)
		}
	}

	// Add l7 load balancing rules. Each host and path combination is mapped to a l7 policy in octavia,
	// which contains two rules(with type 'HOST_NAME' and 'PATH' respectively)
	for _, rule := range ing.Spec.Rules {
		host := rule.Host

		for _, path := range rule.HTTP.Paths {
			// make the pool name unique in the load balancer
			poolName := utils.Hash(fmt.Sprintf("%s+%s", path.Backend.ServiceName, path.Backend.ServicePort.String()))

			serviceName := fmt.Sprintf("%s/%s", ingNamespace, path.Backend.ServiceName)
			nodePort, err := c.getServiceNodePort(serviceName, path.Backend.ServicePort)
			if err != nil {
				return err
			}

			nodePorts = append(nodePorts, nodePort)

			poolID, err := c.osClient.EnsurePoolMembers(false, poolName, lb.ID, "", &nodePort, nodeObjs)
			if err != nil {
				return err
			}

			if err = c.osClient.CreatePolicyRules(lb.ID, listener.ID, *poolID, host, path.Path, port); err != nil {
				return err
			}
		}
	}

	if c.config.Octavia.ManageSecurityGroups {
		log.WithFields(log.Fields{"ingress": key, "sgID": sgID}).Info("ensuring security group rules")

		if err := c.osClient.EnsureSecurityGroupRules(sgID, c.subnetCIDR, nodePorts); err != nil {
			return fmt.Errorf("failed to ensure security group rules for Ingress %s: %v", ingName, err)
		}

		if err := c.osClient.EnsurePortSecurityGroup(false, sgID, nodeObjs); err != nil {
			return fmt.Errorf("failed to operate port security group for Ingress %s: %v", ingName, err)
		}

		log.WithFields(log.Fields{"ingress": key, "sgID": sgID}).Info("ensured security group rules")
	}

	internalSetting := getStringFromIngressAnnotation(ing, IngressAnnotationInternal, "true")
	isInternal, err := strconv.ParseBool(internalSetting)
	if err != nil {
		return fmt.Errorf("unknown annotation %s: %v", IngressAnnotationInternal, err)
	}

	address := lb.VipAddress
	// Allocate floating ip for loadbalancer vip if the external network is configured and the Ingress is not internal.
	if !isInternal && c.config.Octavia.FloatingIPNetwork != "" {
		log.WithFields(log.Fields{"ingress": key}).Info("creating floating IP")

		description := fmt.Sprintf("Floating IP for Kubernetes ingress %s in namespace %s from cluster %s", ingName, ingNamespace, clusterName)
		address, err = c.osClient.EnsureFloatingIP(false, lb.VipPortID, c.config.Octavia.FloatingIPNetwork, description)
		if err != nil {
			return fmt.Errorf("failed to create floating IP: %v", err)
		}

		log.WithFields(log.Fields{"ingress": key, "fip": address}).Info("floating IP created")
	}

	// Update ingress status
	newIng, err := c.updateIngressStatus(ing, address)
	if err != nil {
		return err
	}
	c.recorder.Event(ing, apiv1.EventTypeNormal, "Updated", fmt.Sprintf("Successfully associated IP address %s to ingress %s", address, key))

	// Add ingress resource version to the load balancer description
	newDes := fmt.Sprintf("Kubernetes Ingress %s in namespace %s from cluster %s, version: %s", ingName, ingNamespace, clusterName, newIng.ResourceVersion)
	if err = c.osClient.UpdateLoadBalancerDescription(lb.ID, newDes); err != nil {
		return err
	}

	log.WithFields(log.Fields{"ingress": key, "lbID": lb.ID}).Info("openstack resources for ingress created")

	return nil
}

func (c *Controller) updateIngressStatus(ing *nwv1beta1.Ingress, vip string) (*nwv1beta1.Ingress, error) {
	newState := new(apiv1.LoadBalancerStatus)
	newState.Ingress = []apiv1.LoadBalancerIngress{{IP: vip}}
	newIng := ing.DeepCopy()
	newIng.Status.LoadBalancer = *newState

	newObj, err := c.kubeClient.NetworkingV1beta1().Ingresses(newIng.Namespace).UpdateStatus(context.TODO(), newIng, apimetav1.UpdateOptions{})
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

// getStringFromIngressAnnotation searches a given Ingress for a specific annotationKey and either returns the
// annotation's value or a specified defaultSetting
func getStringFromIngressAnnotation(ingress *nwv1beta1.Ingress, annotationKey string, defaultValue string) string {
	if annotationValue, ok := ingress.Annotations[annotationKey]; ok {
		return annotationValue
	}

	return defaultValue
}

// privateKeyFromPEM converts a PEM block into a crypto.PrivateKey.
func privateKeyFromPEM(pemData []byte) (crypto.PrivateKey, error) {
	var result *pem.Block
	rest := pemData
	for {
		result, rest = pem.Decode(rest)
		if result == nil {
			return nil, fmt.Errorf("Cannot decode supplied PEM data")
		}

		switch result.Type {
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(result.Bytes)
		case "EC PRIVATE KEY":
			return x509.ParseECPrivateKey(result.Bytes)
		}
	}
}

// parsePEMBundle parses a certificate bundle from top to bottom and returns
// a slice of x509 certificates. This function will error if no certificates are found.
//
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
