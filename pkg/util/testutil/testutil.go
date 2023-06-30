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

package testutil

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MustCreateFile creates a temporary file and initialises it with the
// provided content.  Returns the path (used for updates etc.) and a cleanup
// function that should be deferred.
func MustCreateFile(t *testing.T, content string) (string, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "")
	assert.Nil(t, err)

	_, err = f.WriteString(content)
	assert.Nil(t, err)

	err = f.Close()
	assert.Nil(t, err)

	cleanup := func() {
		_ = os.Remove(f.Name())
	}

	return f.Name(), cleanup
}

// MustUpdateFile writes the provided contents to the given file path.
func MustUpdateFile(t *testing.T, path, content string) {
	t.Helper()

	f, err := os.Create(path)
	assert.Nil(t, err)

	_, err = f.WriteString(content)
	assert.Nil(t, err)

	err = f.Close()
	assert.Nil(t, err)
}

// MustReadAllString takes an io.Reader and reads the contents into a string.
func MustReadAllString(t *testing.T, reader io.Reader) string {
	t.Helper()

	contents, err := io.ReadAll(reader)
	assert.Nil(t, err)

	return string(contents)
}
