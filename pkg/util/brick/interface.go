/*
Copyright 2025 The Kubernetes Authors.

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

// Package brick provides a client interface for communicating with the
// os-brick sidecar over gRPC. The sidecar handles low-level volume
// attach/detach operations (iSCSI, FC, etc.) for nodes using direct
// attachment mode (typically bare-metal nodes).
package brick

import "context"

// ConnectorProperties describes the host-side initiator information
// that Cinder needs to set up a volume connection (e.g. iSCSI IQN,
// FC WWPNs).
type ConnectorProperties struct {
	Initiator string
	Wwpns     []string
	Host      string
	Multipath bool
	Extras    map[string]string

	// RawJSON is the complete connector properties dict from os-brick,
	// serialized as JSON. It preserves original Python types (bools,
	// lists, ints) that would be lost through the typed fields above
	// and the string-valued Extras map. When non-empty, this should
	// be used as the canonical representation for node annotations
	// and Cinder API calls.
	RawJSON string
}

// IConnector is the interface to the os-brick sidecar.
type IConnector interface {
	// GetConnectorProperties returns the host initiator information.
	GetConnectorProperties(ctx context.Context) (*ConnectorProperties, error)

	// ConnectVolume asks the sidecar to connect a volume described by
	// connectionInfo (JSON) and returns the resulting device path.
	ConnectVolume(ctx context.Context, connectionInfo string) (string, error)

	// DisconnectVolume asks the sidecar to disconnect a previously
	// connected volume described by connectionInfo (JSON).
	DisconnectVolume(ctx context.Context, connectionInfo string) error

	// ExtendVolume asks the sidecar to rescan/extend a connected
	// volume described by connectionInfo (JSON).
	ExtendVolume(ctx context.Context, connectionInfo string) error
}
