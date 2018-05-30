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

package manila

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	th "github.com/gophercloud/gophercloud/testhelper"
	fakeclient "github.com/gophercloud/gophercloud/testhelper/client"
	"github.com/kubernetes-incubator/external-storage/lib/controller"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/cloud-provider-openstack/pkg/share/manila/shareoptions"
)

func TestChooseExportLocation(t *testing.T) {
	const (
		validPath              = "ip://directory"
		preferredPath          = "ip://preferred/directory"
		emptyPath              = ""
		spacesOnlyPath         = "  	  "
		shareExportLocationID1 = "123456-1"
		shareExportLocationID2 = "1234567-1"
		shareExportLocationID3 = "1234567-2"
		shareExportLocationID4 = "7654321-1"
		shareID1               = "123456"
		shareID2               = "1234567"
	)

	tests := []struct {
		testCaseName string
		locs         []shares.ExportLocation
		want         shares.ExportLocation
	}{
		{
			testCaseName: "Match first item:",
			locs: []shares.ExportLocation{
				{
					Path:            validPath,
					ShareInstanceID: shareID1,
					IsAdminOnly:     false,
					ID:              shareExportLocationID1,
					Preferred:       false,
				},
			},
			want: shares.ExportLocation{
				Path:            validPath,
				ShareInstanceID: shareID1,
				IsAdminOnly:     false,
				ID:              shareExportLocationID1,
				Preferred:       false,
			},
		},
		{
			testCaseName: "Match preferred location:",
			locs: []shares.ExportLocation{
				{
					Path:            validPath,
					ShareInstanceID: shareID2,
					IsAdminOnly:     false,
					ID:              shareExportLocationID2,
					Preferred:       false,
				},
				{
					Path:            preferredPath,
					ShareInstanceID: shareID2,
					IsAdminOnly:     false,
					ID:              shareExportLocationID3,
					Preferred:       true,
				},
			},
			want: shares.ExportLocation{
				Path:            preferredPath,
				ShareInstanceID: shareID2,
				IsAdminOnly:     false,
				ID:              shareExportLocationID3,
				Preferred:       true,
			},
		},
		{
			testCaseName: "Match first not-preferred location that matches shareID:",
			locs: []shares.ExportLocation{
				{
					Path:            validPath,
					ShareInstanceID: shareID2,
					IsAdminOnly:     false,
					ID:              shareExportLocationID2,
					Preferred:       false,
				},
				{
					Path:            preferredPath,
					ShareInstanceID: shareID2,
					IsAdminOnly:     false,
					ID:              shareExportLocationID3,
					Preferred:       false,
				},
			},
			want: shares.ExportLocation{
				Path:            validPath,
				ShareInstanceID: shareID2,
				IsAdminOnly:     false,
				ID:              shareExportLocationID2,
				Preferred:       false,
			},
		},
	}

	for _, tt := range tests {
		if got, err := chooseExportLocation(tt.locs); err != nil {
			t.Errorf("%q chooseExportLocation(%v) = (%v, %q) want (%v, nil)", tt.testCaseName, tt.locs, got, err.Error(), tt.want)
		} else if !reflect.DeepEqual(tt.want, got) {
			t.Errorf("%q chooseExportLocation(%v) = (%v, nil) want (%v, nil)", tt.testCaseName, tt.locs, got, tt.want)
		}
	}
}

func TestCreateShare(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	volOptions := controller.VolumeOptions{PVC: &v1.PersistentVolumeClaim{}}
	volOptions.PVC.Name = "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264"
	volOptions.PVC.Namespace = "default"
	volOptions.PVC.Spec.Resources.Requests = make(v1.ResourceList)
	volOptions.PVC.Spec.Resources.Requests[v1.ResourceStorage] = *resource.NewQuantity(1, resource.BinarySI)

	shareOptions := shareoptions.ShareOptions{ShareName: volOptions.PVC.Name}
	shareOptions.Protocol = "NFS"
	shareOptions.Backend = "nfs"
	shareOptions.Type = "default"

	var createRequest = `{
		"share": {
			"name": "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
			"metadata": {
		      "kubernetes.io/created-for/pv/name": "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
		      "kubernetes.io/created-for/pvc/name": "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
		      "kubernetes.io/created-for/pvc/namespace": "default"
		    },
			"size": 1,
			"share_type": "default",
			"share_proto": "NFS"
		}
	}`

	var createResponse = `{
		"share": {
			"name": "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
			"share_proto": "NFS",
			"size": 1,
			"status": null,
			"share_server_id": null,
			"project_id": "16e1ab15c35a457e9c2b2aa189f544e1",
			"share_type": "25747776-08e5-494f-ab40-a64b9d20d8f7",
			"share_type_name": "default",
			"availability_zone": null,
			"export_location": null,
			"links": [
				{
					"href": "http://172.18.198.54:8786/v1/16e1ab15c35a457e9c2b2aa189f544e1/shares/011d21e2-fbc3-4e4a-9993-9ea223f73264",
					"rel": "self"
				},
				{
					"href": "http://172.18.198.54:8786/16e1ab15c35a457e9c2b2aa189f544e1/shares/011d21e2-fbc3-4e4a-9993-9ea223f73264",
					"rel": "bookmark"
				}
			],
			"share_network_id": null,
			"export_locations": [],
			"host": null,
			"access_rules_status": "active",
			"has_replicas": false,
			"replication_type": null,
			"task_state": null,
			"snapshot_support": false,
			"consistency_group_id": "9397c191-8427-4661-a2e8-b23820dc01d4",
			"source_cgsnapshot_member_id": null,
			"volume_type": "default",
			"snapshot_id": null,
			"is_public": false,
			"id": "011d21e2-fbc3-4e4a-9993-9ea223f73264",
			"description": "share test"
		}
	}`

	th.Mux.HandleFunc("/shares", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "X-Auth-Token", fakeclient.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json")
		th.TestJSONRequest(t, r, createRequest)
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, createResponse)
	})

	share, err := createShare(&volOptions, &shareOptions, fakeclient.ServiceClient())

	if err != nil {
		t.Fatalf("failed to create share: %v", err)
	}

	eq := reflect.DeepEqual(share, &shares.Share{
		Description:        "share test",
		ID:                 "011d21e2-fbc3-4e4a-9993-9ea223f73264",
		Name:               "pvc-011d21e2-fbc3-4e4a-9993-9ea223f73264",
		ProjectID:          "16e1ab15c35a457e9c2b2aa189f544e1",
		ShareProto:         "NFS",
		ShareType:          "25747776-08e5-494f-ab40-a64b9d20d8f7",
		ShareTypeName:      "default",
		Size:               1,
		VolumeType:         "default",
		ConsistencyGroupID: "9397c191-8427-4661-a2e8-b23820dc01d4",
		Links: []map[string]string{
			{
				"href": "http://172.18.198.54:8786/v1/16e1ab15c35a457e9c2b2aa189f544e1/shares/011d21e2-fbc3-4e4a-9993-9ea223f73264",
				"rel":  "self",
			},
			{
				"href": "http://172.18.198.54:8786/16e1ab15c35a457e9c2b2aa189f544e1/shares/011d21e2-fbc3-4e4a-9993-9ea223f73264",
				"rel":  "bookmark",
			},
		},
	})

	if !eq {
		t.Error("unexpected Share contents")
	}
}
