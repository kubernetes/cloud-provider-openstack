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

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/spf13/pflag"

	"golang.org/x/crypto/ssh/terminal"

	"k8s.io/cloud-provider-openstack/pkg/identity/keystone"
	kflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
)

const errRespTemplate string = `{
	"apiVersion": "client.authentication.k8s.io/v1beta1",
	"kind": "ExecCredential",
	"status": {}
}`

const respTemplate string = `{
	"apiVersion": "client.authentication.k8s.io/v1beta1",
	"kind": "ExecCredential",
	"status": {
		"token": "%v",
		"expirationTimestamp": "%v"
	}
}`

func promptForString(field string, r io.Reader, show bool) (result string, err error) {
	// We have to print output to Stderr, because Stdout is redirected and not shown to the user.
	fmt.Fprintf(os.Stderr, "Please enter %s: ", field)

	if show {
		_, err = fmt.Fscan(r, &result)
	} else {
		var data []byte
		data, err = terminal.ReadPassword(int(os.Stdin.Fd()))
		result = string(data)
		fmt.Fprintf(os.Stderr, "\n")
	}
	return result, err
}

// prompt pulls keystone auth url, domain, project, username and password from stdin,
// if they are not specified initially (i.e. equal "").
func prompt(url string, domain string, user string, project string, password string, applicationCredentialID string, applicationCredentialName string, applicationCredentialSecret string) (gophercloud.AuthOptions, error) {
	var err error
	var options gophercloud.AuthOptions

	if url == "" {
		url, err = promptForString("Keystone Auth URL", os.Stdin, true)
		if err != nil {
			return options, err
		}
	}

	if domain == "" {
		domain, err = promptForString("domain name", os.Stdin, true)
		if err != nil {
			return options, err
		}
	}

	if user == "" {
		user, err = promptForString("user name", os.Stdin, true)
		if err != nil {
			return options, err
		}
	}

	if project == "" && applicationCredentialID == "" && applicationCredentialName == "" {
		project, err = promptForString("project name", os.Stdin, true)
		if err != nil {
			return options, err
		}
	}

	if password == "" && applicationCredentialID == "" && applicationCredentialName == "" {
		password, err = promptForString("password", nil, false)
		if err != nil {
			return options, err
		}
	}

	options = gophercloud.AuthOptions{
		IdentityEndpoint:            url,
		Username:                    user,
		TenantName:                  project,
		Password:                    password,
		DomainName:                  domain,
		ApplicationCredentialID:     applicationCredentialID,
		ApplicationCredentialName:   applicationCredentialName,
		ApplicationCredentialSecret: applicationCredentialSecret,
	}

	return options, nil
}

func argumentsAreSet(url, user, project, password, domain, applicationCredentialID, applicationCredentialName, applicationCredentialSecret string) bool {
	if url == "" {
		return false
	}

	if user != "" && project != "" && domain != "" && password != "" {
		return true
	}

	if applicationCredentialID != "" && applicationCredentialName != "" && applicationCredentialSecret != "" {
		return true
	}

	return false
}

func main() {
	// Glog requires this otherwise it complains.
	flag.CommandLine.Parse(nil)
	// This is a temporary hack to enable proper logging until upstream dependencies
	// are migrated to fully utilize klog instead of glog.
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	var url string
	var domain string
	var user string
	var project string
	var password string
	var clientCertPath string
	var clientKeyPath string
	var clientCAPath string
	var options keystone.Options
	var err error
	var applicationCredentialID string
	var applicationCredentialName string
	var applicationCredentialSecret string

	pflag.StringVar(&url, "keystone-url", os.Getenv("OS_AUTH_URL"), "URL for the OpenStack Keystone API")
	pflag.StringVar(&domain, "domain-name", os.Getenv("OS_DOMAIN_NAME"), "Keystone domain name")
	pflag.StringVar(&user, "user-name", os.Getenv("OS_USERNAME"), "User name")
	pflag.StringVar(&project, "project-name", os.Getenv("OS_PROJECT_NAME"), "Keystone project name")
	pflag.StringVar(&password, "password", os.Getenv("OS_PASSWORD"), "Password")
	pflag.StringVar(&clientCertPath, "cert", os.Getenv("OS_CERT"), "Client certificate bundle file")
	pflag.StringVar(&clientKeyPath, "key", os.Getenv("OS_KEY"), "Client certificate key file")
	pflag.StringVar(&clientCAPath, "cacert", os.Getenv("OS_CACERT"), "Certificate authority file")
	pflag.StringVar(&applicationCredentialID, "application-credential-id", os.Getenv("OS_APPLICATION_CREDENTIAL_ID"), "Application Credential ID")
	pflag.StringVar(&applicationCredentialName, "application-credential-name", os.Getenv("OS_APPLICATION_CREDENTIAL_NAME"), "Application Credential Name")
	pflag.StringVar(&applicationCredentialSecret, "application-credential-secret", os.Getenv("OS_APPLICATION_CREDENTIAL_SECRET"), "Application Credential Secret")
	pflag.CommandLine.AddGoFlagSet(klogFlags)
	kflag.InitFlags()

	// Generate Gophercloud Auth Options based on input data from stdin
	// if IsTerminal returns "true", or from env variables otherwise.
	if !terminal.IsTerminal(int(os.Stdin.Fd())) {
		// If all requiered arguments are set use them
		if argumentsAreSet(url, user, project, password, domain, applicationCredentialID, applicationCredentialName, applicationCredentialSecret) {
			options.AuthOptions = gophercloud.AuthOptions{
				IdentityEndpoint:            url,
				Username:                    user,
				TenantName:                  project,
				Password:                    password,
				DomainName:                  domain,
				ApplicationCredentialID:     applicationCredentialID,
				ApplicationCredentialName:   applicationCredentialName,
				ApplicationCredentialSecret: applicationCredentialSecret,
			}
		} else {
			// Use environment variables if arguments are missing
			authOpts, err := clientconfig.AuthOptions(nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to read openstack env vars: %s\n", err)
				os.Exit(1)
			}
			options.AuthOptions = *authOpts
		}
	} else {
		options.AuthOptions, err = prompt(url, domain, user, project, password, applicationCredentialID, applicationCredentialName, applicationCredentialSecret)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read data from console: %s\n", err)
			os.Exit(1)
		}
	}

	options.ClientCertPath = clientCertPath
	options.ClientKeyPath = clientKeyPath
	options.ClientCAPath = clientCAPath

	token, err := keystone.GetToken(options)
	if err != nil {
		if _, ok := err.(gophercloud.ErrDefault401); ok {
			fmt.Println(errRespTemplate)
			os.Stderr.WriteString("Invalid user credentials were provided\n")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "An error occurred: %v\n", err)
		os.Exit(1)
	}

	out := fmt.Sprintf(respTemplate, token.ID, token.ExpiresAt.Format(time.RFC3339Nano))
	fmt.Println(out)
}
