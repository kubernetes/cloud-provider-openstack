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
	"fmt"
	"time"

	"github.com/mitchellh/mapstructure"
	log "k8s.io/klog/v2"

	"k8s.io/cloud-provider-openstack/pkg/autohealing/utils"
)

const (
	NodeConditionType = "NodeCondition"
)

type NodeConditionCheck struct {
	// (Optional) The condition types(case sensitive) specified in Node.spec.status.conditions items. Default: ["Ready"].
	Types []string `mapstructure:"types"`

	// (Optional) How long to wait before a unhealthy worker node should be repaired. Default: 300s
	UnhealthyDuration time.Duration `mapstructure:"unhealthy-duration"`

	// (Optional) The accepted healthy values(case sensitive) for the type. Default: ["True"]. OKValues could not be used together with ErrorValues.
	OKValues []string `mapstructure:"ok-values"`

	// (Optional) The unhealthy values(case sensitive) for the type. Default: []. ErrorValues could not be used together with OKValues.
	ErrorValues []string `mapstructure:"error-values"`
}

// Check checks the node health, returns false if the node is unhealthy.
func (check *NodeConditionCheck) Check(node NodeInfo, controller NodeController) bool {
	nodeName := node.KubeNode.Name

	for _, cond := range node.KubeNode.Status.Conditions {
		if utils.Contains(check.Types, string(cond.Type)) {
			unhealthyDuration := time.Now().Sub(cond.LastTransitionTime.Time)

			if len(check.ErrorValues) > 0 {
				if utils.Contains(check.ErrorValues, string(cond.Status)) {
					if unhealthyDuration >= check.UnhealthyDuration {
						return false
					}
					log.Warningf("Node %s is unhealthy, %s: %s", nodeName, string(cond.Type), string(cond.Status))
				}
			} else if len(check.OKValues) > 0 {
				if !utils.Contains(check.OKValues, string(cond.Status)) {
					if unhealthyDuration >= check.UnhealthyDuration {
						return false
					}
					log.Warningf("Node %s is unhealthy, %s: %s", nodeName, string(cond.Type), string(cond.Status))
				}
			}
		}
	}

	return true
}

// GetName returns name of the health check
func (check *NodeConditionCheck) GetName() string {
	return "NodeConditionCheck"
}

// IsMasterSupported checks if the health check plugin supports master node.
func (check *NodeConditionCheck) IsMasterSupported() bool {
	return true
}

// IsWorkerSupported checks if the health check plugin supports worker node.
func (check *NodeConditionCheck) IsWorkerSupported() bool {
	return true
}

func newNodeConditionCheck(config interface{}) (HealthCheck, error) {
	check := NodeConditionCheck{
		UnhealthyDuration: 300 * time.Second,
		Types:             []string{"Ready"},
		OKValues:          []string{"True"},
	}

	decConfig := mapstructure.DecoderConfig{
		DecodeHook: mapstructure.StringToTimeDurationHookFunc(),
		Result:     &check,
	}
	decoder, err := mapstructure.NewDecoder(&decConfig)
	if err != nil {
		return nil, err
	}
	err = decoder.Decode(config)
	if err != nil {
		return nil, fmt.Errorf("failed to get configuration for health check plugin %s, error: %v", NodeConditionType, err)
	}

	return &check, nil
}

func init() {
	registerHealthCheck(NodeConditionType, newNodeConditionCheck)
}
