/*
Copyright 2017 The Kubernetes Authors.

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

package cinder

import (
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/snapshots"
	"golang.org/x/net/context"
	"k8s.io/cloud-provider-openstack/pkg/csi/cinder/openstack"
)

var fakeCluster = "cluster"
var fakeNodeID = "CSINodeID"
var fakeEndpoint = "tcp://127.0.0.1:10000"
var fakeConfig = "/etc/cloud.conf"
var fakeCtx = context.Background()
var fakeVolName = "CSIVolumeName"
var fakeVolID = "CSIVolumeID"
var fakeSnapshotName = "CSISnapshotName"
var fakeSnapshotID = "261a8b81-3660-43e5-bab8-6470b65ee4e8"
var fakeCapacityGiB = 1
var fakeVolType = ""
var fakeAvailability = "nova"
var fakeDevicePath = "/dev/xxx"
var fakeTargetPath = "/mnt/cinder"

var fakeVol1 = openstack.Volume{
	ID:     "261a8b81-3660-43e5-bab8-6470b65ee4e9",
	Name:   "fake-duplicate",
	Status: "available",
	AZ:     "",
}
var fakeVol2 = openstack.Volume{
	ID:     "261a8b81-3660-43e5-bab8-6470b65ee4e9",
	Name:   "fake-duplicate",
	Status: "available",
	AZ:     "",
}
var fakeSnapshotRes = snapshots.Snapshot{
	ID:       fakeSnapshotID,
	Name:     "fake-snapshot",
	VolumeID: fakeVolID,
}
var fakeSnapshotsRes = []snapshots.Snapshot{fakeSnapshotRes}
