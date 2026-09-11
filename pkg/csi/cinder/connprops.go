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

package cinder

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	// ConnectorPropertiesAnnotation is the node annotation key where the
	// node plugin stores its os-brick connector properties (JSON). The
	// controller reads this annotation to pass connector properties to
	// Cinder's Attachment API (AttachmentCreate).
	ConnectorPropertiesAnnotation = "cinder.csi.openstack.org/connector-properties"

	// csiNodeByDriverNodeID is the name of the custom cache index
	// that maps "<driverName>/<nodeID>" to CSINode objects.
	csiNodeByDriverNodeID = "byDriverNodeID"
)

// ConnectorPropertiesGetter retrieves host connector properties
// (iSCSI initiator, FC WWPNs, etc.) for a given CSI node ID.
type ConnectorPropertiesGetter interface {
	GetConnectorProperties(ctx context.Context, nodeID string) (map[string]any, error)
}

// csiNodeDriverNodeIDIndexFunc returns index keys of the form
// "<driverName>/<nodeID>" for every driver registered in a CSINode.
func csiNodeDriverNodeIDIndexFunc(obj interface{}) ([]string, error) {
	csiNode, ok := obj.(*storagev1.CSINode)
	if !ok {
		return nil, fmt.Errorf("expected *storagev1.CSINode, got %T", obj)
	}
	keys := make([]string, 0, len(csiNode.Spec.Drivers))
	for _, d := range csiNode.Spec.Drivers {
		keys = append(keys, d.Name+"/"+d.NodeID)
	}
	return keys, nil
}

// kubeConnectorPropertiesGetter reads connector properties from Kubernetes
// Node annotations. It uses a custom CSINode cache index to map the CSI
// NodeId to a Kubernetes Node name, then reads the annotation from that Node.
type kubeConnectorPropertiesGetter struct {
	csiNodeIndexer cache.Indexer
	nodeLister     corev1listers.NodeLister
}

// NewKubeConnectorPropertiesGetter creates a ConnectorPropertiesGetter
// backed by Kubernetes Node annotations. It starts informers for Node
// and CSINode objects and blocks until the caches are synced.
//
// stopCh controls the lifecycle of the underlying informers. The caller
// should close it when the driver is shutting down so the informer
// goroutines exit cleanly.
func NewKubeConnectorPropertiesGetter(kubeClient kubernetes.Interface, stopCh <-chan struct{}) (ConnectorPropertiesGetter, error) {
	factory := informers.NewSharedInformerFactory(kubeClient, 0)

	// Use a timeout context for the initial cache sync so we don't
	// block startup indefinitely if the API server is unreachable.
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer syncCancel()

	nodeInformer := factory.Core().V1().Nodes().Informer()
	csiNodeInformer := factory.Storage().V1().CSINodes().Informer()

	// Add a custom index so GetConnectorProperties can look up the
	// CSINode by (driverName, nodeID) instead of listing all.
	if err := csiNodeInformer.AddIndexers(cache.Indexers{
		csiNodeByDriverNodeID: csiNodeDriverNodeIDIndexFunc,
	}); err != nil {
		return nil, fmt.Errorf("failed to add CSINode indexer: %w", err)
	}

	go nodeInformer.Run(stopCh)
	go csiNodeInformer.Run(stopCh)

	if !cache.WaitForCacheSync(syncCtx.Done(), nodeInformer.HasSynced, csiNodeInformer.HasSynced) {
		return nil, fmt.Errorf("timed out after 60s syncing Node/CSINode informer caches for connector properties")
	}

	klog.Info("Successfully created connector properties getter with Node and CSINode listers")

	return &kubeConnectorPropertiesGetter{
		csiNodeIndexer: csiNodeInformer.GetIndexer(),
		nodeLister:     factory.Core().V1().Nodes().Lister(),
	}, nil
}

func (g *kubeConnectorPropertiesGetter) GetConnectorProperties(ctx context.Context, nodeID string) (map[string]any, error) {
	// Look up the Kubernetes Node name via the custom cache index.
	indexKey := driverName + "/" + nodeID
	items, err := g.csiNodeIndexer.ByIndex(csiNodeByDriverNodeID, indexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to look up CSINode by index %q: %w", indexKey, err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no CSINode found with driver %s and nodeID %s", driverName, nodeID)
	}

	csiNode := items[0].(*storagev1.CSINode)
	k8sNodeName := csiNode.Name

	// Read the connector properties annotation from the Kubernetes Node.
	node, err := g.nodeLister.Get(k8sNodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", k8sNodeName, err)
	}

	propsJSON, ok := node.Annotations[ConnectorPropertiesAnnotation]
	if !ok {
		return nil, fmt.Errorf("connector properties annotation %s not found on node %s", ConnectorPropertiesAnnotation, k8sNodeName)
	}

	var props map[string]any
	if err := json.Unmarshal([]byte(propsJSON), &props); err != nil {
		return nil, fmt.Errorf("failed to parse connector properties from node %s: %w", k8sNodeName, err)
	}

	klog.V(4).Infof("Retrieved connector properties for nodeID %s (node %s)", nodeID, k8sNodeName)
	return props, nil
}
