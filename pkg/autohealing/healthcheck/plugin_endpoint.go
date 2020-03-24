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
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	log "k8s.io/klog/v2"

	"k8s.io/cloud-provider-openstack/pkg/autohealing/utils"
)

const (
	EndpointType = "Endpoint"
	TokenPath    = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	TimeLayout   = "2006-01-02 15:04:05"
)

type EndpointCheck struct {
	// (Optional) URL scheme, case insensitive, e.g. HTTP, HTTPS. Default: ["HTTPS"].
	Protocol string `mapstructure:"protocol"`

	// (Optional) Service port. Default: 6443
	Port int `mapstructure:"port"`

	// (Optional) The endpoints for health check. Default: ["/healthz"].
	Endpoints []string `mapstructure:"endpoints"`

	// (Optional) How long to wait before a unhealthy worker node should be repaired. Default: 300s
	UnhealthyDuration time.Duration `mapstructure:"unhealthy-duration"`

	// (Optional) The node annotation which records the node unhealthy time. Default: autohealing.openstack.org/unhealthy-timestamp
	UnhealthyAnnotation string `mapstructure:"unhealthy-annotation"`

	// (Optional) The accepted HTTP response codes. Default: [200].
	OKCodes []int `mapstructure:"ok-codes"`

	// (Optional) If token is required to access the endpoint. Default: false
	RequireToken bool `mapstructure:"require-token"`

	// (Optional) Token to use in the request header. Default: read from TokenPath file
	Token string `mapstructure:"token"`
}

// GetName returns name of the health check
func (check *EndpointCheck) GetName() string {
	return "EndpointCheck"
}

// IsMasterSupported checks if the health check plugin supports master node.
func (check *EndpointCheck) IsMasterSupported() bool {
	return true
}

// IsWorkerSupported checks if the health check plugin supports worker node.
func (check *EndpointCheck) IsWorkerSupported() bool {
	return false
}

// checkDuration checks if the node should be marked as healthy or not.
func (check *EndpointCheck) checkDuration(node NodeInfo, controller NodeController, checkRet bool) bool {
	name := node.KubeNode.Name

	if checkRet {
		// Remove the annotation
		if err := controller.UpdateNodeAnnotation(node, check.UnhealthyAnnotation, ""); err != nil {
			log.Errorf("Failed to remove the node annotation(will skip the check) for %s, error: %v", name, err)
		}
		return true
	}

	now := time.Now()
	var unhealthyStartTime *time.Time

	// Get the current annotation value
	if timeStr, isPresent := node.KubeNode.Annotations[check.UnhealthyAnnotation]; isPresent {
		if timeStr != "" {
			startTime, err := time.Parse(TimeLayout, timeStr)
			if err != nil {
				unhealthyStartTime = nil
			} else {
				unhealthyStartTime = &startTime
			}
		}
	}

	if unhealthyStartTime == nil {
		// Set the annotation value
		if err := controller.UpdateNodeAnnotation(node, check.UnhealthyAnnotation, now.Format(TimeLayout)); err != nil {
			log.Errorf("Failed to set the node annotation(will skip the check) for %s, error: %v", name, err)
		}
		return true
	}

	if now.Sub(*unhealthyStartTime) >= check.UnhealthyDuration {
		// Need repair
		return false
	}
	// Keep the annotation value
	return true
}

// Check checks the node health, returns false if the node is unhealthy. Update the node cache accordingly.
func (check *EndpointCheck) Check(node NodeInfo, controller NodeController) bool {
	nodeName := node.KubeNode.Name
	ip := ""
	for _, addr := range node.KubeNode.Status.Addresses {
		if addr.Type == "InternalIP" {
			ip = addr.Address
			break
		}
	}

	if ip == "" {
		log.Warningf("Cannot find IP address for node %s, skip the check", nodeName)
		return true
	}

	var client *http.Client
	protocol := strings.ToLower(check.Protocol)
	if protocol == "https" {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: tr, Timeout: time.Second * 5}
	} else if protocol == "http" {
		client = &http.Client{Timeout: time.Second * 5}
	}

	for _, endpoint := range check.Endpoints {
		url := fmt.Sprintf("%s://%s:%d/%s", strings.ToLower(check.Protocol), ip, check.Port, strings.TrimLeft(endpoint, "/"))
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Errorf("Node %s, failed to get request %s, error: %v", nodeName, url, err)
			return check.checkDuration(node, controller, false)
		}

		if check.RequireToken {
			if check.Token == "" {
				b, err := ioutil.ReadFile(TokenPath)
				if err != nil {
					log.Warningf("Node %s, failed to get token from %s, skip the check", nodeName, TokenPath)
					return true
				}
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", string(b)))
			} else {
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", check.Token))
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("Node %s, failed to read response for url %s, error: %v", nodeName, url, err)
			return check.checkDuration(node, controller, false)
		}
		resp.Body.Close()

		if !utils.ContainsInt(check.OKCodes, resp.StatusCode) {
			log.V(4).Infof("Node %s, return code for url %s is %d, expected: %d", nodeName, url, resp.StatusCode, check.OKCodes)
			return check.checkDuration(node, controller, false)
		}
	}

	return check.checkDuration(node, controller, true)
}

func newEndpointCheck(config interface{}) (HealthCheck, error) {
	check := EndpointCheck{
		Protocol:            "https",
		Port:                6443,
		UnhealthyDuration:   300 * time.Second,
		Endpoints:           []string{"/healthz"},
		OKCodes:             []int{200},
		RequireToken:        false,
		UnhealthyAnnotation: "autohealing.openstack.org/unhealthy-timestamp",
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
	registerHealthCheck(EndpointType, newEndpointCheck)
}
