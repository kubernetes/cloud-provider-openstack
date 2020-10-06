package shares

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/sharedfilesystems/v2/shares"
)

// IDFromName is a convenience function that returns a share's ID given its name.
func IDFromName(client *gophercloud.ServiceClient, name string) (string, error) {
	listOpts := shares.ListOpts{
		Name: name,
	}

	r, err := shares.ListDetail(client, listOpts).AllPages()
	if err != nil {
		return "", err
	}

	ss, err := shares.ExtractShares(r)
	if err != nil {
		return "", err
	}

	switch len(ss) {
	case 0:
		return "", gophercloud.ErrResourceNotFound{Name: name, ResourceType: "share"}
	case 1:
		return ss[0].ID, nil
	default:
		return "", gophercloud.ErrMultipleResourcesFound{Name: name, Count: len(ss), ResourceType: "share"}
	}
}
