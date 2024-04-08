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

// Package openstack backups provides an implementation of Cinder Backup features
// cinder functions using Gophercloud.
package openstack

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/backups"
	"golang.org/x/net/context"
	"k8s.io/cloud-provider-openstack/pkg/metrics"
	"k8s.io/klog/v2"
)

const (
	backupReadyStatus                    = "available"
	backupErrorStatus                    = "error"
	backupBinary                         = "cinder-backup"
	backupDescription                    = "Created by OpenStack Cinder CSI driver"
	BackupMaxDurationSecondsPerGBDefault = 20
	BackupMaxDurationPerGB               = "backup-max-duration-seconds-per-gb"
	backupBaseDurationSeconds            = 30
	backupReadyCheckIntervalSeconds      = 7
)

// CreateBackup issues a request to create a Backup from the specified Snapshot with the corresponding ID and
// returns the resultant gophercloud Backup Item upon success.
func (os *OpenStack) CreateBackup(name, volID, snapshotID, availabilityZone string, tags map[string]string) (*backups.Backup, error) {
	blockstorageServiceClient, err := openstack.NewBlockStorageV3(os.blockstorage.ProviderClient, os.epOpts)
	if err != nil {
		return &backups.Backup{}, err
	}

	force := false
	// if no flag given, then force will be false by default
	// if flag it given , check it
	if item, ok := (tags)[SnapshotForceCreate]; ok {
		var err error
		force, err = strconv.ParseBool(item)
		if err != nil {
			klog.V(5).Infof("Make force create flag to false due to: %v", err)
		}
		delete(tags, SnapshotForceCreate)
	}

	opts := &backups.CreateOpts{
		VolumeID:         volID,
		SnapshotID:       snapshotID,
		Name:             name,
		Force:            force,
		Description:      backupDescription,
		AvailabilityZone: availabilityZone,
	}

	if tags != nil {
		// Set openstack microversion to 3.51 to send metadata along with the backup
		blockstorageServiceClient.Microversion = "3.51"
		opts.Metadata = tags
	}

	// TODO: Do some check before really call openstack API on the input
	mc := metrics.NewMetricContext("backup", "create")
	backup, err := backups.Create(blockstorageServiceClient, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return &backups.Backup{}, err
	}
	// There's little value in rewrapping these gophercloud types into yet another abstraction/type, instead just
	// return the gophercloud item
	return backup, nil
}

// ListBackups retrieves a list of active backups from Cinder for the corresponding Tenant.  We also
// provide the ability to provide limit and offset to enable the consumer to provide accurate pagination.
// In addition the filters argument provides a mechanism for passing in valid filter strings to the list
// operation.  Valid filter keys are:  Name, Status, VolumeID, Limit, Marker (TenantID has no effect).
func (os *OpenStack) ListBackups(filters map[string]string) ([]backups.Backup, error) {
	var allBackups []backups.Backup

	// Build the Opts
	opts := backups.ListOpts{}
	for key, val := range filters {
		switch key {
		case "Status":
			opts.Status = val
		case "Name":
			opts.Name = val
		case "VolumeID":
			opts.VolumeID = val
		case "Marker":
			opts.Marker = val
		case "Limit":
			opts.Limit, _ = strconv.Atoi(val)
		default:
			klog.V(3).Infof("Not a valid filter key %s", key)
		}
	}
	mc := metrics.NewMetricContext("backup", "list")

	allPages, err := backups.List(os.blockstorage, opts).AllPages()
	if err != nil {
		return nil, err
	}
	allBackups, err = backups.ExtractBackups(allPages)
	if err != nil {
		return nil, err
	}

	if mc.ObserveRequest(err) != nil {
		return nil, err
	}

	return allBackups, nil
}

// DeleteBackup issues a request to delete the Backup with the specified ID from the Cinder backend.
func (os *OpenStack) DeleteBackup(backupID string) error {
	mc := metrics.NewMetricContext("backup", "delete")
	err := backups.Delete(os.blockstorage, backupID).ExtractErr()
	if mc.ObserveRequest(err) != nil {
		klog.Errorf("Failed to delete backup: %v", err)
	}
	return err
}

// GetBackupByID returns backup details by id.
func (os *OpenStack) GetBackupByID(backupID string) (*backups.Backup, error) {
	mc := metrics.NewMetricContext("backup", "get")
	backup, err := backups.Get(os.blockstorage, backupID).Extract()
	if mc.ObserveRequest(err) != nil {
		klog.Errorf("Failed to get backup: %v", err)
		return nil, err
	}
	return backup, nil
}

func (os *OpenStack) BackupsAreEnabled() (bool, error) {
	// TODO: Check if the backup service is enabled
	return true, nil
}

// WaitBackupReady waits until backup is ready. It waits longer depending on
// the size of the corresponding snapshot.
func (os *OpenStack) WaitBackupReady(backupID string, snapshotSize int, backupMaxDurationSecondsPerGB int) (string, error) {
	var err error

	duration := time.Duration(backupMaxDurationSecondsPerGB*snapshotSize + backupBaseDurationSeconds)

	err = os.waitBackupReadyWithContext(backupID, duration)
	if err == context.DeadlineExceeded {
		err = fmt.Errorf("timeout, Backup %s is still not Ready: %v", backupID, err)
	}

	back, _ := os.GetBackupByID(backupID)

	if back != nil {
		return back.Status, err
	} else {
		return "Failed to get backup status", err
	}
}

// Supporting function for WaitBackupReady().
// Allows for a timeout while waiting for the backup to be ready.
func (os *OpenStack) waitBackupReadyWithContext(backupID string, duration time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), duration*time.Second)
	defer cancel()
	var done bool
	var err error
	ticker := time.NewTicker(backupReadyCheckIntervalSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			done, err = os.backupIsReady(backupID)
			if err != nil {
				return err
			}

			if done {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

}

// Supporting function for waitBackupReadyWithContext().
// Returns true when the backup is ready.
func (os *OpenStack) backupIsReady(backupID string) (bool, error) {
	backup, err := os.GetBackupByID(backupID)
	if err != nil {
		return false, err
	}

	if backup.Status == backupErrorStatus {
		return false, errors.New("backup is in error state")
	}

	return backup.Status == backupReadyStatus, nil
}
