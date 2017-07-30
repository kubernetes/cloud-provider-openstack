/*
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

package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authentication/user"
	"log"
)

type userInfo struct {
	Username string              `json:"username"`
	UID      string              `json:"uid"`
	Groups   []string            `json:"groups"`
	Extra    map[string][]string `json:"extra"`
}
type status struct {
	Authenticated bool     `json:"authenticated"`
	User          userInfo `json:"user"`
}

type WebhookHandler struct {
	Authenticator authenticator.Token
	Authorizer    authorizer.Authorizer
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	err := decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var apiVersion = data["apiVersion"].(string)
	var kind = data["kind"].(string)

	if apiVersion != "authentication.k8s.io/v1beta1" && apiVersion != "authorization.k8s.io/v1beta1" {
		http.Error(w, fmt.Sprintf("unknown apiVersion %q", apiVersion),
			http.StatusBadRequest)
		return
	}
	if kind == "TokenReview" {
		var token = data["spec"].(map[string]interface{})["token"].(string)
		h.authenticateToken(w, r, token, data)
	} else if kind == "SubjectAccessReview" {
		h.authorizeToken(w, r, data)
	} else {
		http.Error(w, fmt.Sprintf("unknown kind/apiVersion %q %q", kind, apiVersion),
			http.StatusBadRequest)
	}
}

func (h *WebhookHandler) authenticateToken(w http.ResponseWriter, r *http.Request, token string, data map[string]interface{}) {
	//log.Printf(">>>> authenticateToken data : %#v\n", data)
	user, authenticated, err := h.Authenticator.AuthenticateToken(token)
	log.Printf("<<<< authenticateToken : %v, %v, %v\n", token, user, err)

	if !authenticated {
		var response status
		response.Authenticated = false
		data["status"] = response

		output, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(output)
		return
	}

	var info userInfo
	info.Username = user.GetName()
	info.UID = user.GetUID()
	info.Groups = user.GetGroups()
	info.Extra = user.GetExtra()

	var response status
	response.Authenticated = true
	response.User = info

	data["status"] = response

	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}

func getField(data map[string]interface{}, name string) string {
	if v, ok := data[name]; ok {
		return v.(string)
	}
	return ""
}

func (h *WebhookHandler) authorizeToken(w http.ResponseWriter, r *http.Request, data map[string]interface{}) {
	output, err := json.MarshalIndent(data, "", "  ")
	log.Printf(">>>> authorizeToken data : %s\n", string(output))

	spec := data["spec"].(map[string]interface{})

	username :=  spec["user"]
	usr := &user.DefaultInfo{
		Name:   username.(string),
	}
	attrs := authorizer.AttributesRecord{
		User: usr,
	}

	groups := spec["group"].([]interface{})
	for _, v := range groups {
			usr.Groups = append(usr.Groups, v.(string))
	}
	if extras, ok := spec["extra"].(map[string]interface{}); ok {
		usr.Extra = make(map[string][]string, len(extras))
		for key, value := range extras {
				for _,v := range value.([]interface{}) {
					if data, ok := usr.Extra[key] ; ok {
						usr.Extra[key] = append(data, v.(string))
					} else {
						usr.Extra[key] = []string{v.(string)}
					}
				}
		}
	}

	if resourceAttributes, ok := spec["resourceAttributes"]; ok {
		v := resourceAttributes.(map[string]interface{})
		attrs.ResourceRequest = true
		attrs.Verb = getField(v, "verb")
		attrs.Namespace = getField(v, "namespace")
		attrs.APIGroup = getField(v, "group")
		attrs.APIVersion = getField(v, "version")
		attrs.Resource = getField(v, "resource")
		attrs.Name = getField(v, "name")
	} else if nonResourceAttributes, ok := spec["nonResourceAttributes"]; ok {
		v := nonResourceAttributes.(map[string]interface{})
		attrs.ResourceRequest = false
		attrs.Verb = getField(v, "verb")
		attrs.Path = getField(v, "path")
	} else {
		err := fmt.Errorf("Unable to find attributes")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	allowed, reason, err := h.Authorizer.Authorize(attrs)
	log.Printf("<<<< authorizeToken : %v, %v, %v\n", allowed, reason, err)
	if err != nil {
		http.Error(w, reason, http.StatusInternalServerError)
		return
	}

	delete(data, "spec")
	data["status"] = map[string]interface{}{
		"allowed": allowed,
	}
	output, err = json.MarshalIndent(data, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}
