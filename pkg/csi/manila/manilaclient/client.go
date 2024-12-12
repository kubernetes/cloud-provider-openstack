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

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/messages"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/sharetypes"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/snapshots"
	shares_utils "github.com/gophercloud/utils/v2/openstack/sharedfilesystems/v2/shares"
	sharetypes_utils "github.com/gophercloud/utils/v2/openstack/sharedfilesystems/v2/sharetypes"
	snapshots_utils "github.com/gophercloud/utils/v2/openstack/sharedfilesystems/v2/snapshots"
)

type Client struct {
	c *gophercloud.ServiceClient
}

func (c Client) GetMicroversion() string {
	return c.c.Microversion
}

func (c Client) SetMicroversion(version string) {
	c.c.Microversion = version
}

func (c Client) GetShareByID(ctx context.Context, shareID string) (*shares.Share, error) {
	return shares.Get(ctx, c.c, shareID).Extract()
}

func (c Client) GetShareByName(ctx context.Context, shareName string) (*shares.Share, error) {
	shareID, err := shares_utils.IDFromName(ctx, c.c, shareName)
	if err != nil {
		return nil, err
	}

	return shares.Get(ctx, c.c, shareID).Extract()
}

func (c Client) CreateShare(ctx context.Context, opts shares.CreateOptsBuilder) (*shares.Share, error) {
	return shares.Create(ctx, c.c, opts).Extract()
}

func (c Client) DeleteShare(ctx context.Context, shareID string) error {
	return shares.Delete(ctx, c.c, shareID).ExtractErr()
}

func (c Client) ExtendShare(ctx context.Context, shareID string, opts shares.ExtendOptsBuilder) error {
	return shares.Extend(ctx, c.c, shareID, opts).ExtractErr()
}

func (c Client) GetExportLocations(ctx context.Context, shareID string) ([]shares.ExportLocation, error) {
	return shares.ListExportLocations(ctx, c.c, shareID).Extract()
}

func (c Client) SetShareMetadata(ctx context.Context, shareID string, opts shares.SetMetadataOptsBuilder) (map[string]string, error) {
	return shares.SetMetadata(ctx, c.c, shareID, opts).Extract()
}

func (c Client) GetAccessRights(ctx context.Context, shareID string) ([]shares.AccessRight, error) {
	return shares.ListAccessRights(ctx, c.c, shareID).Extract()
}

func (c Client) GrantAccess(ctx context.Context, shareID string, opts shares.GrantAccessOptsBuilder) (*shares.AccessRight, error) {
	return shares.GrantAccess(ctx, c.c, shareID, opts).Extract()
}

func (c Client) GetSnapshotByID(ctx context.Context, snapID string) (*snapshots.Snapshot, error) {
	return snapshots.Get(ctx, c.c, snapID).Extract()
}

func (c Client) GetSnapshotByName(ctx context.Context, snapName string) (*snapshots.Snapshot, error) {
	snapID, err := snapshots_utils.IDFromName(ctx, c.c, snapName)
	if err != nil {
		return nil, err
	}

	return snapshots.Get(ctx, c.c, snapID).Extract()
}

func (c Client) CreateSnapshot(ctx context.Context, opts snapshots.CreateOptsBuilder) (*snapshots.Snapshot, error) {
	return snapshots.Create(ctx, c.c, opts).Extract()
}

func (c Client) DeleteSnapshot(ctx context.Context, snapID string) error {
	return snapshots.Delete(ctx, c.c, snapID).ExtractErr()
}

func (c Client) GetExtraSpecs(ctx context.Context, shareTypeID string) (sharetypes.ExtraSpecs, error) {
	return sharetypes.GetExtraSpecs(ctx, c.c, shareTypeID).Extract()
}

func (c Client) GetShareTypes(ctx context.Context) ([]sharetypes.ShareType, error) {
	allPages, err := sharetypes.List(c.c, sharetypes.ListOpts{}).AllPages(ctx)
	if err != nil {
		return nil, err
	}

	return sharetypes.ExtractShareTypes(allPages)
}

func (c Client) GetShareTypeIDFromName(ctx context.Context, shareTypeName string) (string, error) {
	return sharetypes_utils.IDFromName(ctx, c.c, shareTypeName)
}

func (c Client) GetUserMessages(ctx context.Context, opts messages.ListOptsBuilder) ([]messages.Message, error) {
	allPages, err := messages.List(c.c, opts).AllPages(ctx)
	if err != nil {
		return nil, err
	}

	return messages.ExtractMessages(allPages)
}
