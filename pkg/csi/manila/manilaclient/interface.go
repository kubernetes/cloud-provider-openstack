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
	"context"

	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/messages"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/sharetypes"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/snapshots"
	"k8s.io/cloud-provider-openstack/pkg/client"
)

type Interface interface {
	GetMicroversion() string
	SetMicroversion(version string)

	GetShareByID(ctx context.Context, shareID string) (*shares.Share, error)
	GetShareByName(ctx context.Context, shareName string) (*shares.Share, error)
	CreateShare(ctx context.Context, opts shares.CreateOptsBuilder) (*shares.Share, error)
	DeleteShare(ctx context.Context, shareID string) error
	ExtendShare(ctx context.Context, shareID string, opts shares.ExtendOptsBuilder) error

	GetExportLocations(ctx context.Context, shareID string) ([]shares.ExportLocation, error)

	SetShareMetadata(ctx context.Context, shareID string, opts shares.SetMetadataOptsBuilder) (map[string]string, error)

	GetAccessRights(ctx context.Context, shareID string) ([]shares.AccessRight, error)
	GrantAccess(ctx context.Context, shareID string, opts shares.GrantAccessOptsBuilder) (*shares.AccessRight, error)

	GetSnapshotByID(ctx context.Context, snapID string) (*snapshots.Snapshot, error)
	GetSnapshotByName(ctx context.Context, snapName string) (*snapshots.Snapshot, error)
	CreateSnapshot(ctx context.Context, opts snapshots.CreateOptsBuilder) (*snapshots.Snapshot, error)
	DeleteSnapshot(ctx context.Context, snapID string) error

	GetExtraSpecs(ctx context.Context, shareTypeID string) (sharetypes.ExtraSpecs, error)
	GetShareTypes(ctx context.Context) ([]sharetypes.ShareType, error)
	GetShareTypeIDFromName(ctx context.Context, shareTypeName string) (string, error)

	GetUserMessages(ctx context.Context, opts messages.ListOptsBuilder) ([]messages.Message, error)
}

type Builder interface {
	New(ctx context.Context, o *client.AuthOpts) (Interface, error)
}
