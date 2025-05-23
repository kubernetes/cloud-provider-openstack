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

package sanity

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/messages"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/shares"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/sharetypes"
	"github.com/gophercloud/gophercloud/v2/openstack/sharedfilesystems/v2/snapshots"
	"k8s.io/cloud-provider-openstack/pkg/client"
	"k8s.io/cloud-provider-openstack/pkg/csi/manila/manilaclient"
)

var (
	fakeShareID       = 1
	fakeAccessRightID = 1
	fakeSnapshotID    = 1

	fakeShares       = make(map[int]*shares.Share)
	fakeAccessRights = make(map[int]*shares.AccessRight)
	fakeSnapshots    = make(map[int]*snapshots.Snapshot)
)

type fakeManilaClientBuilder struct{}

func (b fakeManilaClientBuilder) New(ctx context.Context, o *client.AuthOpts) (manilaclient.Interface, error) {
	return &fakeManilaClient{}, nil
}

type fakeManilaClient struct{}

func optsMapToStruct(optsMap map[string]interface{}, dst interface{}) error {
	res := gophercloud.Result{Body: optsMap}
	return res.ExtractInto(dst)
}

func strToInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func intToStr(n int) string {
	return strconv.Itoa(n)
}

func shareExists(shareID string) bool {
	_, ok := fakeShares[strToInt(shareID)]
	return ok
}

func (c fakeManilaClient) GetMicroversion() string {
	return ""
}

func (c fakeManilaClient) SetMicroversion(_ string) {
}

func (c fakeManilaClient) GetShareByID(_ context.Context, shareID string) (*shares.Share, error) {
	s, ok := fakeShares[strToInt(shareID)]
	if !ok {
		return nil, gophercloud.ErrResourceNotFound{}
	}

	return s, nil
}

func (c fakeManilaClient) GetShareByName(ctx context.Context, shareName string) (*shares.Share, error) {
	var shareID string
	for _, share := range fakeShares {
		if share.Name == shareName {
			shareID = share.ID
			break
		}
	}

	if shareID == "" {
		return nil, gophercloud.ErrResourceNotFound{}
	}

	return c.GetShareByID(ctx, shareID)
}

func (c fakeManilaClient) CreateShare(_ context.Context, opts shares.CreateOptsBuilder) (*shares.Share, error) {
	var res shares.CreateResult
	res.Body = opts

	share := &shares.Share{}
	if err := res.ExtractInto(share); err != nil {
		return nil, err
	}

	share.ID = intToStr(fakeShareID)
	share.Status = "available"
	share.SnapshotSupport = true
	share.CreateShareFromSnapshotSupport = true

	fakeShares[fakeShareID] = share
	fakeShareID++

	return share, nil
}

func (c fakeManilaClient) DeleteShare(_ context.Context, shareID string) error {
	id := strToInt(shareID)
	if _, ok := fakeShares[id]; !ok {
		return gophercloud.ErrResourceNotFound{}
	}

	delete(fakeShares, id)
	return nil
}

func (c fakeManilaClient) ExtendShare(ctx context.Context, shareID string, opts shares.ExtendOptsBuilder) error {
	share, err := c.GetShareByID(ctx, shareID)
	if err != nil {
		return err
	}

	var res shares.ExtendResult
	res.Body = opts

	extendOpts := &shares.ExtendOpts{}
	if err := res.ExtractInto(extendOpts); err != nil {
		return err
	}

	if extendOpts.NewSize < share.Size {
		return fmt.Errorf("new size is smaller than old size: %d < %d", extendOpts.NewSize, share.Size)
	}

	share.Size = extendOpts.NewSize

	return nil
}

func (c fakeManilaClient) GetExportLocations(_ context.Context, shareID string) ([]shares.ExportLocation, error) {
	if !shareExists(shareID) {
		return nil, gophercloud.ErrResourceNotFound{}
	}

	return []shares.ExportLocation{{Path: "fake-server:/fake-path"}}, nil
}

func (c fakeManilaClient) SetShareMetadata(_ context.Context, shareID string, opts shares.SetMetadataOptsBuilder) (map[string]string, error) {
	return nil, nil
}

