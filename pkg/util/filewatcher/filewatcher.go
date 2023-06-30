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
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	log "k8s.io/klog/v2"
)

const (
	// defaultPeriod controls the default time period between checking
	// whether the file contents have updated.
	defaultPeriod = 10 * time.Second
)

// loadFile reads the contents and returns them with a SHA256 hash of a file's contents.
func loadFile(path string) ([]byte, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}

	defer file.Close()

	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, "", err
	}

	return contents, fmt.Sprintf("%x", sha256.Sum256(contents)), nil
}

type PollingFileWatcher struct {
	// path is the absolute path to the file.
	path string

	// contents is a cached version of the contents.
	contents []byte

	// hash is a hash of the file contents.
	hash string

	// subscribers is a map of clients to their respective callbacks.
	subscribers map[string]FileUpdateCallback

	// period controls the duration between polls.
	period time.Duration

	// readMutex controls atomicity of contents reads.
	readMutex sync.Mutex

	// subscribersMutex controls atomicity of subscribers.
	subscribersMutex sync.Mutex

	// updates records the number of updates, useful for spotting
	// files changing all the time, and indeed testing.
	updates int

	// refreshes records the number of times the watcher has polled
	// the file.
	refreshes int
}

// New reads the file and returns the initial contents.
func NewPollingFileWatcher(path string, period time.Duration) (*PollingFileWatcher, error) {
	contents, hash, err := loadFile(path)
	if err != nil {
		return nil, err
	}

	if period == 0 {
		period = defaultPeriod
	}

	watcher := &PollingFileWatcher{
		path:        path,
		contents:    contents,
		hash:        hash,
		subscribers: map[string]FileUpdateCallback{},
		period:      period,
	}

	return watcher, nil
}

// Contents allows the initial contents to be read.
func (f *PollingFileWatcher) Contents() io.Reader {
	f.readMutex.Lock()
	defer f.readMutex.Unlock()

	return bytes.NewBuffer(f.contents)
}

// Updates returns the number of file updates that have been seen and
// processed.
func (f *PollingFileWatcher) Updates() int {
	f.readMutex.Lock()
	defer f.readMutex.Unlock()

	return f.updates
}

// Refreshes returns the number of file refreshes that have been attempted.
func (f *PollingFileWatcher) Refreshes() int {
	f.readMutex.Lock()
	defer f.readMutex.Unlock()

	return f.refreshes
}

// Subscribe allows a client to begin listening for file updates.
// Does not get called on subscription, but may do in the future.
func (f *PollingFileWatcher) Subscribe(clientID string, callback FileUpdateCallback) {
	f.subscribersMutex.Lock()
	defer f.subscribersMutex.Unlock()

	// TODO: do we want to raise an error if already set?
	f.subscribers[clientID] = callback
}

// Unsubscribe removes a client from file update notifications.
func (f *PollingFileWatcher) Unsubscribe(clientID string) {
	f.subscribersMutex.Lock()
	defer f.subscribersMutex.Unlock()

	// TODO: do we want to raise an error if not set?
	delete(f.subscribers, clientID)
}

// update updates the file contents and metadata.
func (f *PollingFileWatcher) update(contents []byte, hash string) {
	f.readMutex.Lock()
	defer f.readMutex.Unlock()

	f.updates++
	f.contents = contents
	f.hash = hash
}

// publish pushes updates out to subscibers.
func (f *PollingFileWatcher) publish() {
	f.subscribersMutex.Lock()
	defer f.subscribersMutex.Unlock()

	for _, callback := range f.subscribers {
		callback(f.contents)
	}
}

// refresh logs a file refresh.
func (f *PollingFileWatcher) refresh() {
	f.subscribersMutex.Lock()
	defer f.subscribersMutex.Unlock()

	f.refreshes++
}

// Run starts the watcher routine.
func (f *PollingFileWatcher) Run(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(f.period)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f.refresh()

				contents, hash, err := loadFile(f.path)
				if err != nil {
					log.ErrorS(err, "Failed to read watched file")
					break
				}

				if hash == f.hash {
					break
				}

				f.update(contents, hash)
				f.publish()
			}
		}
	}()
}
