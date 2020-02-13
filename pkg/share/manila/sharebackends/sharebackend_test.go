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

package sharebackends

import (
	"fmt"
	"io/ioutil"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions"
	"net/http"
	"reflect"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	th "github.com/gophercloud/gophercloud/testhelper"
	fakeclient "github.com/gophercloud/gophercloud/testhelper/client"
	"k8s.io/api/core/v1"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

func TestSplitExportLocation(t *testing.T) {
	var (
		expLocation       shares.ExportLocation
		address, location string
		err               error
	)

	expLocation.Path = "addr1:port1,addr2:port2:/some-path"
	if address, location, err = splitExportLocation(&expLocation); err != nil {
		t.Fatalf("failed to split export location: %v", err)
	}

	if address != "addr1:port1,addr2:port2" {
		t.Errorf("wrong address: wanted addr1:port1,addr2:port2, got %s", address)
	}

	if location != "/some-path" {
		t.Errorf("wrong location: wanted /some-path, got %s", address)
	}
}

func TestCSICephFSBuildSource(t *testing.T) {
	var b CSICephFS

	args := BuildSourceArgs{
		VolumeHandle: "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
		Share: &shares.Share{
			ID: "011d21e2-fbc3-4e4a-9993-9ea223f73264",
		},
		Options: &shareoptions.ShareOptions{
			CSICEPHFSdriver:      "csi-cephfs",
			CSICEPHFSmounter:     "fuse",
			OSSecretNamespace:    "default",
			ShareSecretNamespace: "default",
		},
		ShareSecretRef: &v1.SecretReference{
			Name:      "manila-011d21e2-fbc3-4e4a-9993-9ea223f73264",
			Namespace: "default",
		},
		Location: &shares.ExportLocation{
			Path: "192.168.2.1:6789,192.168.2.2:6789:/shares/011d21e2-fbc3-4e4a-9993-9ea223f73264",
		},
	}

	source, err := b.BuildSource(&args)

	if err != nil {
		t.Errorf("failed to build PV source: %v	", err)
	}

	expected := v1.PersistentVolumeSource{
		CSI: &v1.CSIPersistentVolumeSource{
			Driver:       "csi-cephfs",
			VolumeHandle: args.VolumeHandle,
			VolumeAttributes: map[string]string{
				"monitors":        "192.168.2.1:6789,192.168.2.2:6789",
				"rootPath":        "/shares/011d21e2-fbc3-4e4a-9993-9ea223f73264",
				"mounter":         "fuse",
				"provisionVolume": "false",
			},
			NodeStageSecretRef: &v1.SecretReference{
				Name:      "manila-011d21e2-fbc3-4e4a-9993-9ea223f73264",
				Namespace: "default",
			},
		},
	}

	eq := reflect.DeepEqual(source, &expected)

	if !eq {
		t.Errorf("unexpected PV source contents: got %+v, expected %+v", source, &expected)
	}
}

func TestCSICephFSGrantAccess(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	listAccessRightsRequest := `{"access_list":null}`
	listAccessRightsResponse := `
		{
			"access_list": [
				{
					"share_id": "011d21e2-fbc3-4e4a-9993-9ea223f73264",
					"access_type": "cephx",
					"access_to": "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
					"access_key": "MDExZDIxZTItZmJjMy00ZTRhLTk5OTMt",
					"access_level": "rw",
					"state": "available",
					"id": "a2f226a5-cee8-430b-8a03-78a59bd84ee8"
				}
			]
		}`

	grantAccessRequest := `{"allow_access":{"access_level":"rw","access_to":"pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264","access_type":"cephx"}}`

	grantAccessResponse := `{
    "access": {
		"share_id": "011d21e2-fbc3-4e4a-9993-9ea223f73264",
		"access_type": "ip",
		"access_to": "0.0.0.0/0",
		"access_key": "",
		"access_level": "rw",
		"state": "new",
		"id": "a2f226a5-cee8-430b-8a03-78a59bd84ee8"
    }
}`

	th.Mux.HandleFunc("/shares/011d21e2-fbc3-4e4a-9993-9ea223f73264/action", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "X-Auth-Token", fakeclient.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json")

		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body failed: %v", err)
		}

		strBody := string(body)

		if strBody == listAccessRightsRequest {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, listAccessRightsResponse)
		} else if strBody == grantAccessRequest {
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, grantAccessResponse)
		} else {
			t.Errorf("unexpected request: '%s'", strBody)
		}
	})

	args := GrantAccessArgs{
		Share: &shares.Share{
			ID:   "011d21e2-fbc3-4e4a-9993-9ea223f73264",
			Name: "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
		},
		ShareSecretRef: &v1.SecretReference{Namespace: "default", Name: "manila-xxx"},
		Options: &shareoptions.ShareOptions{
			OSSecretNamespace:    "default",
			ShareSecretNamespace: "default",
		},
		Clientset: fakeclientset.NewSimpleClientset(),
		Client:    manilaclient.NewFromServiceClient(fakeclient.ServiceClient()),
	}

	b := CSICephFS{}

	accessRight, err := b.GrantAccess(&args)

	if err != nil {
		t.Errorf("failed to grant access: %v", err)
	}

	eq := reflect.DeepEqual(accessRight, &shares.AccessRight{
		ID:          "a2f226a5-cee8-430b-8a03-78a59bd84ee8",
		ShareID:     "011d21e2-fbc3-4e4a-9993-9ea223f73264",
		AccessType:  "cephx",
		AccessTo:    "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
		AccessKey:   "MDExZDIxZTItZmJjMy00ZTRhLTk5OTMt",
		AccessLevel: "rw",
		State:       "available",
	})

	if !eq {
		t.Errorf("unexpected AccessRight contents")
	}
}
