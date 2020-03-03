/*
Copyright 2018 The Kubernetes Authors.

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

package keystone

import (
	"fmt"
	"strings"
	"testing"

	th "github.com/gophercloud/gophercloud/testhelper"
)

func TestSyncConfigFromFile(t *testing.T) {
	sc, err := newSyncConfigFromFile("sync_test.yaml")
	th.AssertNoErr(t, err)
	th.AssertEquals(t, "prefix-%d-%n-%i-suffix", sc.NamespaceFormat)
	th.AssertEquals(t, "id1", sc.ProjectBlackList[0])
	th.AssertEquals(t, "id2", sc.ProjectBlackList[1])
	th.AssertEquals(t, "name1", sc.ProjectNameBlackList[0])
	th.AssertEquals(t, "name2", sc.ProjectNameBlackList[1])
	th.AssertEquals(t, 1, len(sc.RoleMaps))
	th.AssertEquals(t, "_member_", sc.RoleMaps[0].KeystoneRole)
	th.AssertEquals(t, "myuser", sc.RoleMaps[0].Username)
	th.AssertEquals(t, 1, len(sc.RoleMaps[0].Groups))
	th.AssertEquals(t, "mygroup", sc.RoleMaps[0].Groups[0])
}

func TestSyncConfigValidation(t *testing.T) {
	// Default sync config
	sc := newSyncConfig()
	err := sc.validate()
	th.AssertNoErr(t, err)

	// Forbidden characters in the format string
	sc.NamespaceFormat = strings.Join([]string{"%i", "!@#$"}, "")
	err = sc.validate()
	th.AssertEquals(
		t,
		"namespace name must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		err.Error(),
	)

	// Format string starts with "-"
	sc.NamespaceFormat = "-%i"
	err = sc.validate()
	th.AssertEquals(
		t,
		"namespace name must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		err.Error(),
	)

	// NamespaceFormat string doesn't have project id
	sc.NamespaceFormat = "%n"
	err = sc.validate()
	th.AssertEquals(t, "format string should comprise a %i substring (keystone project id)", err.Error())

	sc = newSyncConfig()

	// DataTypesToSync must contain only allowed types
	sc.DataTypesToSync = []string{"not_allowed_type"}
	err = sc.validate()
	th.AssertEquals(
		t,
		fmt.Sprintf(
			"Unsupported data type to sync: not_allowed_type. Available values: %v",
			strings.Join(allowedDataTypesToSync, ","),
		),
		err.Error(),
	)
}

func TestSyncRoles(t *testing.T) {
	sc, err := newSyncConfigFromFile("sync_test.yaml")
	th.AssertNoErr(t, err)

	syncer := Syncer{
		k8sClient:  nil,
		syncConfig: sc,
	}

	fakeName := "fake-user"
	fakeID := "b4db78f0-4dd7-41cf-8475-203c34230dc0"
	fakeGroups := []string{"_member_", "kube_viewer"}
	user1 := &userInfo{
		Username: fakeName,
		UID:      fakeID,
		Groups:   []string{fakeID},
		Extra:    map[string][]string{Roles: fakeGroups},
	}

	userModified := syncer.syncRoles(user1)

	th.AssertEquals(t, "myuser", userModified.Username)
	expectedGroups := []string{fakeID, "mygroup"}
	th.AssertDeepEquals(t, expectedGroups, userModified.Groups)
}

func TestSyncRolesSkipNilConfig(t *testing.T) {
	syncer := Syncer{
		k8sClient:  nil,
		syncConfig: nil,
	}

	fakeName := "fake-user"
	fakeID := "b4db78f0-4dd7-41cf-8475-203c34230dc0"
	fakeGroups := []string{"_member_", "kube_viewer"}
	user1 := &userInfo{
		Username: fakeName,
		UID:      fakeID,
		Groups:   []string{fakeID},
		Extra:    map[string][]string{Roles: fakeGroups},
	}

	userModified := syncer.syncRoles(user1)

	th.AssertEquals(t, userModified, user1)
}

func TestSyncRolesSkipm(t *testing.T) {
	sc, err := newSyncConfigFromFile("sync_test.yaml")
	th.AssertNoErr(t, err)

	sc.RoleMaps = []*roleMap{}
	syncer := Syncer{
		k8sClient:  nil,
		syncConfig: sc,
	}

	fakeName := "fake-user"
	fakeID := "b4db78f0-4dd7-41cf-8475-203c34230dc0"
	fakeGroups := []string{"_member_", "kube_viewer"}
	user1 := &userInfo{
		Username: fakeName,
		UID:      fakeID,
		Groups:   []string{fakeID},
		Extra:    map[string][]string{Roles: fakeGroups},
	}

	userModified := syncer.syncRoles(user1)

	th.AssertEquals(t, userModified, user1)
}
