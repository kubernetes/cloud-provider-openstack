package groups

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
)

// IDFromName is a convenience function that returns a security group's ID,
// given its name.
func IDFromName(client *gophercloud.ServiceClient, name string) (string, error) {
	count := 0
	id := ""

	listOpts := groups.ListOpts{
		Name: name,
	}

	pages, err := groups.List(client, listOpts).AllPages()
	if err != nil {
		return "", err
	}

	all, err := groups.ExtractGroups(pages)
	if err != nil {
		return "", err
	}

	for _, s := range all {
		if s.Name == name {
			count++
			id = s.ID
		}
	}

	switch count {
	case 0:
		return "", gophercloud.ErrResourceNotFound{Name: name, ResourceType: "security group"}
	case 1:
		return id, nil
	default:
		return "", gophercloud.ErrMultipleResourcesFound{Name: name, Count: count, ResourceType: "security group"}
	}
}
