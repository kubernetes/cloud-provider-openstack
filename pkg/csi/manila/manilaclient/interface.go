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

package manilaclient

import (
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/messages"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/sharetypes"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/snapshots"
	openstack_provider "k8s.io/cloud-provider-openstack/pkg/cloudprovider/providers/openstack"
)

type Interface interface {
	GetShareByID(shareID string) (*shares.Share, error)
	GetShareByName(shareName string) (*shares.Share, error)
	CreateShare(opts shares.CreateOptsBuilder) (*shares.Share, error)
	DeleteShare(shareID string) error

	GetExportLocations(shareID string) ([]shares.ExportLocation, error)

	SetShareMetadata(shareID string, opts shares.SetMetadataOptsBuilder) (map[string]string, error)

	GetAccessRights(shareID string) ([]shares.AccessRight, error)
	GrantAccess(shareID string, opts shares.GrantAccessOptsBuilder) (*shares.AccessRight, error)

	GetSnapshotByID(snapID string) (*snapshots.Snapshot, error)
	GetSnapshotByName(snapName string) (*snapshots.Snapshot, error)
	CreateSnapshot(opts snapshots.CreateOptsBuilder) (*snapshots.Snapshot, error)
	DeleteSnapshot(snapID string) error

	GetExtraSpecs(shareTypeID string) (sharetypes.ExtraSpecs, error)
	GetShareTypes() ([]sharetypes.ShareType, error)
	GetShareTypeIDFromName(shareTypeName string) (string, error)

	GetUserMessages(opts messages.ListOptsBuilder) ([]messages.Message, error)
}

type Builder interface {
	New(o *openstack_provider.AuthOpts) (Interface, error)
}
