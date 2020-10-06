package snapshots

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/snapshots"
)

// IDFromName is a convenience function that returns a snapshot's ID given its name.
func IDFromName(client *gophercloud.ServiceClient, name string) (string, error) {
	listOpts := snapshots.ListOpts{
		Name: name,
	}

	r, err := snapshots.ListDetail(client, listOpts).AllPages()
	if err != nil {
		return "", err
	}

	ss, err := snapshots.ExtractSnapshots(r)
	if err != nil {
		return "", err
	}

	switch len(ss) {
	case 0:
		return "", gophercloud.ErrResourceNotFound{Name: name, ResourceType: "snapshot"}
	case 1:
		return ss[0].ID, nil
	default:
		return "", gophercloud.ErrMultipleResourcesFound{Name: name, Count: len(ss), ResourceType: "snapshot"}
	}
}
