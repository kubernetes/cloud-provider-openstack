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

package manila

import (
	"errors"
	"fmt"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/shareadapters"
	"k8s.io/klog"
)

type (
	volumeID string
)

const (
	bytesInGiB = 1024 * 1024 * 1024
)

func parseGRPCEndpoint(endpoint string) (proto, addr string, err error) {
	const (
		unixScheme = "unix://"
		tcpScheme  = "tcp://"
	)

	if strings.HasPrefix(endpoint, "/") {
		return "unix", endpoint, nil
	}

	if strings.HasPrefix(endpoint, unixScheme) {
		pos := len(unixScheme)
		if endpoint[pos] != '/' {
			// endpoint seems to be "unix://absolute/path/to/somewhere"
			// we're missing one '/'...compensate by decrementing pos
			pos--
		}

		return "unix", endpoint[pos:], nil
	}

	if strings.HasPrefix(endpoint, tcpScheme) {
		return "tcp", endpoint[len(tcpScheme):], nil
	}

	return "", "", errors.New("endpoint uses unsupported scheme")
}

func endpointAddress(proto, addr string) string {
	return fmt.Sprintf("%s://%s", proto, addr)
}

func fmtGrpcConnError(fwdEndpoint string, err error) string {
	return fmt.Sprintf("connecting to fwd plugin at %s failed: %v", fwdEndpoint, err)
}

func newVolumeID(volumeName string) volumeID {
	return volumeID(fmt.Sprintf("csi-manila-%s", volumeName))
}

func bytesToGiB(sizeInBytes int64) int {
	sizeInGiB := int(sizeInBytes / bytesInGiB)

	if int64(sizeInGiB)*bytesInGiB < sizeInBytes {
		// Round up
		return sizeInGiB + 1
	}

	return sizeInGiB
}

func getShareAdapter(proto string) shareadapters.ShareAdapter {
	switch strings.ToLower(proto) {
	case "cephfs":
		return &shareadapters.Cephfs{}
	case "nfs":
		return &shareadapters.NFS{}
	default:
		klog.Fatalf("unknown share adapter %s", proto)
	}

	return nil
}

func getAccessRightByID(id, shareID string, manilaClient *gophercloud.ServiceClient) (*shares.AccessRight, error) {
	accessRights, err := shares.ListAccessRights(manilaClient, shareID).Extract()
	if err != nil {
		return nil, err
	}

	for i := range accessRights {
		if accessRights[i].ID == id {
			return &accessRights[i], nil
		}
	}

	return nil, fmt.Errorf("access right %s for share ID %s not found", id, shareID)
}

//
// Controller service request validation
//

func validateCreateVolumeRequest(req *csi.CreateVolumeRequest) error {
	if req.GetName() == "" {
		return errors.New("volume name cannot be empty")
	}

	reqCaps := req.GetVolumeCapabilities()
	if reqCaps == nil {
		return errors.New("volume capabilities cannot be empty")
	}

	for _, cap := range reqCaps {
		if cap.GetBlock() != nil {
			return errors.New("block volume not supported")
		}
	}

	if req.GetSecrets() == nil || len(req.GetSecrets()) == 0 {
		return errors.New("secrets cannot be empty")
	}

	return nil
}

func validateDeleteVolumeRequest(req *csi.DeleteVolumeRequest) error {
	if req.GetVolumeId() == "" {
		return errors.New("volume ID cannot be empty")
	}

	if req.GetSecrets() == nil || len(req.GetSecrets()) == 0 {
		return errors.New("secrets cannot be empty")
	}

	return nil
}

//
// Node service request validation
//

func validateNodeStageVolumeRequest(req *csi.NodeStageVolumeRequest) error {
	if req.GetVolumeCapability() == nil {
		return errors.New("volume capability missing in request")
	}

	if req.GetVolumeId() == "" {
		return errors.New("volume ID missing in request")
	}

	if req.GetVolumeContext() == nil || len(req.GetVolumeContext()) == 0 {
		return errors.New("volume context cannot be nil or empty")
	}

	if req.GetSecrets() == nil || len(req.GetSecrets()) == 0 {
		return errors.New("stage secrets cannot be nil or empty")
	}

	return nil
}

func validateNodeUnstageVolumeRequest(req *csi.NodeUnstageVolumeRequest) error {
	if req.GetVolumeId() == "" {
		return errors.New("volume ID missing in request")
	}

	return nil
}

func validateNodePublishVolumeRequest(req *csi.NodePublishVolumeRequest) error {
	if req.GetVolumeCapability() == nil {
		return errors.New("volume capability missing in request")
	}

	if req.GetVolumeId() == "" {
		return errors.New("volume ID missing in request")
	}

	if req.GetVolumeContext() == nil || len(req.GetSecrets()) == 0 {
		return errors.New("volume context cannot be nil or empty")
	}

	if req.GetSecrets() == nil || len(req.GetSecrets()) == 0 {
		return errors.New("node publish secrets cannot be nil or empty")
	}

	return nil
}

func validateNodeUnpublishVolumeRequest(req *csi.NodeUnpublishVolumeRequest) error {
	if req.GetVolumeId() == "" {
		return errors.New("volume ID missing in request")
	}

	return nil
}
