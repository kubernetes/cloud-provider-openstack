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

package responsebroker

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type (
	response struct {
		dataPtr interface{}
		err     error

		m               sync.Mutex
		wg              sync.WaitGroup
		ownerProtection int32
	}

	// ResponseBroker propagates a response to an arbitrary number of requests
	ResponseBroker struct {
		m  sync.Mutex
		rs map[string]*response
	}

	// ResponseHandle for a request
	ResponseHandle struct {
		resp *response
	}
)

func New() *ResponseBroker {
	return &ResponseBroker{
		rs: make(map[string]*response),
	}
}

func (rb *ResponseBroker) Acquire(identifier string) (handle ResponseHandle, isOwner bool) {
	rb.m.Lock()

	if resp, ok := rb.rs[identifier]; ok {
		handle.resp = resp
		handle.resp.wg.Add(1)
	} else {
		isOwner = true

		handle.resp = &response{ownerProtection: 1}
		rb.rs[identifier] = handle.resp
	}

	rb.m.Unlock()

	if !isOwner {
		// Spin-lock to guard against a race condition when ResponseBroker's map mutex
		// is already unlocked but ResponseHandle is still not locked, which could lead to a situation
		// where a non-owner acquires handle.resp.m.Lock() before the owner - the owner must be the
		// first one to lock.
		// Although such situation is unlikely, it could happen if e.g. CO makes multiple CreateVolume requests
		// for the same volume in a quick succession.
		for atomic.LoadInt32(&handle.resp.ownerProtection) != 0 {
			runtime.Gosched()
		}
	}

	handle.resp.m.Lock()

	if isOwner {
		atomic.StoreInt32(&handle.resp.ownerProtection, 0)
	}

	return
}

// Done marks this request identified by `identifier` as finished
// and waits till anyone who's interested in its response has read it.
// Should be called after a successful request.
// Invalidates all ResponseHandles.
func (rb *ResponseBroker) Done(identifier string) {
	rb.m.Lock()
	rb.rs[identifier].wg.Wait()
	delete(rb.rs, identifier)
	rb.m.Unlock()
}

// Write writes a response, letting others read it
func (h *ResponseHandle) Write(data interface{}, err error) {
	h.resp.dataPtr = data
	h.resp.err = err
	h.resp.m.Unlock()
}

// Release releases the handle.
func (h *ResponseHandle) Release() {
	h.resp.m.Unlock()
}

// Read reads the response
func (h *ResponseHandle) Read() (interface{}, error) {
	defer h.resp.wg.Done()
	return h.resp.dataPtr, h.resp.err
}
