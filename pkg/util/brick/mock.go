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

package brick

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockConnector is a testify mock for the IConnector interface.
type MockConnector struct {
	mock.Mock
}

var _ IConnector = &MockConnector{}

func (m *MockConnector) GetConnectorProperties(ctx context.Context) (*ConnectorProperties, error) {
	args := m.Called(ctx)
	if props := args.Get(0); props != nil {
		return props.(*ConnectorProperties), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockConnector) ConnectVolume(ctx context.Context, connectionInfo string) (string, error) {
	args := m.Called(ctx, connectionInfo)
	return args.String(0), args.Error(1)
}

func (m *MockConnector) DisconnectVolume(ctx context.Context, connectionInfo string) error {
	args := m.Called(ctx, connectionInfo)
	return args.Error(0)
}

func (m *MockConnector) ExtendVolume(ctx context.Context, connectionInfo string) error {
	args := m.Called(ctx, connectionInfo)
	return args.Error(0)
}
