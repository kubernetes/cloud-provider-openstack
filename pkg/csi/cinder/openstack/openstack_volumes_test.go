/*
Copyright 2017 The Kubernetes Authors.

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

package openstack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAttachVolumeRejectsNonMultiattachVolumeAttachedToAnotherInstance(t *testing.T) {
	const (
		volumeID       = "volume-id"
		oldInstanceID  = "old-instance-id"
		newInstanceID  = "new-instance-id"
		attachmentID   = "attachment-id"
		volumeEndpoint = "/volumes/" + volumeID
	)

	computeAttachCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, volumeEndpoint):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"volume": {
					"id": "` + volumeID + `",
					"status": "in-use",
					"multiattach": false,
					"attachments": [{
						"attachment_id": "` + attachmentID + `",
						"id": "` + volumeID + `",
						"server_id": "` + oldInstanceID + `",
						"volume_id": "` + volumeID + `"
					}]
				}
			}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/os-volume_attachments"):
			computeAttachCalled = true
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{},
		Endpoint:       server.URL + "/",
	}
	os := &OpenStack{
		blockstorage: client,
		compute:      client,
	}

	_, err := os.AttachVolume(context.Background(), newInstanceID, volumeID)

	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), oldInstanceID)
	assert.False(t, computeAttachCalled)
}
