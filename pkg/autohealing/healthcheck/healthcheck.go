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

package healthcheck

import (
	apiv1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
)

var (
	checkPlugins = make(map[string]registerPlugin)
)

type registerPlugin func(config interface{}) (HealthCheck, error)

// NodeInfo is a wrapper of Node, may contains more information in future.
type NodeInfo struct {
	KubeNode    apiv1.Node
	IsWorker    bool
	FailedCheck string
}

type HealthCheck interface {
	// Check checks the node health, returns false if the node is unhealthy. The plugin should deal with any error happened.
	Check(node NodeInfo, controller NodeController) bool

	// IsMasterSupported checks if the health check plugin supports master node.
	IsMasterSupported() bool

	// IsWorkerSupported checks if the health check plugin supports worker node.
	IsWorkerSupported() bool

	// GetName returns name of the health check plugin
	GetName() string
}

// NodeController is to avoid circle reference.
type NodeController interface {
	// UpdateNodeAnnotation updates the specified node annotation, if value equals empty string, the annotation will be
	// removed.
	UpdateNodeAnnotation(node NodeInfo, annotation string, value string) error
}

func registerHealthCheck(name string, register registerPlugin) {
	if _, found := checkPlugins[name]; found {
		log.Fatalf("Health check plugin %s is already registered.", name)
	}

	log.Infof("Registered health check plugin %s", name)
	checkPlugins[name] = register
}

func GetHealthChecker(name string, config interface{}) (HealthCheck, error) {
	c, found := checkPlugins[name]
	if !found {
		return nil, nil
	}
	return c(config)
}

// CheckNodes goes through the health checkers, returns the unhealthy nodes.
func CheckNodes(checkers []HealthCheck, nodes []NodeInfo, controller NodeController) []NodeInfo {
	var unhealthyNodes []NodeInfo

	// Check the health for each node.
	for _, node := range nodes {
		for _, checker := range checkers {
			if !checker.Check(node, controller) {
				node.FailedCheck = checker.GetName()
				unhealthyNodes = append(unhealthyNodes, node)
				break
			}
		}
	}

	return unhealthyNodes
}
