/*
Copyright 2023 The Kubernetes Authors.

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

package filewatcher_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/cloud-provider-openstack/pkg/util/filewatcher"
	"k8s.io/cloud-provider-openstack/pkg/util/testutil"
)

const (
	content1 = "foo"
	content2 = "bar"

	refreshPeriod = time.Second
)

// mustCreateNewPollingFileWatcher returns a new filw watcher or dies.
func mustCreateNewPollingFileWatcher(t *testing.T, path string) *filewatcher.PollingFileWatcher {
	t.Helper()

	w, err := filewatcher.NewPollingFileWatcher(path, refreshPeriod)
	assert.Nil(t, err)

	return w
}

// TestReadFile tests the file content is available immediately.
func TestReadFile(t *testing.T) {
	path, cleanup := testutil.MustCreateFile(t, content1)
	defer cleanup()

	watcher := mustCreateNewPollingFileWatcher(t, path)

	contents := testutil.MustReadAllString(t, watcher.Contents())
	assert.Equal(t, content1, contents)
}

// TestUpdateFile tests that when running, the file watcher correctly
// spots any updates and makes them available.
func TestUpdateFile(t *testing.T) {
	path, cleanup := testutil.MustCreateFile(t, content1)
	defer cleanup()

	// Run for at least three refresh periods before reading out the new contents.
	// Check that only 1 update has been seen and at least 3 refreshes have been
	// performed.  This checks the runloop performs as expected for both updates
	// and idle loops.
	ctx, cancel := context.WithTimeout(context.Background(), 4*refreshPeriod)
	defer cancel()

	watcher := mustCreateNewPollingFileWatcher(t, path)
	watcher.Run(ctx)

	contents := testutil.MustReadAllString(t, watcher.Contents())
	assert.Equal(t, content1, contents)

	testutil.MustUpdateFile(t, path, content2)

	// Wait for the context to expire...
	<-ctx.Done()

	contents = testutil.MustReadAllString(t, watcher.Contents())
	assert.Equal(t, content2, contents)
	assert.Equal(t, 1, watcher.Updates())
	assert.GreaterOrEqual(t, watcher.Refreshes(), 3)
}
