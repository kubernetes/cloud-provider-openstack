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
		_, err = fmt.Fscan(r, &result)
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
	return result, err
}

// prompt pulls keystone auth url, domain, project, username and password from stdin,
// if they are not specified initially (i.e. equal "").
func prompt(url string, domain string, user string, project string, password string) (gophercloud.AuthOptions, error) {
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

	if project == "" {
		project, err = promptForString("project name", os.Stdin, true)
		if err != nil {
			return options, err
		}
	}

	if password == "" {
		password, err = promptForString("password", nil, false)
		if err != nil {
			return options, err
		}
	}

	options = gophercloud.AuthOptions{
		IdentityEndpoint: url,
		Username:         user,
		TenantName:       project,
		Password:         password,
		DomainName:       domain,
	}

	return options, nil
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
	var domain string
	var user string
	var project string
	var password string
	var options gophercloud.AuthOptions
	var err error

	pflag.StringVar(&url, "keystone-url", os.Getenv("OS_AUTH_URL"), "URL for the OpenStack Keystone API")
	pflag.StringVar(&domain, "domain-name", os.Getenv("OS_DOMAIN_NAME"), "Keystone domain name")
	pflag.StringVar(&user, "user-name", os.Getenv("OS_USERNAME"), "User name")
	pflag.StringVar(&project, "project-name", os.Getenv("OS_PROJECT_NAME"), "Keystone project name")
	pflag.StringVar(&password, "password", os.Getenv("OS_PASSWORD"), "Password")
	kflag.InitFlags()

	keiData := os.Getenv("KUBERNETES_EXEC_INFO")
	if keiData == "" {
		fmt.Fprintln(os.Stderr, "KUBERNETES_EXEC_INFO env variable must be set.")
		os.Exit(1)
	}

	kei := KuberneteExecInfo{}
	err = json.Unmarshal([]byte(keiData), &kei)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to parse KUBERNETES_EXEC_INFO value.")
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
		options, err = prompt(url, domain, user, project, password)
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

	out := fmt.Sprintf(respTemplate, token.ID, token.ExpiresAt.Format(time.RFC3339Nano))
	fmt.Println(out)
}
