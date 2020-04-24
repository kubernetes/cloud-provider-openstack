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
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/messages"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/sharetypes"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/snapshots"
	shares_utils "github.com/gophercloud/utils/openstack/sharedfilesystems/v2/shares"
	sharetypes_utils "github.com/gophercloud/utils/openstack/sharedfilesystems/v2/sharetypes"
	snapshots_utils "github.com/gophercloud/utils/openstack/sharedfilesystems/v2/snapshots"
)

type Client struct {
	c *gophercloud.ServiceClient
}

func (c Client) GetShareByID(shareID string) (*shares.Share, error) {
	return shares.Get(c.c, shareID).Extract()
}

func (c Client) GetShareByName(shareName string) (*shares.Share, error) {
	shareID, err := shares_utils.IDFromName(c.c, shareName)
	if err != nil {
		return nil, err
	}

	return shares.Get(c.c, shareID).Extract()
}

func (c Client) CreateShare(opts shares.CreateOptsBuilder) (*shares.Share, error) {
	return shares.Create(c.c, opts).Extract()
}

func (c Client) DeleteShare(shareID string) error {
	return shares.Delete(c.c, shareID).ExtractErr()
}

func (c Client) GetExportLocations(shareID string) ([]shares.ExportLocation, error) {
	return shares.ListExportLocations(c.c, shareID).Extract()
}

func (c Client) SetShareMetadata(shareID string, opts shares.SetMetadataOptsBuilder) (map[string]string, error) {
	return shares.SetMetadata(c.c, shareID, opts).Extract()
}

func (c Client) GetAccessRights(shareID string) ([]shares.AccessRight, error) {
	return shares.ListAccessRights(c.c, shareID).Extract()
}

func (c Client) GrantAccess(shareID string, opts shares.GrantAccessOptsBuilder) (*shares.AccessRight, error) {
	return shares.GrantAccess(c.c, shareID, opts).Extract()
}

func (c Client) GetSnapshotByID(snapID string) (*snapshots.Snapshot, error) {
	return snapshots.Get(c.c, snapID).Extract()
}

func (c Client) GetSnapshotByName(snapName string) (*snapshots.Snapshot, error) {
	snapID, err := snapshots_utils.IDFromName(c.c, snapName)
	if err != nil {
		return nil, err
	}

	return snapshots.Get(c.c, snapID).Extract()
}

func (c Client) CreateSnapshot(opts snapshots.CreateOptsBuilder) (*snapshots.Snapshot, error) {
	return snapshots.Create(c.c, opts).Extract()
}

func (c Client) DeleteSnapshot(snapID string) error {
	return snapshots.Delete(c.c, snapID).ExtractErr()
}

func (c Client) GetExtraSpecs(shareTypeID string) (sharetypes.ExtraSpecs, error) {
	return sharetypes.GetExtraSpecs(c.c, shareTypeID).Extract()
}

func (c Client) GetShareTypes() ([]sharetypes.ShareType, error) {
	allPages, err := sharetypes.List(c.c, sharetypes.ListOpts{}).AllPages()
	if err != nil {
		return nil, err
	}

	return sharetypes.ExtractShareTypes(allPages)
}

func (c Client) GetShareTypeIDFromName(shareTypeName string) (string, error) {
	return sharetypes_utils.IDFromName(c.c, shareTypeName)
}

func (c Client) GetUserMessages(opts messages.ListOptsBuilder) ([]messages.Message, error) {
	allPages, err := messages.List(c.c, opts).AllPages()
	if err != nil {
		return nil, err
	}

	return messages.ExtractMessages(allPages)
}
