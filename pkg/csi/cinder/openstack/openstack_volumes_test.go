/*
Copyright 2024 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
)

const (
	testVolumeID      = "vol-123"
	testInstanceID    = "instance-456"
	testOtherInstance = "instance-789"
)

// fakeVolumeResponse returns a JSON response body for a Cinder volume GET.
func fakeVolumeResponse(id, status string, multiattach bool, attachments []volumes.Attachment) string {
	return fakeVolumeResponseWithMigration(id, status, multiattach, attachments, "")
}

// fakeVolumeResponseWithMigration returns a JSON response body for a Cinder volume GET
// including the migration_status field.
func fakeVolumeResponseWithMigration(id, status string, multiattach bool, attachments []volumes.Attachment, migrationStatus string) string {
	atts := "[]"
	if len(attachments) > 0 {
		b, _ := json.Marshal(attachments)
		atts = string(b)
	}
	migField := "null"
	if migrationStatus != "" {
		migField = fmt.Sprintf("%q", migrationStatus)
	}
	return fmt.Sprintf(`{
		"volume": {
			"id": %q,
			"status": %q,
			"multiattach": %t,
			"attachments": %s,
			"migration_status": %s,
			"size": 1,
			"availability_zone": "nova"
		}
	}`, id, status, multiattach, atts, migField)
}

// fakeAttachResponse returns a JSON response for a successful Nova volume attach.
func fakeAttachResponse(serverID, volumeID, device string) string {
	return fmt.Sprintf(`{
		"volumeAttachment": {
			"serverId": %q,
			"volumeId": %q,
			"device": %q
		}
	}`, serverID, volumeID, device)
}

// newFakeOpenStack creates an OpenStack instance backed by httptest servers.
// cinderHandler handles /volumes/{id} requests.
// novaHandler handles /servers/{id}/os-volume_attachments requests.
func newFakeOpenStack(cinderHandler http.HandlerFunc, novaHandler http.HandlerFunc) (*OpenStack, *httptest.Server, *httptest.Server) {
	cinderServer := httptest.NewServer(cinderHandler)
	novaServer := httptest.NewServer(novaHandler)

	novaEndpoint := novaServer.URL + "/"

	providerClient := &gophercloud.ProviderClient{
		// EndpointLocator is required for openstack.NewComputeV2 used in multiattach path
		EndpointLocator: func(opts gophercloud.EndpointOpts) (string, error) {
			return novaEndpoint, nil
		},
	}

	blockstorageClient := &gophercloud.ServiceClient{
		ProviderClient: &gophercloud.ProviderClient{},
		Endpoint:       cinderServer.URL + "/",
	}

	computeClient := &gophercloud.ServiceClient{
		ProviderClient: providerClient,
		Endpoint:       novaEndpoint,
	}

	os := &OpenStack{
		compute:      computeClient,
		blockstorage: blockstorageClient,
		epOpts:       gophercloud.EndpointOpts{},
	}

	return os, cinderServer, novaServer
}

func TestAttachVolume_AvailableStatus(t *testing.T) {
	volumeID := testVolumeID
	instanceID := testInstanceID

	cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeVolumeResponse(volumeID, VolumeAvailableStatus, false, nil))
	})

	novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeAttachResponse(instanceID, volumeID, "/dev/vdb"))
	})

	os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
	defer cinderSrv.Close()
	defer novaSrv.Close()

	result, err := os.AttachVolume(context.Background(), instanceID, volumeID)
	if err != nil {
		t.Fatalf("expected no error for available volume, got: %v", err)
	}
	if result != volumeID {
		t.Errorf("expected volume ID %q, got %q", volumeID, result)
	}
}

func TestAttachVolume_NonAvailableStatus_SingleAttach(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"creating", "creating"},
		{"error", "error"},
		{"in-use", VolumeInUseStatus},
		{"detaching", VolumeDetachingStatus},
		{"attaching", "attaching"},
		{"downloading", "downloading"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			volumeID := testVolumeID
			instanceID := testInstanceID

			cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, fakeVolumeResponse(volumeID, tc.status, false, nil))
			})

			novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("Nova attach should not be called for non-available volume")
				w.WriteHeader(http.StatusInternalServerError)
			})

			os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
			defer cinderSrv.Close()
			defer novaSrv.Close()

			_, err := os.AttachVolume(context.Background(), instanceID, volumeID)
			if err == nil {
				t.Fatalf("expected error for volume in %q status, got nil", tc.status)
			}

			expectedMsg := fmt.Sprintf("volume %s is in %s status, volume must be available", volumeID, tc.status)
			if err.Error() != expectedMsg {
				t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
			}
		})
	}
}

func TestAttachVolume_MultiAttach_AvailableStatus(t *testing.T) {
	volumeID := testVolumeID
	instanceID := testInstanceID

	cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeVolumeResponse(volumeID, VolumeAvailableStatus, true, nil))
	})

	novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeAttachResponse(instanceID, volumeID, "/dev/vdb"))
	})

	os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
	defer cinderSrv.Close()
	defer novaSrv.Close()

	result, err := os.AttachVolume(context.Background(), instanceID, volumeID)
	if err != nil {
		t.Fatalf("expected no error for multiattach available volume, got: %v", err)
	}
	if result != volumeID {
		t.Errorf("expected volume ID %q, got %q", volumeID, result)
	}
}

func TestAttachVolume_MultiAttach_InUseStatus(t *testing.T) {
	volumeID := testVolumeID
	instanceID := testInstanceID
	otherInstanceID := testOtherInstance

	// Volume is already attached to another instance
	attachments := []volumes.Attachment{
		{ServerID: otherInstanceID, VolumeID: volumeID},
	}

	cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeVolumeResponse(volumeID, VolumeInUseStatus, true, attachments))
	})

	novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeAttachResponse(instanceID, volumeID, "/dev/vdc"))
	})

	os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
	defer cinderSrv.Close()
	defer novaSrv.Close()

	result, err := os.AttachVolume(context.Background(), instanceID, volumeID)
	if err != nil {
		t.Fatalf("expected no error for multiattach in-use volume, got: %v", err)
	}
	if result != volumeID {
		t.Errorf("expected volume ID %q, got %q", volumeID, result)
	}
}

func TestAttachVolume_MultiAttach_InvalidStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"creating", "creating"},
		{"error", "error"},
		{"detaching", VolumeDetachingStatus},
		{"attaching", "attaching"},
		{"downloading", "downloading"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			volumeID := testVolumeID
			instanceID := testInstanceID

			cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, fakeVolumeResponse(volumeID, tc.status, true, nil))
			})

			novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("Nova attach should not be called for volume in invalid status")
				w.WriteHeader(http.StatusInternalServerError)
			})

			os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
			defer cinderSrv.Close()
			defer novaSrv.Close()

			_, err := os.AttachVolume(context.Background(), instanceID, volumeID)
			if err == nil {
				t.Fatalf("expected error for multiattach volume in %q status, got nil", tc.status)
			}

			expectedMsg := fmt.Sprintf("volume %s is in %s status, volume must be available or in-use for multi-attach capable volumes", volumeID, tc.status)
			if err.Error() != expectedMsg {
				t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
			}
		})
	}
}

func TestAttachVolume_AlreadyAttachedToSameInstance(t *testing.T) {
	volumeID := testVolumeID
	instanceID := testInstanceID

	// Volume is already attached to the same instance we're trying to attach to
	attachments := []volumes.Attachment{
		{ServerID: instanceID, VolumeID: volumeID},
	}

	cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Status is in-use because it's already attached
		fmt.Fprint(w, fakeVolumeResponse(volumeID, VolumeInUseStatus, false, attachments))
	})

	novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Nova attach should not be called when already attached to same instance")
		w.WriteHeader(http.StatusInternalServerError)
	})

	os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
	defer cinderSrv.Close()
	defer novaSrv.Close()

	// Should return early with the volume ID (idempotent)
	result, err := os.AttachVolume(context.Background(), instanceID, volumeID)
	if err != nil {
		t.Fatalf("expected no error for already-attached volume, got: %v", err)
	}
	if result != volumeID {
		t.Errorf("expected volume ID %q, got %q", volumeID, result)
	}
}

func TestAttachVolume_MigrationStatus_InProgress(t *testing.T) {
	// Per OpenStack docs, "starting", "migrating", and "completing" all indicate
	// a migration is in progress and attach should be rejected.
	tests := []struct {
		name            string
		migrationStatus string
	}{
		{"starting", "starting"},
		{"migrating", "migrating"},
		{"completing", "completing"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			volumeID := testVolumeID
			instanceID := testInstanceID

			cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, fakeVolumeResponseWithMigration(volumeID, VolumeAvailableStatus, false, nil, tc.migrationStatus))
			})

			novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("Nova attach should not be called for volume with active migration")
				w.WriteHeader(http.StatusInternalServerError)
			})

			os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
			defer cinderSrv.Close()
			defer novaSrv.Close()

			_, err := os.AttachVolume(context.Background(), instanceID, volumeID)
			if err == nil {
				t.Fatalf("expected error for volume with migration_status %q, got nil", tc.migrationStatus)
			}

			expectedMsg := fmt.Sprintf("volume %s has migration_status %q, volume must not be migrating before attach", volumeID, tc.migrationStatus)
			if err.Error() != expectedMsg {
				t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
			}
		})
	}
}

func TestAttachVolume_MigrationStatus_Null(t *testing.T) {
	volumeID := testVolumeID
	instanceID := testInstanceID

	cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// migration_status is null (normal case)
		fmt.Fprint(w, fakeVolumeResponseWithMigration(volumeID, VolumeAvailableStatus, false, nil, ""))
	})

	novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeAttachResponse(instanceID, volumeID, "/dev/vdb"))
	})

	os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
	defer cinderSrv.Close()
	defer novaSrv.Close()

	result, err := os.AttachVolume(context.Background(), instanceID, volumeID)
	if err != nil {
		t.Fatalf("expected no error for volume with null migration_status, got: %v", err)
	}
	if result != volumeID {
		t.Errorf("expected volume ID %q, got %q", volumeID, result)
	}
}

func TestAttachVolume_MigrationStatus_Success(t *testing.T) {
	volumeID := testVolumeID
	instanceID := testInstanceID

	cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// migration_status is "success" — migration complete, attach should proceed
		fmt.Fprint(w, fakeVolumeResponseWithMigration(volumeID, VolumeAvailableStatus, false, nil, "success"))
	})

	novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeAttachResponse(instanceID, volumeID, "/dev/vdb"))
	})

	os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
	defer cinderSrv.Close()
	defer novaSrv.Close()

	result, err := os.AttachVolume(context.Background(), instanceID, volumeID)
	if err != nil {
		t.Fatalf("expected no error for volume with success migration_status, got: %v", err)
	}
	if result != volumeID {
		t.Errorf("expected volume ID %q, got %q", volumeID, result)
	}
}

func TestAttachVolume_MigrationStatus_Error(t *testing.T) {
	volumeID := testVolumeID
	instanceID := testInstanceID

	cinderHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// migration_status is "error" — migration failed, but volume is available
		fmt.Fprint(w, fakeVolumeResponseWithMigration(volumeID, VolumeAvailableStatus, false, nil, "error"))
	})

	novaHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, fakeAttachResponse(instanceID, volumeID, "/dev/vdb"))
	})

	os, cinderSrv, novaSrv := newFakeOpenStack(cinderHandler, novaHandler)
	defer cinderSrv.Close()
	defer novaSrv.Close()

	result, err := os.AttachVolume(context.Background(), instanceID, volumeID)
	if err != nil {
		t.Fatalf("expected no error for volume with error migration_status, got: %v", err)
	}
	if result != volumeID {
		t.Errorf("expected volume ID %q, got %q", volumeID, result)
	}
}
