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
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/utils/v2/openstack/clientconfig"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	"golang.org/x/term"

	"k8s.io/cloud-provider-openstack/pkg/identity/keystone"
	"k8s.io/cloud-provider-openstack/pkg/version"
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
		data, err = term.ReadPassword(int(os.Stdin.Fd()))
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

var (
	url                         string
	domain                      string
	user                        string
	project                     string
	password                    string
	clientCertPath              string
	clientKeyPath               string
	clientCAPath                string
	options                     keystone.Options
	err                         error
	applicationCredentialID     string
	applicationCredentialName   string
	applicationCredentialSecret string
)

func main() {
	cmd := &cobra.Command{
		Use:   "client-keystone-auth",
		Short: "Keystone client credential plugin for Kubernetes",
		Run: func(cmd *cobra.Command, args []string) {
			handle(context.Background())
		},
		Version: version.Version,
	}

	cmd.PersistentFlags().StringVar(&url, "keystone-url", os.Getenv("OS_AUTH_URL"), "URL for the OpenStack Keystone API")
	cmd.PersistentFlags().StringVar(&domain, "domain-name", os.Getenv("OS_DOMAIN_NAME"), "Keystone domain name")
	cmd.PersistentFlags().StringVar(&user, "user-name", os.Getenv("OS_USERNAME"), "User name")
	cmd.PersistentFlags().StringVar(&project, "project-name", os.Getenv("OS_PROJECT_NAME"), "Keystone project name")
	cmd.PersistentFlags().StringVar(&password, "password", os.Getenv("OS_PASSWORD"), "Password")
	cmd.PersistentFlags().StringVar(&clientCertPath, "cert", os.Getenv("OS_CERT"), "Client certificate bundle file")
	cmd.PersistentFlags().StringVar(&clientKeyPath, "key", os.Getenv("OS_KEY"), "Client certificate key file")
	cmd.PersistentFlags().StringVar(&clientCAPath, "cacert", os.Getenv("OS_CACERT"), "Certificate authority file")
	cmd.PersistentFlags().StringVar(&applicationCredentialID, "application-credential-id", os.Getenv("OS_APPLICATION_CREDENTIAL_ID"), "Application Credential ID")
	cmd.PersistentFlags().StringVar(&applicationCredentialName, "application-credential-name", os.Getenv("OS_APPLICATION_CREDENTIAL_NAME"), "Application Credential Name")
	cmd.PersistentFlags().StringVar(&applicationCredentialSecret, "application-credential-secret", os.Getenv("OS_APPLICATION_CREDENTIAL_SECRET"), "Application Credential Secret")

	code := cli.Run(cmd)
	os.Exit(code)
}

func handle(ctx context.Context) {
	// Generate Gophercloud Auth Options based on input data from stdin
	// if IsTerminal returns "true", or from env variables otherwise.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// If all required arguments are set use them
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

	token, err := keystone.GetToken(ctx, options)
	if err != nil {
		if gophercloud.ResponseCodeIs(err, http.StatusUnauthorized) {
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
