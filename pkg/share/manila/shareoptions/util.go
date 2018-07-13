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

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func readSecrets(c clientset.Interface, secretRef *v1.SecretReference) (map[string]string, error) {
	secrets, err := c.CoreV1().Secrets(secretRef.Namespace).Get(secretRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	secretsData := make(map[string]string)
	for k, v := range secrets.Data {
		secretsData[k] = string(v)
	}

	return secretsData, nil
}
