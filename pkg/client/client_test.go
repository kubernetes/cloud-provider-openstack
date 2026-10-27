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

package client

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/utils/v2/client"
	"k8s.io/klog/v2"
)

// TestLoggerReportsCallerLine verifies that debug log lines emitted through
// the Logger wrapper are attributed to the caller that initiated the
// gophercloud request, not to the wrapper file itself (issue #2300).
func TestLoggerReportsCallerLine(t *testing.T) {
	state := klog.CaptureState()
	defer state.Restore()

	var fs flag.FlagSet
	klog.InitFlags(&fs)
	if err := fs.Set("v", "6"); err != nil {
		t.Fatalf("failed to set verbosity: %v", err)
	}
	if err := fs.Set("logtostderr", "false"); err != nil {
		t.Fatalf("failed to set logtostderr: %v", err)
	}
	if err := fs.Set("skip_headers", "false"); err != nil {
		t.Fatalf("failed to set skip_headers: %v", err)
	}

	var buf bytes.Buffer
	klog.SetOutput(&buf)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	provider, err := openstack.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("failed to build provider: %v", err)
	}
	provider.HTTPClient = http.Client{
		Transport: &client.RoundTripper{
			Rt:     http.DefaultTransport,
			Logger: &Logger{},
		},
	}

	if _, err := provider.Request(context.Background(), http.MethodGet, srv.URL, &gophercloud.RequestOpts{
		KeepResponseBody: true,
		OkCodes:          []int{http.StatusOK},
	}); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	klog.Flush()

	output := buf.String()
	if output == "" {
		t.Fatalf("expected klog output, got none")
	}
	// The wrapper's own file must not appear as the source location, since
	// that would mean every debug line is mis-attributed to client.go (the
	// behavior reported in issue #2300).
	if strings.Contains(output, "client.go:") && !strings.Contains(output, "client_test.go:") {
		t.Fatalf("debug output is attributed to the wrapper, expected the caller; got:\n%s", output)
	}
	if !strings.Contains(output, "client_test.go:") {
		t.Fatalf("expected debug output to reference the test caller (client_test.go); got:\n%s", output)
	}
}
