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
	m := &staticMetadata{nodeID: "my-node", nodeAZ: "az1"}

	id, err := m.GetInstanceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "my-node" {
		t.Errorf("expected node ID %q, got %q", "my-node", id)
	}

	az, err := m.GetAvailabilityZone()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az != "az1" {
		t.Errorf("expected AZ %q, got %q", "az1", az)
	}
}

func TestOverrideMetadataNodeIDSet(t *testing.T) {
	fallback := &fakeMetadataProvider{instanceID: "meta-id", availabilityZone: "meta-az"}
	m := &overrideMetadata{nodeID: "flag-id", fallback: fallback}

	id, err := m.GetInstanceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "flag-id" {
		t.Errorf("expected node ID %q, got %q", "flag-id", id)
	}

	az, err := m.GetAvailabilityZone()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az != "meta-az" {
		t.Errorf("expected AZ %q, got %q", "meta-az", az)
	}
}

func TestOverrideMetadataNodeAZSet(t *testing.T) {
	fallback := &fakeMetadataProvider{instanceID: "meta-id", availabilityZone: "meta-az"}
	m := &overrideMetadata{nodeAZ: "flag-az", fallback: fallback}

	id, err := m.GetInstanceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "meta-id" {
		t.Errorf("expected node ID %q, got %q", "meta-id", id)
	}

	az, err := m.GetAvailabilityZone()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az != "flag-az" {
		t.Errorf("expected AZ %q, got %q", "flag-az", az)
	}
}

func TestOverrideMetadataBothSet(t *testing.T) {
	fallback := &fakeMetadataProvider{err: errors.New("should not be called")}
	m := &overrideMetadata{nodeID: "flag-id", nodeAZ: "flag-az", fallback: fallback}

	id, err := m.GetInstanceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "flag-id" {
		t.Errorf("expected node ID %q, got %q", "flag-id", id)
	}

	az, err := m.GetAvailabilityZone()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az != "flag-az" {
		t.Errorf("expected AZ %q, got %q", "flag-az", az)
	}
}

func TestOverrideMetadataNeitherSet(t *testing.T) {
	fallback := &fakeMetadataProvider{instanceID: "meta-id", availabilityZone: "meta-az"}
	m := &overrideMetadata{fallback: fallback}

	id, err := m.GetInstanceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "meta-id" {
		t.Errorf("expected node ID %q, got %q", "meta-id", id)
	}

	az, err := m.GetAvailabilityZone()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if az != "meta-az" {
		t.Errorf("expected AZ %q, got %q", "meta-az", az)
	}
}
