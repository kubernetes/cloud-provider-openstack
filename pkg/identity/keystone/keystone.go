/*
Copyright 2017 The Kubernetes Authors.

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

package keystone

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/utils"
	"github.com/gorilla/mux"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	netutil "k8s.io/apimachinery/pkg/util/net"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8suser "k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/klog/v2"
)

const (
	maxRetries  = 5
	cmNamespace = "kube-system"
	Roles       = "alpha.kubernetes.io/identity/roles"
	ProjectID   = "alpha.kubernetes.io/identity/project/id"
	ProjectName = "alpha.kubernetes.io/identity/project/name"
	DomainID    = "alpha.kubernetes.io/identity/user/domain/id"
	DomainName  = "alpha.kubernetes.io/identity/user/domain/name"
)

var userAgentData []string

// AddExtraFlags is called by the main package to add component specific command line flags
func AddExtraFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&userAgentData, "user-agent", nil, "Extra data to add to gophercloud user-agent. Use multiple times to add more than one component.")
}

type userInfo struct {
	Username string              `json:"username"`
	UID      string              `json:"uid"`
	Groups   []string            `json:"groups"`
	Extra    map[string][]string `json:"extra"`
}

type status struct {
	Authenticated bool     `json:"authenticated"`
	User          userInfo `json:"user"`
}

// KeystoneAuth manages authentication and authorization
type KeystoneAuth struct {
	authn          *Authenticator
	authz          *Authorizer
	k8sClient      *kubernetes.Clientset
	syncer         *Syncer
	config         *Config
	stopCh         chan struct{}
	queue          workqueue.RateLimitingInterface
	informer       informers.SharedInformerFactory
	cmLister       corelisters.ConfigMapLister
	cmListerSynced cache.InformerSynced
}

// Run starts the keystone webhook server.
func (k *KeystoneAuth) Run() {
	defer close(k.stopCh)

	if k.k8sClient != nil {
		defer k.queue.ShutDown()
		go k.informer.Start(k.stopCh)

		// wait for the caches to synchronize before starting the worker
		if !cache.WaitForCacheSync(k.stopCh, k.cmListerSynced) {
			runtimeutil.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
			return
		}
		klog.Info("ConfigMaps synced and ready")

		go wait.Until(k.runWorker, time.Second, k.stopCh)
	}

	r := mux.NewRouter()
	r.HandleFunc("/webhook", k.Handler)

	klog.Infof("Starting webhook server...")
	klog.Fatal(http.ListenAndServeTLS(k.config.Address, k.config.CertFile, k.config.KeyFile, r))
}

func (k *KeystoneAuth) enqueueConfigMap(obj interface{}) {
	// obj could be an *v1.ConfigMap, or a DeletionFinalStateUnknown marker item.
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to get key for object: %v", err)
		return
	}
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("Failed to get namespace and name for the key %s: %v", key, err)
		return
	}

	if namespace == cmNamespace && (name == k.config.PolicyConfigMapName || name == k.config.SyncConfigMapName) {
		k.queue.Add(key)
	}
}

func (k *KeystoneAuth) runWorker() {
	for k.processNextItem() {
		// continue looping
	}
}

func (k *KeystoneAuth) processNextItem() bool {
	key, quit := k.queue.Get()

	if quit {
		return false
	}
	defer k.queue.Done(key)

	err := k.processItem(key.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		k.queue.Forget(key)
	} else if k.queue.NumRequeues(key) < maxRetries {
		klog.Errorf("Failed to process key %s (will retry): %v", key, err)
		k.queue.AddRateLimited(key)
	} else {
		// err != nil and too many retries
		klog.Errorf("Failed to process key %s (giving up): %v", key, err)
		k.queue.Forget(key)
		runtimeutil.HandleError(err)
	}

	return true
}

func (k *KeystoneAuth) updatePolicies(cm *apiv1.ConfigMap, key string) {
	klog.Info("ConfigMap created or updated, will update the authorization policy.")

	var policy policyList
	if err := json.Unmarshal([]byte(cm.Data["policies"]), &policy); err != nil {
		runtimeutil.HandleError(fmt.Errorf("failed to parse policies defined in the configmap %s: %v", key, err))
	}
	if len(policy) > 0 {
		if _, err := json.MarshalIndent(policy, "", "  "); err != nil {
			runtimeutil.HandleError(fmt.Errorf("failed to parse policies defined in the configmap %s: %v", key, err))
		}
	}

	k.authz.mu.Lock()
	k.authz.pl = policy
	k.authz.mu.Unlock()

	klog.Infof("Authorization policy updated.")
}

func (k *KeystoneAuth) updateSyncConfig(cm *apiv1.ConfigMap, key string) {
	klog.Info("ConfigMap created or updated, will update the sync configuration.")

	var sc *syncConfig
	newConfig := newSyncConfig()
	sc = &newConfig
	if err := yaml.Unmarshal([]byte(cm.Data["syncConfig"]), sc); err != nil {
		runtimeutil.HandleError(fmt.Errorf("failed to parse sync config defined in the configmap %s: %v", key, err))
	}

	k.syncer.mu.Lock()
	k.syncer.syncConfig = sc
	k.syncer.mu.Unlock()

	klog.Infof("Sync configuration updated.")
}

func (k *KeystoneAuth) processItem(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	cm, err := k.cmLister.ConfigMaps(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		if name == k.config.PolicyConfigMapName {
			klog.Infof("PolicyConfigmap %v has been deleted.", k.config.PolicyConfigMapName)
			k.authz.mu.Lock()
			k.authz.pl = make([]*policy, 0)
			k.authz.mu.Unlock()
		}
		if name == k.config.SyncConfigMapName {
			klog.Infof("SyncConfigmap %v has been deleted.", k.config.SyncConfigMapName)
			k.syncer.mu.Lock()
			sc := newSyncConfig()
			k.syncer.syncConfig = &sc
			k.syncer.mu.Unlock()
		}
	case err != nil:
		return fmt.Errorf("error fetching object with key %s: %v", key, err)
	default:
		if name == k.config.PolicyConfigMapName {
			k.updatePolicies(cm, key)
		}
		if name == k.config.SyncConfigMapName {
			k.updateSyncConfig(cm, key)
		}
	}

	return nil
}

// Handler serves the http requests
func (k *KeystoneAuth) Handler(w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	err := decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var apiVersion = data["apiVersion"].(string)
	var kind = data["kind"].(string)

	if apiVersion != "authentication.k8s.io/v1beta1" && apiVersion != "authorization.k8s.io/v1beta1" {
		http.Error(w, fmt.Sprintf("unknown apiVersion %q", apiVersion), http.StatusBadRequest)
		return
	}

	if kind == "TokenReview" {
		var token = data["spec"].(map[string]interface{})["token"].(string)
		userInfo := k.authenticateToken(w, r, token, data)

		// Do synchronization
		// In the case of unscoped tokens, when project id is not defined, we have to skip this part
		if k.syncer.syncConfig != nil && len(k.syncer.syncConfig.DataTypesToSync) > 0 && userInfo != nil && len(userInfo.Extra[ProjectID]) != 0 {
			err = k.syncer.syncData(userInfo)
			if err != nil {
				klog.Errorf("an error occurred during data synchronization: %v", err)
			}
		}
	} else if kind == "SubjectAccessReview" {
		k.authorizeToken(w, r, data)
	} else {
		http.Error(w, fmt.Sprintf("unknown kind/apiVersion %q %q", kind, apiVersion), http.StatusBadRequest)
	}
}

func (k *KeystoneAuth) authenticateToken(w http.ResponseWriter, r *http.Request, token string, data map[string]interface{}) *userInfo {
	user, authenticated, err := k.authn.AuthenticateToken(token)
	klog.V(4).Infof("authenticateToken : %v, %v, %v\n", token, user, err)

	if !authenticated {
		var response status
		response.Authenticated = false
		data["status"] = response

		output, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(output)
		return nil
	}

	var info userInfo
	info.Username = user.GetName()
	info.UID = user.GetUID()
	info.Groups = user.GetGroups()
	info.Extra = user.GetExtra()

	var response status
	response.Authenticated = true

	// Modify user info accoding to the sync configuration.
	response.User = *k.syncer.syncRoles(&info)

	data["status"] = response

	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(output)

	return &info
}

func (k *KeystoneAuth) authorizeToken(w http.ResponseWriter, r *http.Request, data map[string]interface{}) {
	output, err := json.MarshalIndent(data, "", "  ")
	klog.V(4).Infof("authorizeToken data : %s\n", string(output))

	spec := data["spec"].(map[string]interface{})

	username := spec["user"]
	usr := &k8suser.DefaultInfo{Name: username.(string)}
	attrs := authorizer.AttributesRecord{User: usr}

	groups := spec["group"].([]interface{})
	for _, v := range groups {
		usr.Groups = append(usr.Groups, v.(string))
	}
	if extras, ok := spec["extra"].(map[string]interface{}); ok {
		usr.Extra = make(map[string][]string, len(extras))
		for key, value := range extras {
			for _, v := range value.([]interface{}) {
				if data, ok := usr.Extra[key]; ok {
					usr.Extra[key] = append(data, v.(string))
				} else {
					usr.Extra[key] = []string{v.(string)}
				}
			}
		}
	}

	if resourceAttributes, ok := spec["resourceAttributes"]; ok {
		v := resourceAttributes.(map[string]interface{})
		attrs.ResourceRequest = true
		attrs.Verb = getField(v, "verb")
		attrs.Namespace = getField(v, "namespace")
		attrs.APIGroup = getField(v, "group")
		attrs.APIVersion = getField(v, "version")
		attrs.Resource = getField(v, "resource")
		attrs.Subresource = getField(v, "subresource")
		attrs.Name = getField(v, "name")
	} else if nonResourceAttributes, ok := spec["nonResourceAttributes"]; ok {
		v := nonResourceAttributes.(map[string]interface{})
		attrs.ResourceRequest = false
		attrs.Verb = getField(v, "verb")
		attrs.Path = getField(v, "path")
	} else {
		err := fmt.Errorf("unable to find attributes")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var allowed authorizer.Decision
	if len(k.authz.pl) > 0 {
		var reason string
		allowed, reason, err = k.authz.Authorize(attrs)
		klog.V(4).Infof("<<<< authorizeToken: %v, %v, %v\n", allowed, reason, err)
		if err != nil {
			http.Error(w, reason, http.StatusInternalServerError)
			return
		}
	} else {
		// The operator didn't set authorization policy, deny by default.
		allowed = authorizer.DecisionDeny
	}

	delete(data, "spec")
	data["status"] = map[string]interface{}{
		"allowed": allowed == authorizer.DecisionAllow,
	}
	output, err = json.MarshalIndent(data, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}

// NewKeystoneAuth returns a new KeystoneAuth controller
func NewKeystoneAuth(c *Config) (*KeystoneAuth, error) {
	keystoneClient, err := createKeystoneClient(c.KeystoneURL, c.KeystoneCA)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize keystone client: %v", err)
	}

	var k8sClient *kubernetes.Clientset
	if c.PolicyConfigMapName != "" || c.SyncConfigMapName != "" || c.SyncConfigFile != "" {
		k8sClient, err = createKubernetesClient(c.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to get kubernetes client: %v", err)
		}
	}

	// Get policy definition either from a policy file or the policy configmap. Policy file takes precedence
	// over the configmap, but the policy definition will be refreshed based on the configmap change on-the-fly. It
	// is possible that both are not provided, in this case, the keytone webhook authorization will always return deny.
	var policy policyList
	if c.PolicyConfigMapName != "" {
		cm, err := k8sClient.CoreV1().ConfigMaps(cmNamespace).Get(context.TODO(), c.PolicyConfigMapName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get configmap %s: %v", c.PolicyConfigMapName, err)
		}

		if err := json.Unmarshal([]byte(cm.Data["policies"]), &policy); err != nil {
			return nil, fmt.Errorf("failed to parse policies defined in the configmap %s: %v", c.PolicyConfigMapName, err)
		}
	}
	if c.PolicyFile != "" {
		policy, err = newFromFile(c.PolicyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to extract policy from policy file %s: %v", c.PolicyFile, err)
		}
	}

	if len(policy) > 0 {
		output, err := json.MarshalIndent(policy, "", "  ")
		if err == nil {
			klog.V(4).Infof("Policy %s", string(output))
		} else {
			return nil, err
		}
	}

	// Get sync config either from a sync config file or the sync configmap. Sync config file takes precedence
	// over the configmap, but the sync config definition will be refreshed based on the configmap change on-the-fly. It
	// is possible that both are not provided, in this case, the keytone webhook authenticator will not synchronize data.
	var sc *syncConfig
	if c.SyncConfigMapName != "" {
		cm, err := k8sClient.CoreV1().ConfigMaps(cmNamespace).Get(context.TODO(), c.SyncConfigMapName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("configmap get err   #%v ", err)
			return nil, fmt.Errorf("failed to get configmap %s: %v", c.SyncConfigMapName, err)
		}

		newConfig := newSyncConfig()
		sc = &newConfig
		if err := yaml.Unmarshal([]byte(cm.Data["syncConfig"]), sc); err != nil {
			klog.Errorf("Unmarshal: %v", err)
			return nil, fmt.Errorf("failed to parse sync config defined in the configmap %s: %v", c.SyncConfigMapName, err)
		}
	}
	if c.SyncConfigFile != "" {
		sc, err = newSyncConfigFromFile(c.SyncConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to extract data from sync config file %s: %v", c.SyncConfigFile, err)
		}
	}
	if sc != nil {
		// Validate that config data is correct
		sc.validate()
	}

	keystoneAuth := &KeystoneAuth{
		authn:     &Authenticator{keystoner: NewKeystoner(keystoneClient)},
		authz:     &Authorizer{authURL: c.KeystoneURL, client: keystoneClient, pl: policy},
		syncer:    &Syncer{k8sClient: k8sClient, syncConfig: sc},
		k8sClient: k8sClient,
		config:    c,
		stopCh:    make(chan struct{}),
	}

	if k8sClient != nil {
		queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		kubeInformerFactory := informers.NewSharedInformerFactory(k8sClient, time.Minute*5)
		cmInformer := kubeInformerFactory.Core().V1().ConfigMaps()
		cmInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: keystoneAuth.enqueueConfigMap,
			UpdateFunc: func(old, new interface{}) {
				newIng := new.(*apiv1.ConfigMap)
				oldIng := old.(*apiv1.ConfigMap)
				if newIng.ResourceVersion == oldIng.ResourceVersion {
					// Periodic resync will send update events for all known ConfigMaps.
					// Two different versions of the same ConfigMap will always have different RVs.
					return
				}
				keystoneAuth.enqueueConfigMap(new)
			},
			DeleteFunc: keystoneAuth.enqueueConfigMap,
		})

		keystoneAuth.informer = kubeInformerFactory
		keystoneAuth.cmLister = cmInformer.Lister()
		keystoneAuth.cmListerSynced = cmInformer.Informer().HasSynced
		keystoneAuth.queue = queue
	}

	return keystoneAuth, nil
}

func getField(data map[string]interface{}, name string) string {
	if v, ok := data[name]; ok {
		return v.(string)
	}
	return ""
}

// Construct a Keystone v3 client, bail out if we cannot find the v3 API endpoint
func createIdentityV3Provider(options gophercloud.AuthOptions, transport http.RoundTripper) (*gophercloud.ProviderClient, error) {
	client, err := openstack.NewClient(options.IdentityEndpoint)
	if err != nil {
		return nil, err
	}

	if transport != nil {
		client.HTTPClient.Transport = transport
	}

	versions := []*utils.Version{
		{ID: "v3", Priority: 30, Suffix: "/v3/"},
	}
	chosen, _, err := utils.ChooseVersion(client, versions)
	if err != nil {
		return nil, fmt.Errorf("Unable to find identity API v3 version : %v", err)
	}

	switch chosen.ID {
	case "v3":
		return client, nil
	default:
		// The switch statement must be out of date from the versions list.
		return nil, fmt.Errorf("Unsupported identity API version: %s", chosen.ID)
	}
}

func createKubernetesClient(kubeConfig string) (*kubernetes.Clientset, error) {
	klog.Info("Creating kubernetes API client.")

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	v, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}

	klog.Infof("Kubernetes API client created, server version %s", fmt.Sprintf("v%v.%v", v.Major, v.Minor))
	return client, nil
}

func createKeystoneClient(authURL string, caFile string) (*gophercloud.ServiceClient, error) {
	// FIXME: Enable this check later
	//if !strings.HasPrefix(authURL, "https") {
	//	return nil, errors.New("Auth URL should be secure and start with https")
	//}
	var transport http.RoundTripper
	if authURL == "" {
		return nil, fmt.Errorf("auth URL is empty")
	}
	if caFile != "" {
		roots, err := certutil.NewPool(caFile)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{}
		config.RootCAs = roots
		transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: config})
	}
	opts := gophercloud.AuthOptions{IdentityEndpoint: authURL}
	provider, err := createIdentityV3Provider(opts, transport)
	if err != nil {
		return nil, err
	}

	userAgent := gophercloud.UserAgent{}
	userAgent.Prepend(fmt.Sprintf("k8s-keystone-auth/%s", version.Version))
	for _, data := range userAgentData {
		userAgent.Prepend(data)
	}
	provider.UserAgent = userAgent
	klog.V(4).Infof("Using user-agent %s", userAgent.Join())

	// We should use the V3 API
	client, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		klog.Warningf("Failed: Unable to use keystone v3 identity service: %v", err)
		return nil, fmt.Errorf("failed to authenticate")
	}

	// Make sure we look under /v3 for resources
	client.IdentityBase = client.IdentityEndpoint
	client.Endpoint = client.IdentityEndpoint
	return client, nil
}
