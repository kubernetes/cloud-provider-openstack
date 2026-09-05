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

package manila

import (
	"errors"
	"testing"
)

type fakeMetadataProvider struct {
	instanceID       string
	availabilityZone string
	err              error
}

func (f *fakeMetadataProvider) GetInstanceID() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.instanceID, nil
}

func (f *fakeMetadataProvider) GetAvailabilityZone() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.availabilityZone, nil
}

func TestStaticMetadata(t *testing.T) {
	tests := []struct {
		name   string
		nodeID string
		nodeAZ string
		wantID string
		wantAZ string
	}{
		{"both set", "my-node", "az1", "my-node", "az1"},
		{"empty values", "", "", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &staticMetadata{nodeID: tc.nodeID, nodeAZ: tc.nodeAZ}

			id, err := m.GetInstanceID()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tc.wantID {
				t.Errorf("expected node ID %q, got %q", tc.wantID, id)
			}

			az, err := m.GetAvailabilityZone()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if az != tc.wantAZ {
				t.Errorf("expected AZ %q, got %q", tc.wantAZ, az)
			}
		})
	}
}

func TestOverrideMetadata(t *testing.T) {
	tests := []struct {
		name     string
		nodeID   string
		nodeAZ   string
		fallback *fakeMetadataProvider
		wantID   string
		wantAZ   string
	}{
		{
			"nodeID set, AZ from fallback",
			"flag-id", "",
			&fakeMetadataProvider{instanceID: "meta-id", availabilityZone: "meta-az"},
			"flag-id", "meta-az",
		},
		{
			"nodeAZ set, ID from fallback",
			"", "flag-az",
			&fakeMetadataProvider{instanceID: "meta-id", availabilityZone: "meta-az"},
			"meta-id", "flag-az",
		},
		{
			"both set, fallback not called",
			"flag-id", "flag-az",
			&fakeMetadataProvider{err: errors.New("should not be called")},
			"flag-id", "flag-az",
		},
		{
			"neither set, both from fallback",
			"", "",
			&fakeMetadataProvider{instanceID: "meta-id", availabilityZone: "meta-az"},
			"meta-id", "meta-az",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &overrideMetadata{nodeID: tc.nodeID, nodeAZ: tc.nodeAZ, fallback: tc.fallback}

			id, err := m.GetInstanceID()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tc.wantID {
				t.Errorf("expected node ID %q, got %q", tc.wantID, id)
			}

			az, err := m.GetAvailabilityZone()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if az != tc.wantAZ {
				t.Errorf("expected AZ %q, got %q", tc.wantAZ, az)
			}
		})
	}
}
