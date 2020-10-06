/*
Copyright 2018 The Kubernetes Authors All rights reserved.

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
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

// Config configures a keystone webhook server
type Config struct {
	Address             string
	CertFile            string
	KeyFile             string
	KeystoneURL         string
	KeystoneCA          string
	PolicyFile          string
	PolicyConfigMapName string
	SyncConfigFile      string
	SyncConfigMapName   string
	Kubeconfig          string
}

// NewConfig returns a Config
func NewConfig() *Config {
	return &Config{
		Address:             "0.0.0.0:8443",
		CertFile:            os.Getenv("TLS_CERT_FILE"),
		KeyFile:             os.Getenv("TLS_PRIVATE_KEY_FILE"),
		KeystoneURL:         os.Getenv("OS_AUTH_URL"),
		KeystoneCA:          os.Getenv("KEYSTONE_CA_FILE"),
		PolicyFile:          os.Getenv("KEYSTONE_POLICY_FILE"),
		PolicyConfigMapName: os.Getenv("KEYSTONE_POLICY_CONFIGMAP_NAME"),
		SyncConfigFile:      os.Getenv("KEYSTONE_SYNC_CONFIG_FILE"),
		SyncConfigMapName:   os.Getenv("KEYSTONE_SYNC_CONFIGMAP_NAME"),
		Kubeconfig:          os.Getenv("KEYSTONE_KUBECONFIG_FILE"),
	}
}

// ValidateFlags validates whether flags are set up correctly
func (c *Config) ValidateFlags() error {
	var errorsFound bool

	if c.KeystoneURL == "" {
		errorsFound = true
		klog.Errorf("please specify --keystone-url or set the OS_AUTH_URL environment variable.")
	}
	if c.CertFile == "" || c.KeyFile == "" {
		errorsFound = true
		klog.Errorf("Please specify --tls-cert-file and --tls-private-key-file arguments.")
	}
	if c.PolicyFile == "" && c.PolicyConfigMapName == "" {
		klog.Warning("Argument --keystone-policy-file or --policy-configmap-name missing. Only keystone authentication will work. Use RBAC for authorization.")
	}
	if c.SyncConfigFile == "" && c.SyncConfigMapName == "" {
		klog.Warning("Argument --sync-config-file or --sync-configmap-name missing. Data synchronization between Keystone and Kubernetes is disabled.")
	}

	if errorsFound {
		return fmt.Errorf("failed to validate the input parameters")
	}
	return nil
}

// AddFlags adds flags for a specific AutoScaler to the specified FlagSet
func (c *Config) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.Address, "listen", c.Address, "<address>:<port> to listen on")
	fs.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, "File containing the default x509 Certificate for HTTPS.")
	fs.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	fs.StringVar(&c.KeystoneURL, "keystone-url", c.KeystoneURL, "URL for the OpenStack Keystone API")
	fs.StringVar(&c.KeystoneCA, "keystone-ca-file", c.KeystoneCA, "File containing the certificate authority for Keystone Service.")
	fs.StringVar(&c.PolicyFile, "keystone-policy-file", c.PolicyFile, "File containing the policy, if provided, it takes precedence over the policy configmap.")
	fs.StringVar(&c.PolicyConfigMapName, "policy-configmap-name", c.PolicyConfigMapName, "ConfigMap in kube-system namespace containing the policy configuration, the ConfigMap data must contain the key 'policies'")
	fs.StringVar(&c.SyncConfigFile, "sync-config-file", c.SyncConfigFile, "File containing config values for data synchronization beetween Keystone and Kubernetes.")
	fs.StringVar(&c.SyncConfigMapName, "sync-configmap-name", "", "ConfigMap in kube-system namespace containing config values for data synchronization beetween Keystone and Kubernetes.")
	fs.StringVar(&c.Kubeconfig, "kubeconfig", c.Kubeconfig, "Kubeconfig file used to connect to Kubernetes API to get policy configmap. If the service is running inside the pod, this option is not necessary, will use in-cluster config instead.")
}
