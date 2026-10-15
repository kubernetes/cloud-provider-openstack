/*
Copyright 2021 The Kubernetes Authors.
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
	"context"
	"fmt"
	"reflect"
	"testing"

	csispec "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/csi"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
)

func TestParseNFSShareClients(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    []string
		wantErr bool
	}{
		{name: "addresses and CIDRs", value: " 10.0.0.1, 192.0.2.0/24,2001:db8::1 ", want: []string{"10.0.0.1", "192.0.2.0/24", "2001:db8::1"}},
		{name: "duplicates", value: "10.0.0.1,10.0.0.1", want: []string{"10.0.0.1"}},
		{name: "empty revokes all", value: "", want: []string{}},
		{name: "invalid", value: "not-an-address", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseNFSShareClients(tc.value)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

type modifyVolumeTestClient struct {
	manilaclient.Interface
	share        *shares.Share
	getCalls     int
	granted      []shares.GrantAccessOpts
	revoked      []string
	accessRights [][]shares.AccessRight
}

func (c *modifyVolumeTestClient) GetShareByID(context.Context, string) (*shares.Share, error) {
	return c.share, nil
}

func (c *modifyVolumeTestClient) GetAccessRights(context.Context, string) ([]shares.AccessRight, error) {
	idx := c.getCalls
	if idx >= len(c.accessRights) {
		idx = len(c.accessRights) - 1
	}
	c.getCalls++
	return c.accessRights[idx], nil
}

func (c *modifyVolumeTestClient) GrantAccess(_ context.Context, _ string, opts shares.GrantAccessOptsBuilder) (*shares.AccessRight, error) {
	grantOpts := opts.(shares.GrantAccessOpts)
	c.granted = append(c.granted, grantOpts)
	return &shares.AccessRight{ID: "new-rule"}, nil
}

func (c *modifyVolumeTestClient) RevokeAccess(_ context.Context, _ string, accessID string) error {
	c.revoked = append(c.revoked, accessID)
	return nil
}

type modifyVolumeTestBuilder struct {
	client manilaclient.Interface
}

func (b *modifyVolumeTestBuilder) New(context.Context, *client.AuthOpts) (manilaclient.Interface, error) {
	return b.client, nil
}

func TestControllerModifyVolumeReconcilesNFSAccessRules(t *testing.T) {
	manilaClient := &modifyVolumeTestClient{
		share: &shares.Share{ID: "share-1", Name: "pvc-share", ShareProto: "NFS"},
		accessRights: [][]shares.AccessRight{
			{{ID: "old-rule", AccessType: "ip", AccessLevel: "rw", AccessTo: "192.0.2.0/24", State: "active"}},
			{{ID: "new-rule", AccessType: "ip", AccessLevel: "rw", AccessTo: "10.0.0.0/24", State: "active"}},
		},
	}
	server := &controllerServer{d: &Driver{
		shareProto:          "NFS",
		manilaClientBuilder: &modifyVolumeTestBuilder{client: manilaClient},
	}}

	_, err := server.ControllerModifyVolume(context.Background(), &csispec.ControllerModifyVolumeRequest{
		VolumeId:          "share-1",
		Secrets:           map[string]string{"os-region": "RegionOne"},
		MutableParameters: map[string]string{"nfs-shareClient": "10.0.0.0/24"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manilaClient.granted) != 1 || manilaClient.granted[0].AccessTo != "10.0.0.0/24" {
		t.Fatalf("unexpected grants: %#v", manilaClient.granted)
	}
	if !reflect.DeepEqual(manilaClient.revoked, []string{"old-rule"}) {
		t.Fatalf("unexpected revocations: %#v", manilaClient.revoked)
	}
}

func TestControllerModifyVolumeRejectsUnsupportedParameter(t *testing.T) {
	server := &controllerServer{d: &Driver{shareProto: "NFS"}}
	_, err := server.ControllerModifyVolume(context.Background(), &csispec.ControllerModifyVolumeRequest{
		VolumeId:          "share-1",
		Secrets:           map[string]string{"os-region": "RegionOne"},
		MutableParameters: map[string]string{"unsupported": "value"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}

func TestPrepareShareMetadata(t *testing.T) {
	ts := []struct {
		allVolumeParams     map[string]string
		appendShareMetadata string
		cluster             string
		expectedResult      map[string]string
		expectedError       bool
	}{
		{
			// Empty metadata and cluster
			allVolumeParams:     map[string]string{},
			appendShareMetadata: "",
			cluster:             "",
			expectedResult:      nil,
			expectedError:       false,
		},
		{
			// Existing metadata and empty cluster
			allVolumeParams:     map[string]string{"appendShareMetadata": `{"keyA": "valueA", "keyB": "valueB"}`},
			appendShareMetadata: `{"keyA": "valueA", "keyB": "valueB"}`,
			cluster:             "",
			expectedResult:      map[string]string{"keyA": "valueA", "keyB": "valueB"},
			expectedError:       false,
		},
		{
			// Just cluster and no metadata
			appendShareMetadata: "",
			cluster:             "MyCluster",
			expectedResult:      map[string]string{clusterMetadataKey: "MyCluster"},
			expectedError:       false,
		},
		{
			// Both metadata and cluster
			allVolumeParams:     map[string]string{"appendShareMetadata": `{"keyA": "valueA", "keyB": "valueB"}`},
			appendShareMetadata: "{\"keyA\": \"valueA\", \"keyB\": \"valueB\"}",
			cluster:             "MyCluster",
			expectedResult:      map[string]string{"keyA": "valueA", "keyB": "valueB", clusterMetadataKey: "MyCluster"},
			expectedError:       false,
		},
		{
			// Overwrite cluster
			allVolumeParams:     map[string]string{"appendShareMetadata": "{\"keyA\": \"valueA\", \"" + clusterMetadataKey + "\": \"SomeValue\"}"},
			appendShareMetadata: "{\"keyA\": \"valueA\", \"" + clusterMetadataKey + "\": \"SomeValue\"}",
			cluster:             "MyCluster",
			expectedResult:      map[string]string{"keyA": "valueA", clusterMetadataKey: "SomeValue"},
			expectedError:       false,
		},
		{
			// Incorrect metadata
			allVolumeParams:     map[string]string{"appendShareMetadata": "INVALID"},
			appendShareMetadata: "INVALID",
			cluster:             "MyCluster",
			expectedResult:      nil,
			expectedError:       true,
		},
		{
			// csi-provisioner PV/PVC metadata
			allVolumeParams: map[string]string{
				csi.PvcNameKey:      "pvc-name",
				csi.PvcNamespaceKey: "pvc-namespace",
				csi.PvNameKey:       "pv-name",
			},
			cluster: "",
			expectedResult: map[string]string{
				csi.PvcNameKey:      "pvc-name",
				csi.PvcNamespaceKey: "pvc-namespace",
				csi.PvNameKey:       "pv-name",
			},
			appendShareMetadata: "",
			expectedError:       false,
		},
		{
			// csi-provisioner PV/PVC metadata with conflicting appendShareMetadata
			allVolumeParams: map[string]string{
				csi.PvcNameKey:        "pvc-name",
				csi.PvcNamespaceKey:   "pvc-namespace",
				csi.PvNameKey:         "pv-name",
				"appendShareMetadata": `{"` + csi.PvcNameKey + `": "SomeValue", "keyX": "valueX"}`,
			},
			appendShareMetadata: `{"` + csi.PvcNameKey + `": "SomeValue", "keyX": "valueX"}`,
			cluster:             "",
			expectedResult: map[string]string{
				csi.PvcNameKey:      "pvc-name",
				csi.PvcNamespaceKey: "pvc-namespace",
				csi.PvNameKey:       "pv-name",
				"keyX":              "valueX",
			},
			expectedError: false,
		},
	}

	for i := range ts {
		result, err := prepareShareMetadata(ts[i].appendShareMetadata, ts[i].cluster, ts[i].allVolumeParams)

		if err != nil && !ts[i].expectedError {
			t.Errorf("test %d: unexpected error: %v", i, err)
		}

		if fmt.Sprint(result) != fmt.Sprint(ts[i].expectedResult) {
			t.Errorf("test %d: returned an incorrect result: got %#v, expected %#v", i, result, ts[i].expectedResult)
		}
	}
}
