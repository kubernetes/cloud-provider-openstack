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

package filewatcher

import (
	"context"
	"io"
)

// FileUpdateCallback allows clients to register a callback of this
// type when a file updates.
type FileUpdateCallback func([]byte)

// FileWatcher allows a file to be read, and updates notified via
// a subscribe/publish interface.
type FileWatcher interface {
	// Contents allows the initial contents to be read.
	Contents() io.Reader
	// Subscribe allows a client to begin listening for file updates.
	// Does not get called on subscription, but may do in the future.
	Subscribe(clientID string, callback FileUpdateCallback)
	// Unsubscribe removes a client from file update notifications.
	Unsubscribe(clientID string)
	// Run starts the watcher routine.
	Run(ctx context.Context)
}
