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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/spf13/pflag"

	"golang.org/x/crypto/ssh/terminal"

	kflag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/client-go/pkg/apis/clientauthentication/v1alpha1"
	"k8s.io/cloud-provider-openstack/pkg/identity/keystone"
)

const errRespTemplate string = `{
	"apiVersion": "client.authentication.k8s.io/v1alpha1",
	"kind": "ExecCredential",
	"spec": {
		"response": {
			"code": 401,
			"header": {},
		},
	}
}`

const respTemplate string = `{
	"apiVersion": "client.authentication.k8s.io/v1alpha1",
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
		_, err = fmt.Fscanln(r, &result)
	} else {
		var data []byte
		if terminal.IsTerminal(int(os.Stdin.Fd())) {
			data, err = terminal.ReadPassword(int(os.Stdin.Fd()))
			result = string(data)
			fmt.Fprintf(os.Stderr, "\n")
		} else {
			return "", fmt.Errorf("error reading input for %s", field)
		}
	}

	// intentionally ignore "unexpected newline" errors to allow read empty lines from the console
	if err != nil && err.Error() == "unexpected newline" {
		return "", nil
	}

	return result, err
}

// prompt pulls missing auth parameters from stdin, if they were not specified initially (i.e. equal "")
func prompt(options *gophercloud.AuthOptions) error {
	var err error

	if options.IdentityEndpoint == "" {
		options.IdentityEndpoint, err = promptForString("Keystone auth URL", os.Stdin, true)
		if err != nil {
			return err
		}

		if options.IdentityEndpoint == "" {
			return fmt.Errorf("Keystone url must be specified")
		}
	}

	if options.DomainID == "" && options.DomainName == "" {
		options.DomainName, err = promptForString("domain name (leave empty to set domain id instead)", os.Stdin, true)
		if err != nil {
			return err
		}

		if options.DomainName == "" {
			options.DomainID, err = promptForString("domain ID", os.Stdin, true)
			if err != nil {
				return err
			}
		}

		if options.DomainID == "" && options.DomainName == "" {
			return fmt.Errorf("either domain ID or domain name must be specified")
		}
	}

	if options.UserID == "" && options.Username == "" {
		options.Username, err = promptForString("user name (leave empty to set user id instead)", os.Stdin, true)
		if err != nil {
			return err
		}

		if options.Username == "" {
			options.UserID, err = promptForString("user ID", os.Stdin, true)
			if err != nil {
				return err
			}
		}

		if options.UserID == "" && options.Username == "" {
			return fmt.Errorf("either user ID or user name must be specified")
		}
	}

	if options.TenantID == "" && options.TenantName == "" {
		options.TenantName, err = promptForString("project name (leave empty to set project id instead)", os.Stdin, true)
		if err != nil {
			return err
		}

		if options.TenantName == "" {
			options.UserID, err = promptForString("project ID", os.Stdin, true)
			if err != nil {
				return err
			}
		}

		if options.TenantID == "" && options.TenantName == "" {
			fmt.Fprintf(os.Stderr, "Neither project ID nor project name have been specified. Proceed with domain-scoped token.")
		}
	}

	if options.Password == "" {
		options.Password, err = promptForString("password", nil, false)
		if err != nil {
			return err
		}
	}

	return nil
}

// KuberneteExecInfo holds information passed to the plugin by the transport. This
// contains runtime specific information, such as if the session is interactive,
// auth API version and kind of request.
type KuberneteExecInfo struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Spec       v1alpha1.ExecCredentialSpec `json:"spec"`
}

func validateKebernetesExecInfo(kei KuberneteExecInfo) error {
	if kei.APIVersion != v1alpha1.SchemeGroupVersion.String() {
		return fmt.Errorf("unsupported API version: %v", kei.APIVersion)
	}

	if kei.Kind != "ExecCredential" {
		return fmt.Errorf("incorrect request kind: %v", kei.Kind)
	}

	return nil
}

func main() {
	var url string
	var domainID string
	var domainName string
	var userID string
	var userName string
	var projectID string
	var projectName string
	var password string
	var options gophercloud.AuthOptions
	var err error

	pflag.StringVar(&url, "keystone-url", os.Getenv("OS_AUTH_URL"), "URL for the OpenStack Keystone API")
	pflag.StringVar(&domainID, "domain-id", os.Getenv("OS_DOMAIN_ID"), "Keystone domain identifier")
	pflag.StringVar(&domainName, "domain-name", os.Getenv("OS_DOMAIN_NAME"), "Keystone domain name")
	pflag.StringVar(&userID, "user-id", os.Getenv("OS_USERID"), "User identifier")
	pflag.StringVar(&userName, "user-name", os.Getenv("OS_USERNAME"), "User name")
	pflag.StringVar(&projectID, "project-id", os.Getenv("OS_PROJECT_ID"), "Keystone project identifier")
	pflag.StringVar(&projectName, "project-name", os.Getenv("OS_PROJECT_NAME"), "Keystone project name")
	pflag.StringVar(&password, "password", os.Getenv("OS_PASSWORD"), "Password")
	kflag.InitFlags()

	keiData := os.Getenv("KUBERNETES_EXEC_INFO")
	if keiData == "" {
		fmt.Fprintln(os.Stderr, "KUBERNETES_EXEC_INFO env variable must be set")
		os.Exit(1)
	}

	kei := KuberneteExecInfo{}
	err = json.Unmarshal([]byte(keiData), &kei)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to parse KUBERNETES_EXEC_INFO value")
		os.Exit(1)
	}

	err = validateKebernetesExecInfo(kei)
	if err != nil {
		fmt.Fprintf(os.Stderr, "An error occurred: %v\n", err)
		os.Exit(1)
	}

	// Generate Gophercloud Auth Options based on input data from stdin
	// if "intaractive" is set to true, or from env variables otherwise.
	if !kei.Spec.Interactive {
		options, err = openstack.AuthOptionsFromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read openstack env vars: %s", err)
			os.Exit(1)
		}
	} else {
		options = gophercloud.AuthOptions{
			IdentityEndpoint: url,
			UserID:           userID,
			Username:         userName,
			TenantID:         projectID,
			TenantName:       projectName,
			Password:         password,
			DomainID:         domainID,
			DomainName:       domainName,
		}

		// Read missing auth options from stdin
		err = prompt(&options)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read data from console: %s", err)
			os.Exit(1)
		}
	}

	token, err := keystone.GetToken(options)
	if err != nil {
		if _, ok := err.(gophercloud.ErrDefault401); ok {
			fmt.Println(errRespTemplate)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "An error occurred: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf(respTemplate, token.ID, token.ExpiresAt.Format(time.RFC3339Nano))
}
