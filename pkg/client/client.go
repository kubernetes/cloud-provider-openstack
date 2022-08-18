/*
Copyright 2014 The Kubernetes Authors.

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

package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/gophercloud/utils/client"
	"github.com/gophercloud/utils/openstack/clientconfig"

	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/util/cert"
	"k8s.io/cloud-provider-openstack/pkg/version"
	"k8s.io/klog/v2"
)

type AuthOpts struct {
	AuthURL          string                   `gcfg:"auth-url" mapstructure:"auth-url" name:"os-authURL" dependsOn:"os-password|os-trustID|os-applicationCredentialSecret|os-clientCertPath"`
	UserID           string                   `gcfg:"user-id" mapstructure:"user-id" name:"os-userID" value:"optional" dependsOn:"os-password"`
	Username         string                   `name:"os-userName" value:"optional" dependsOn:"os-password"`
	Password         string                   `name:"os-password" value:"optional" dependsOn:"os-domainID|os-domainName,os-projectID|os-projectName,os-userID|os-userName"`
	TenantID         string                   `gcfg:"tenant-id" mapstructure:"project-id" name:"os-projectID" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	TenantName       string                   `gcfg:"tenant-name" mapstructure:"project-name" name:"os-projectName" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	TrustID          string                   `gcfg:"trust-id" mapstructure:"trust-id" name:"os-trustID" value:"optional"`
	TrusteeID        string                   `gcfg:"trustee-id" mapstructure:"trustee-id" name:"os-trusteeID" value:"optional" dependsOn:"os-trustID"`
	TrusteePassword  string                   `gcfg:"trustee-password" mapstructure:"trustee-password" name:"os-trusteePassword" value:"optional" dependsOn:"os-trustID"`
	DomainID         string                   `gcfg:"domain-id" mapstructure:"domain-id" name:"os-domainID" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	DomainName       string                   `gcfg:"domain-name" mapstructure:"domain-name" name:"os-domainName" value:"optional" dependsOn:"os-password|os-clientCertPath"`
	TenantDomainID   string                   `gcfg:"tenant-domain-id" mapstructure:"project-domain-id" name:"os-projectDomainID" value:"optional"`
	TenantDomainName string                   `gcfg:"tenant-domain-name" mapstructure:"project-domain-name" name:"os-projectDomainName" value:"optional"`
	UserDomainID     string                   `gcfg:"user-domain-id" mapstructure:"user-domain-id" name:"os-userDomainID" value:"optional"`
	UserDomainName   string                   `gcfg:"user-domain-name" mapstructure:"user-domain-name" name:"os-userDomainName" value:"optional"`
	Region           string                   `name:"os-region"`
	EndpointType     gophercloud.Availability `gcfg:"os-endpoint-type" mapstructure:"os-endpoint-type" name:"os-endpointType" value:"optional"`
	CAFile           string                   `gcfg:"ca-file" mapstructure:"ca-file" name:"os-certAuthorityPath" value:"optional"`
	TLSInsecure      string                   `gcfg:"tls-insecure" mapstructure:"tls-insecure" name:"os-TLSInsecure" value:"optional" matches:"^true|false$"`

	// TLS client auth
	CertFile string `gcfg:"cert-file" mapstructure:"cert-file" name:"os-clientCertPath" value:"optional" dependsOn:"os-clientKeyPath"`
	KeyFile  string `gcfg:"key-file" mapstructure:"key-file" name:"os-clientKeyPath" value:"optional" dependsOn:"os-clientCertPath"`

	// backward compatibility with the manila-csi-plugin
	CAFileContents string `name:"os-certAuthority" value:"optional"`

	UseClouds  bool   `gcfg:"use-clouds" mapstructure:"use-clouds" name:"os-useClouds" value:"optional"`
	CloudsFile string `gcfg:"clouds-file,omitempty" mapstructure:"clouds-file,omitempty" name:"os-cloudsFile" value:"optional"`
	Cloud      string `gcfg:"cloud,omitempty" mapstructure:"cloud,omitempty" name:"os-cloud" value:"optional"`

	ApplicationCredentialID     string `gcfg:"application-credential-id" mapstructure:"application-credential-id" name:"os-applicationCredentialID" value:"optional" dependsOn:"os-applicationCredentialSecret"`
	ApplicationCredentialName   string `gcfg:"application-credential-name" mapstructure:"application-credential-name" name:"os-applicationCredentialName" value:"optional" dependsOn:"os-applicationCredentialSecret"`
	ApplicationCredentialSecret string `gcfg:"application-credential-secret" mapstructure:"application-credential-secret" name:"os-applicationCredentialSecret" value:"optional" dependsOn:"os-applicationCredentialID|os-applicationCredentialName"`
}

func LogCfg(authOpts AuthOpts) {
	klog.V(5).Infof("AuthURL: %s", authOpts.AuthURL)
	klog.V(5).Infof("UserID: %s", authOpts.UserID)
	klog.V(5).Infof("Username: %s", authOpts.Username)
	klog.V(5).Infof("TenantID: %s", authOpts.TenantID)
	klog.V(5).Infof("TenantName: %s", authOpts.TenantName)
	klog.V(5).Infof("TrustID: %s", authOpts.TrustID)
	klog.V(5).Infof("DomainID: %s", authOpts.DomainID)
	klog.V(5).Infof("DomainName: %s", authOpts.DomainName)
	klog.V(5).Infof("TenantDomainID: %s", authOpts.TenantDomainID)
	klog.V(5).Infof("TenantDomainName: %s", authOpts.TenantDomainName)
	klog.V(5).Infof("UserDomainID: %s", authOpts.UserDomainID)
	klog.V(5).Infof("UserDomainName: %s", authOpts.UserDomainName)
	klog.V(5).Infof("Region: %s", authOpts.Region)
	klog.V(5).Infof("EndpointType: %s", authOpts.EndpointType)
	klog.V(5).Infof("CAFile: %s", authOpts.CAFile)
	klog.V(5).Infof("CertFile: %s", authOpts.CertFile)
	klog.V(5).Infof("KeyFile: %s", authOpts.KeyFile)
	klog.V(5).Infof("UseClouds: %t", authOpts.UseClouds)
	klog.V(5).Infof("CloudsFile: %s", authOpts.CloudsFile)
	klog.V(5).Infof("Cloud: %s", authOpts.Cloud)
	klog.V(5).Infof("ApplicationCredentialID: %s", authOpts.ApplicationCredentialID)
	klog.V(5).Infof("ApplicationCredentialName: %s", authOpts.ApplicationCredentialName)
}

type Logger struct{}

func (l Logger) Printf(format string, args ...interface{}) {
	debugger := klog.V(6).Enabled()

	// extra check in case, when verbosity has been changed dynamically
	if debugger {
		var skip int
		var found bool
		var gc = "/github.com/gophercloud/gophercloud"

		// detect the depth of the actual function, which calls gophercloud code
		// 10 is the common depth from the logger to "github.com/gophercloud/gophercloud"
		for i := 10; i <= 20; i++ {
			if _, file, _, ok := runtime.Caller(i); ok && !found && strings.Contains(file, gc) {
				found = true
				continue
			} else if ok && found && !strings.Contains(file, gc) {
				skip = i
				break
			} else if !ok {
				break
			}
		}

		for _, v := range strings.Split(fmt.Sprintf(format, args...), "\n") {
			klog.InfoDepth(skip, v)
		}
	}
}

func (authOpts AuthOpts) ToAuthOptions() gophercloud.AuthOptions {
	opts := clientconfig.ClientOpts{
		// this is needed to disable the clientconfig.AuthOptions func env detection
		EnvPrefix: "_",
		Cloud:     authOpts.Cloud,
		AuthInfo: &clientconfig.AuthInfo{
			AuthURL:                     authOpts.AuthURL,
			UserID:                      authOpts.UserID,
			Username:                    authOpts.Username,
			Password:                    authOpts.Password,
			ProjectID:                   authOpts.TenantID,
			ProjectName:                 authOpts.TenantName,
			DomainID:                    authOpts.DomainID,
			DomainName:                  authOpts.DomainName,
			ProjectDomainID:             authOpts.TenantDomainID,
			ProjectDomainName:           authOpts.TenantDomainName,
			UserDomainID:                authOpts.UserDomainID,
			UserDomainName:              authOpts.UserDomainName,
			ApplicationCredentialID:     authOpts.ApplicationCredentialID,
			ApplicationCredentialName:   authOpts.ApplicationCredentialName,
			ApplicationCredentialSecret: authOpts.ApplicationCredentialSecret,
		},
	}

	ao, err := clientconfig.AuthOptions(&opts)
	if err != nil {
		klog.V(1).Infof("Error parsing auth: %s", err)
		return gophercloud.AuthOptions{}
	}

	// Persistent service, so we need to be able to renew tokens.
	ao.AllowReauth = true

	return *ao
}

func (authOpts AuthOpts) ToAuth3Options() tokens.AuthOptions {
	ao := authOpts.ToAuthOptions()

	var scope tokens.Scope
	if ao.Scope != nil {
		scope.ProjectID = ao.Scope.ProjectID
		scope.ProjectName = ao.Scope.ProjectName
		scope.DomainID = ao.Scope.DomainID
		scope.DomainName = ao.Scope.DomainName
	}

	return tokens.AuthOptions{
		IdentityEndpoint:            ao.IdentityEndpoint,
		UserID:                      ao.UserID,
		Username:                    ao.Username,
		Password:                    ao.Password,
		DomainID:                    ao.DomainID,
		DomainName:                  ao.DomainName,
		ApplicationCredentialID:     ao.ApplicationCredentialID,
		ApplicationCredentialName:   ao.ApplicationCredentialName,
		ApplicationCredentialSecret: ao.ApplicationCredentialSecret,
		Scope:                       scope,
		AllowReauth:                 ao.AllowReauth,
	}
}

// replaceEmpty is a helper function to replace empty fields with another field
func replaceEmpty(a string, b string) string {
	if a == "" {
		return b
	}
	return a
}

// ReadClouds reads Reads clouds.yaml to generate a Config
// Allows the cloud-config to have priority
func ReadClouds(authOpts *AuthOpts) error {
	co := new(clientconfig.ClientOpts)
	if authOpts.Cloud != "" {
		co.Cloud = authOpts.Cloud
	}
	cloud, err := clientconfig.GetCloudFromYAML(co)
	if err != nil {
		return err
	}

	authOpts.AuthURL = replaceEmpty(authOpts.AuthURL, cloud.AuthInfo.AuthURL)
	authOpts.UserID = replaceEmpty(authOpts.UserID, cloud.AuthInfo.UserID)
	authOpts.Username = replaceEmpty(authOpts.Username, cloud.AuthInfo.Username)
	authOpts.Password = replaceEmpty(authOpts.Password, cloud.AuthInfo.Password)
	authOpts.TenantID = replaceEmpty(authOpts.TenantID, cloud.AuthInfo.ProjectID)
	authOpts.TenantName = replaceEmpty(authOpts.TenantName, cloud.AuthInfo.ProjectName)
	authOpts.DomainID = replaceEmpty(authOpts.DomainID, cloud.AuthInfo.DomainID)
	authOpts.DomainName = replaceEmpty(authOpts.DomainName, cloud.AuthInfo.DomainName)
	authOpts.TenantDomainID = replaceEmpty(authOpts.TenantDomainID, cloud.AuthInfo.ProjectDomainID)
	authOpts.TenantDomainName = replaceEmpty(authOpts.TenantDomainName, cloud.AuthInfo.ProjectDomainName)
	authOpts.UserDomainID = replaceEmpty(authOpts.UserDomainID, cloud.AuthInfo.UserDomainID)
	authOpts.UserDomainName = replaceEmpty(authOpts.UserDomainName, cloud.AuthInfo.UserDomainName)
	authOpts.Region = replaceEmpty(authOpts.Region, cloud.RegionName)
	authOpts.EndpointType = gophercloud.Availability(replaceEmpty(string(authOpts.EndpointType), cloud.EndpointType))
	authOpts.CAFile = replaceEmpty(authOpts.CAFile, cloud.CACertFile)
	authOpts.CertFile = replaceEmpty(authOpts.CertFile, cloud.ClientCertFile)
	authOpts.KeyFile = replaceEmpty(authOpts.KeyFile, cloud.ClientKeyFile)
	authOpts.ApplicationCredentialID = replaceEmpty(authOpts.ApplicationCredentialID, cloud.AuthInfo.ApplicationCredentialID)
	authOpts.ApplicationCredentialName = replaceEmpty(authOpts.ApplicationCredentialName, cloud.AuthInfo.ApplicationCredentialName)
	authOpts.ApplicationCredentialSecret = replaceEmpty(authOpts.ApplicationCredentialSecret, cloud.AuthInfo.ApplicationCredentialSecret)

	return nil
}

// NewOpenStackClient creates a new instance of the openstack client
func NewOpenStackClient(cfg *AuthOpts, userAgent string, extraUserAgent ...string) (*gophercloud.ProviderClient, error) {
	provider, err := openstack.NewClient(cfg.AuthURL)
	if err != nil {
		return nil, err
	}

	ua := gophercloud.UserAgent{}
	ua.Prepend(fmt.Sprintf("%s/%s", userAgent, version.Version))
	for _, data := range extraUserAgent {
		ua.Prepend(data)
	}
	provider.UserAgent = ua
	klog.V(4).Infof("Using user-agent %s", ua.Join())

	var caPool *x509.CertPool
	if cfg.CAFile != "" {
		// read and parse CA certificate from file
		caPool, err = cert.NewPool(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read and parse %s certificate: %s", cfg.CAFile, err)
		}
	} else if cfg.CAFileContents != "" {
		// parse CA certificate from the contents
		caPool = x509.NewCertPool()
		if ok := caPool.AppendCertsFromPEM([]byte(cfg.CAFileContents)); !ok {
			return nil, fmt.Errorf("failed to parse os-certAuthority certificate")
		}
	}

	config := &tls.Config{}
	config.InsecureSkipVerify = cfg.TLSInsecure == "true"

	if caPool != nil {
		config.RootCAs = caPool
	}

	// configure TLS client auth
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading TLS key pair: %s", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}

	provider.HTTPClient.Transport = net.SetOldTransportDefaults(&http.Transport{TLSClientConfig: config})

	if klog.V(6).Enabled() {
		provider.HTTPClient.Transport = &client.RoundTripper{
			Rt:     provider.HTTPClient.Transport,
			Logger: &Logger{},
		}
	}

	if cfg.TrustID != "" {
		opts := cfg.ToAuth3Options()

		// support for the legacy manila auth
		// if TrusteeID and TrusteePassword were defined, then use them
		opts.UserID = replaceEmpty(cfg.TrusteeID, opts.UserID)
		opts.Password = replaceEmpty(cfg.TrusteePassword, opts.Password)

		authOptsExt := trusts.AuthOptsExt{
			TrustID:            cfg.TrustID,
			AuthOptionsBuilder: &opts,
		}
		err = openstack.AuthenticateV3(provider, authOptsExt, gophercloud.EndpointOpts{})

		return provider, err
	}

	opts := cfg.ToAuthOptions()
	err = openstack.Authenticate(provider, opts)

	return provider, err
}
