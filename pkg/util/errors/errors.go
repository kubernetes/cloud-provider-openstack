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

package errors

import (
	"errors"
	"net/http"

	"github.com/gophercloud/gophercloud/v2"
)

// ErrQuotaExceeded is used when openstack runs in to quota limits
var ErrQuotaExceeded = errors.New("quota exceeded")

// ErrNotFound is used to inform that the object is missing
var ErrNotFound = errors.New("failed to find object")

// ErrMultipleResults is used when we unexpectedly get back multiple results
var ErrMultipleResults = errors.New("multiple results where only one expected")

// ErrNoAddressFound is used when we cannot find an ip address for the host
var ErrNoAddressFound = errors.New("no address found for host")

// ErrIPv6SupportDisabled is used when one tries to use IPv6 Addresses when
// IPv6 support is disabled by config
var ErrIPv6SupportDisabled = errors.New("IPv6 support is disabled")

// ErrNoRouterID is used when router-id is not set
var ErrNoRouterID = errors.New("router-id not set in cloud provider config")

// ErrNoNodeInformer is used when node informer is not yet initialized
var ErrNoNodeInformer = errors.New("node informer is not yet initialized")

func IsNotFound(err error) bool {
	if err == ErrNotFound {
		return true
	}

	if _, ok := err.(gophercloud.ErrResourceNotFound); ok {
		return true
	}

	return gophercloud.ResponseCodeIs(err, http.StatusNotFound)
}

func IsInvalidError(err error) bool {
	return gophercloud.ResponseCodeIs(err, http.StatusBadRequest)
}

func IsConflictError(err error) bool {
	return gophercloud.ResponseCodeIs(err, http.StatusConflict)
}
