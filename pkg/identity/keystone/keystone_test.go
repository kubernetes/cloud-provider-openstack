/*
Copyright 2019 The Kubernetes Authors.

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

package keystone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

func TestUserAgentFlag(t *testing.T) {
	tests := []struct {
		name        string
		shouldParse bool
		flags       []string
		expected    []string
	}{
		{"no_flag", true, []string{}, nil},
		{"one_flag", true, []string{"--user-agent=cluster/abc-123"}, []string{"cluster/abc-123"}},
		{"multiple_flags", true, []string{"--user-agent=a/b", "--user-agent=c/d"}, []string{"a/b", "c/d"}},
		{"flag_with_space", true, []string{"--user-agent=a b"}, []string{"a b"}},
		{"flag_split_with_space", true, []string{"--user-agent=a", "b"}, []string{"a"}},
		{"empty_flag", false, []string{"--user-agent"}, nil},
	}

	for _, testCase := range tests {
		userAgentData = []string{}

		t.Run(testCase.name, func(t *testing.T) {
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			AddExtraFlags(fs)

			err := fs.Parse(testCase.flags)

			if testCase.shouldParse && err != nil {
				t.Errorf("Flags failed to parse")
			} else if !testCase.shouldParse && err == nil {
				t.Errorf("Flags should not have parsed")
			} else if testCase.shouldParse {
				if !reflect.DeepEqual(userAgentData, testCase.expected) {
					t.Errorf("userAgentData %#v did not match expected value %#v", userAgentData, testCase.expected)
				}
			}
		})
	}
}

// mockKeystoner is a mock implementation of IKeystone for testing
type mockKeystoner struct{}

func (m *mockKeystoner) GetTokenInfo(ctx context.Context, token string) (*tokenInfo, error) {
	return nil, fmt.Errorf("invalid token")
}

func (m *mockKeystoner) GetGroups(ctx context.Context, token string, userID string) ([]string, error) {
	return nil, fmt.Errorf("invalid token")
}

func TestWebhookRouting(t *testing.T) {
	// Create a minimal Auth instance for testing
	auth := &Auth{
		authn: &Authenticator{
			keystoner: &mockKeystoner{},
		},
		authz: &Authorizer{
			pl: nil,
		},
		syncer: &Syncer{
			syncConfig: nil,
		},
	}

	tests := []struct {
		name           string
		path           string
		method         string
		body           map[string]interface{}
		expectedStatus int
	}{
		{
			name:   "valid_webhook_path",
			path:   "/webhook",
			method: http.MethodPost,
			body: map[string]interface{}{
				"apiVersion": "authentication.k8s.io/v1beta1",
				"kind":       "TokenReview",
				"spec": map[string]interface{}{
					"token": "test-token",
				},
			},
			// Handler will try to authenticate and fail, but we're testing routing
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid_path",
			path:           "/invalid",
			method:         http.MethodPost,
			body:           map[string]interface{}{},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "root_path",
			path:           "/",
			method:         http.MethodPost,
			body:           map[string]interface{}{},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a request
			bodyBytes, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			// Create a response recorder
			rr := httptest.NewRecorder()

			// Create router and register handler
			mux := http.NewServeMux()
			mux.HandleFunc("/webhook", auth.Handler)

			// Serve the request
			mux.ServeHTTP(rr, req)

			// Check status code
			if rr.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, rr.Code)
			}
		})
	}
}
