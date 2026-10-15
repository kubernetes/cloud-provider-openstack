/*
Copyright 2026 The Kubernetes Authors.

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

package shareadapters

import (
	"context"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/messages"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/sharetypes"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/snapshots"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
)

var _ manilaclient.Interface = &mockManilaClient{}

type mockManilaClient struct {
	callCount    int
	accessRights [][]shares.AccessRight
	revokeCalled bool
	granted      []shares.GrantAccessOpts
	revoked      []string
}

func (m *mockManilaClient) GetMicroversion() string  { return "" }
func (m *mockManilaClient) SetMicroversion(_ string) {}
func (m *mockManilaClient) GetShareByID(_ context.Context, _ string) (*shares.Share, error) {
	return nil, nil
}
func (m *mockManilaClient) GetShareByName(_ context.Context, _ string) (*shares.Share, error) {
	return nil, nil
}
func (m *mockManilaClient) CreateShare(_ context.Context, _ shares.CreateOptsBuilder) (*shares.Share, error) {
	return nil, nil
}
func (m *mockManilaClient) DeleteShare(_ context.Context, _ string) error { return nil }
func (m *mockManilaClient) ExtendShare(_ context.Context, _ string, _ shares.ExtendOptsBuilder) error {
	return nil
}
func (m *mockManilaClient) GetExportLocations(_ context.Context, _ string) ([]shares.ExportLocation, error) {
	return nil, nil
}
func (m *mockManilaClient) SetShareMetadata(_ context.Context, _ string, _ shares.SetMetadataOptsBuilder) (map[string]string, error) {
	return nil, nil
}
func (m *mockManilaClient) GrantAccess(_ context.Context, _ string, opts shares.GrantAccessOptsBuilder) (*shares.AccessRight, error) {
	grantOpts := opts.(shares.GrantAccessOpts)
	m.granted = append(m.granted, grantOpts)
	return &shares.AccessRight{ID: "new-" + grantOpts.AccessTo}, nil
}
func (m *mockManilaClient) GetSnapshotByID(_ context.Context, _ string) (*snapshots.Snapshot, error) {
	return nil, nil
}
func (m *mockManilaClient) GetSnapshotByName(_ context.Context, _ string) (*snapshots.Snapshot, error) {
	return nil, nil
}
func (m *mockManilaClient) CreateSnapshot(_ context.Context, _ snapshots.CreateOptsBuilder) (*snapshots.Snapshot, error) {
	return nil, nil
}
func (m *mockManilaClient) DeleteSnapshot(_ context.Context, _ string) error { return nil }
func (m *mockManilaClient) GetExtraSpecs(_ context.Context, _ string) (sharetypes.ExtraSpecs, error) {
	return nil, nil
}
func (m *mockManilaClient) GetShareTypes(_ context.Context) ([]sharetypes.ShareType, error) {
	return nil, nil
}
func (m *mockManilaClient) GetShareTypeIDFromName(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockManilaClient) GetUserMessages(_ context.Context, _ messages.ListOptsBuilder) ([]messages.Message, error) {
	return nil, nil
}

func (m *mockManilaClient) GetAccessRights(_ context.Context, _ string) ([]shares.AccessRight, error) {
	idx := m.callCount
	if idx >= len(m.accessRights) {
		idx = len(m.accessRights) - 1
	}
	m.callCount++
	return m.accessRights[idx], nil
}

func (m *mockManilaClient) RevokeAccess(_ context.Context, _ string, accessID string) error {
	m.revokeCalled = true
	m.revoked = append(m.revoked, accessID)
	return nil
}

func TestWaitForAccessRuleAlreadyActive(t *testing.T) {
	mock := &mockManilaClient{
		accessRights: [][]shares.AccessRight{
			{
				{ID: "ar-1", ShareID: "share-1", State: "active", AccessKey: "key123"},
			},
		},
	}

	result, err := waitForAccessRuleActive(context.Background(), mock, "share-1", "ar-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "ar-1" {
		t.Errorf("expected ID ar-1, got %s", result.ID)
	}
	if result.AccessKey != "key123" {
		t.Errorf("expected AccessKey key123, got %s", result.AccessKey)
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 call, got %d", mock.callCount)
	}
}

func TestWaitForAccessRuleTransientToActive(t *testing.T) {
	mock := &mockManilaClient{
		accessRights: [][]shares.AccessRight{
			{
				{ID: "ar-1", ShareID: "share-1", State: "queued_to_apply"},
			},
			{
				{ID: "ar-1", ShareID: "share-1", State: "applying"},
			},
			{
				{ID: "ar-1", ShareID: "share-1", State: "active", AccessKey: "key456"},
			},
		},
	}

	result, err := waitForAccessRuleActive(context.Background(), mock, "share-1", "ar-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != "active" {
		t.Errorf("expected state active, got %s", result.State)
	}
	if result.AccessKey != "key456" {
		t.Errorf("expected AccessKey key456, got %s", result.AccessKey)
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", mock.callCount)
	}
}

func TestWaitForAccessRuleErrorState(t *testing.T) {
	mock := &mockManilaClient{
		accessRights: [][]shares.AccessRight{
			{
				{ID: "ar-1", ShareID: "share-1", State: "queued_to_apply"},
			},
			{
				{ID: "ar-1", ShareID: "share-1", State: "error"},
			},
		},
	}

	_, err := waitForAccessRuleActive(context.Background(), mock, "share-1", "ar-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "error state") {
		t.Errorf("expected 'error state' in error message, got: %s", err.Error())
	}
	if !mock.revokeCalled {
		t.Error("expected RevokeAccess to be called")
	}
}

func TestWaitForAccessRuleNotFound(t *testing.T) {
	mock := &mockManilaClient{
		accessRights: [][]shares.AccessRight{
			{
				{ID: "ar-other", ShareID: "share-1", State: "active"},
			},
		},
	}

	_, err := waitForAccessRuleActive(context.Background(), mock, "share-1", "ar-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error message, got: %s", err.Error())
	}
}

func TestNFSReconcileAccesses(t *testing.T) {
	mock := &mockManilaClient{
		accessRights: [][]shares.AccessRight{
			{
				{ID: "keep", AccessType: "ip", AccessLevel: "rw", AccessTo: "10.0.0.0/24", State: "active"},
				{ID: "remove", AccessType: "ip", AccessLevel: "rw", AccessTo: "192.0.2.0/24", State: "active"},
				{ID: "preserve-ro", AccessType: "ip", AccessLevel: "ro", AccessTo: "198.51.100.0/24", State: "active"},
				{ID: "preserve-cephx", AccessType: "cephx", AccessLevel: "rw", AccessTo: "client", State: "active"},
			},
			{
				{ID: "new-203.0.113.10", AccessType: "ip", AccessLevel: "rw", AccessTo: "203.0.113.10", State: "active"},
			},
		},
	}

	err := (NFS{}).ReconcileAccesses(context.Background(), mock, "share-1", []string{"10.0.0.0/24", "203.0.113.10"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.granted) != 1 || mock.granted[0].AccessTo != "203.0.113.10" {
		t.Fatalf("unexpected grants: %#v", mock.granted)
	}
	if len(mock.revoked) != 1 || mock.revoked[0] != "remove" {
		t.Fatalf("unexpected revocations: %#v", mock.revoked)
	}
}

func TestNFSReconcileAccessesIsIdempotent(t *testing.T) {
	mock := &mockManilaClient{
		accessRights: [][]shares.AccessRight{{
			{ID: "keep", AccessType: "ip", AccessLevel: "rw", AccessTo: "10.0.0.0/24", State: "active"},
		}},
	}

	err := (NFS{}).ReconcileAccesses(context.Background(), mock, "share-1", []string{"10.0.0.0/24"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.granted) != 0 || len(mock.revoked) != 0 {
		t.Fatalf("expected no changes, got grants %#v and revocations %#v", mock.granted, mock.revoked)
	}
}
