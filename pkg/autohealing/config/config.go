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

package config

import (
	"time"

	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
)

// Config struct contains ingress controller configuration
type Config struct {
	// (Optional) Emit an event without repairing nodes. Default: false
	DryRun bool `mapstructure:"dry-run"`

	// (Required) Cluster identifier
	ClusterName string `mapstructure:"cluster-name"`

	// (Optional) Cloud provider name. Default: openstack
	CloudProvider string `mapstructure:"cloud-provider"`

	// (Optional) Interval of the nodes monitoring check. Default: 30s
	MonitorInterval time.Duration `mapstructure:"monitor-interval"`

	// (Optional) If master nodes monitoring is enabled. Default: true
	MasterMonitorEnabled bool `mapstructure:"master-monitor-enabled"`

	// (Optional) If worker nodes monitoring is enabled. Default: true
	WorkerMonitorEnabled bool `mapstructure:"worker-monitor-enabled"`

	// (Optional) Kubernetes related configuration.
	Kubernetes kubeConfig `mapstructure:"kubernetes"`

	// (Required) OpenStack related configuration.
	OpenStack openstack_provider.AuthOpts `mapstructure:"openstack"`

	// (Optional) Healthcheck configuration for master and worker.
	HealthCheck healthCheck `mapstructure:"healthcheck"`

	// (Optional) Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated components for high availability. Default: true
	LeaderElect bool `mapstructure:"leader-elect"`

	// (Optional) How long after new node added that the node will be checked for health status. Default: 10m
	CheckDelayAfterAdd time.Duration `mapstructure:"check-delay-after-add"`
}

type healthCheck struct {
	Master []Check `mapstructure:"master"`
	Worker []Check `mapstructure:"worker"`
}

type Check struct {
	// (Required) Health check plugin type.
	Type string `mapstructure:"type"`

	// (Required) Customized health check parameters defined by individual health check plugin.
	Params map[string]interface{} `mapstructure:"params"`
}

// Configuration for connecting to Kubernetes API server, either api_host or kubeconfig should be configured.
type kubeConfig struct {
	// (Optional) Kubernetes API server host address.
	ApiserverHost string `mapstructure:"api-host"`

	// (Optional) Kubeconfig file used to connect to Kubernetes cluster.
	KubeConfig string `mapstructure:"kubeconfig"`
}

// NewConfig defines the default values for Config
func NewConfig() Config {
	return Config{
		DryRun:               false,
		CloudProvider:        "openstack",
		MonitorInterval:      30 * time.Second,
		MasterMonitorEnabled: true,
		WorkerMonitorEnabled: true,
		LeaderElect:          true,
		CheckDelayAfterAdd:   10 * time.Minute,
	}
}
