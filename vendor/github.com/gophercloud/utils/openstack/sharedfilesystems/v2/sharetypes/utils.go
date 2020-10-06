package sharetypes

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/sharetypes"
)

// IDFromName is a convenience function that returns a share-type's ID given its name.
func IDFromName(client *gophercloud.ServiceClient, name string) (string, error) {
	r, err := sharetypes.List(client, nil).AllPages()
	if err != nil {
		return "", nil
	}

	ss, err := sharetypes.ExtractShareTypes(r)
	if err != nil {
		return "", err
	}

	var (
		count int
		id    string
	)

	for _, s := range ss {
		if s.Name == name {
			count++
			id = s.ID
		}
	}

	switch count {
	case 0:
		return "", gophercloud.ErrResourceNotFound{Name: name, ResourceType: "share type"}
	case 1:
		return id, nil
	default:
		return "", gophercloud.ErrMultipleResourcesFound{Name: name, Count: count, ResourceType: "share type"}
	}
}