func (c fakeManilaClient) GetExtraSpecs(_ context.Context, shareTypeID string) (sharetypes.ExtraSpecs, error) {
	return map[string]interface{}{"snapshot_support": "True", "create_share_from_snapshot_support": "True"}, nil
}

func (c fakeManilaClient) GetShareTypes(_ context.Context) ([]sharetypes.ShareType, error) {
	return []sharetypes.ShareType{
		{
			ID:                 "914dbaad-7242-4c34-a9ee-aa3831189972",
			Name:               "default",
			IsPublic:           true,
			RequiredExtraSpecs: map[string]interface{}{"driver_handles_share_servers": "True"},
			ExtraSpecs:         map[string]interface{}{"driver_handles_share_servers": "True", "snapshot_support": "True", "create_share_from_snapshot_support": "True"},
		},
	}, nil
}

func (c fakeManilaClient) GetShareTypeIDFromName(_ context.Context, shareTypeName string) (string, error) {
	return "", nil
}

func (c fakeManilaClient) GetAccessRights(_ context.Context, shareID string) ([]shares.AccessRight, error) {
	if !shareExists(shareID) {
		return nil, gophercloud.ErrResourceNotFound{}
	}

	var accessRights []shares.AccessRight
	for _, r := range fakeAccessRights {
		if r.ShareID == shareID {
			accessRights = append(accessRights, *r)
		}
	}

	return accessRights, nil
}

func (c fakeManilaClient) GrantAccess(_ context.Context, shareID string, opts shares.GrantAccessOptsBuilder) (*shares.AccessRight, error) {
	if !shareExists(shareID) {
		return nil, gophercloud.ErrResourceNotFound{}
	}

	optsMap, err := opts.ToGrantAccessMap()
	if err != nil {
		return nil, err
	}

	accessRight := &shares.AccessRight{}
	if err = optsMapToStruct(optsMap, accessRight); err != nil {
		return nil, err
	}

	accessRight.ID = intToStr(fakeAccessRightID)
	accessRight.ShareID = shareID
	fakeAccessRights[fakeAccessRightID] = accessRight
	fakeAccessRightID++

	return accessRight, nil
}

func (c fakeManilaClient) GetSnapshotByID(_ context.Context, snapID string) (*snapshots.Snapshot, error) {
	s, ok := fakeSnapshots[strToInt(snapID)]
	if !ok {
		return nil, gophercloud.ErrUnexpectedResponseCode{Actual: 404}
	}

	return s, nil
}

func (c fakeManilaClient) GetSnapshotByName(ctx context.Context, snapName string) (*snapshots.Snapshot, error) {
	var snapID string
	for _, snap := range fakeSnapshots {
		if snap.Name == snapName {
			snapID = snap.ID
			break
		}
	}

	if snapID == "" {
		return nil, gophercloud.ErrResourceNotFound{Name: snapName, ResourceType: "snapshot"}
	}

	return c.GetSnapshotByID(ctx, snapID)
}

func (c fakeManilaClient) CreateSnapshot(_ context.Context, opts snapshots.CreateOptsBuilder) (*snapshots.Snapshot, error) {
	var res snapshots.CreateResult
	res.Body = opts

	snap := &snapshots.Snapshot{}
	if err := res.ExtractInto(snap); err != nil {
		return nil, err
	}

	snap.ID = intToStr(fakeSnapshotID)
	snap.Status = "available"

	if !shareExists(snap.ShareID) {
		return nil, gophercloud.ErrUnexpectedResponseCode{Actual: 404}
	}

	fakeSnapshots[fakeSnapshotID] = snap
	fakeSnapshotID++

	return snap, nil
}

func (c fakeManilaClient) DeleteSnapshot(_ context.Context, snapID string) error {
	id := strToInt(snapID)
	if _, ok := fakeSnapshots[id]; !ok {
		return gophercloud.ErrResourceNotFound{}
	}

	delete(fakeSnapshots, id)
	return nil
}

func (c fakeManilaClient) GetUserMessages(_ context.Context, opts messages.ListOptsBuilder) ([]messages.Message, error) {
	return nil, nil
}
