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

package shareoptions

// CommonOptions contains options common for any backend/protocol
type CommonOptions struct {
	Zones    string `name:"zones" value:"default=nova"`
	Type     string `name:"type" value:"default=default"`
	Protocol string `name:"protocol"`
	Backend  string `name:"backend"`

	OSSecretName         string `name:"osSecretName"`
	OSSecretNamespace    string `name:"osSecretNamespace" value:"default=default"`
	ShareSecretNamespace string `name:"shareSecretNamespace" value:"coalesce=osSecretNamespace"`

	OSShareID       string `name:"osShareID" value:"requires=osShareAccessID"`
	OSShareName     string `name:"osShareName" value:"requires=osShareAccessID"`
	OSShareAccessID string `name:"osShareAccessID" value:"requires=osShareID|osShareName"`
}
