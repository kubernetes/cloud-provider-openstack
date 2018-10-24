package openstack

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

const (
	defaultMetadataVersion = "2012-08-10"
	metadataURLTemplate    = "http://169.254.169.254/openstack/%s/meta_data.json"
)

type metadata struct {
	UUID string
}

func getMetadata(metadataURL string) ([]byte, error) {
	resp, err := http.Get(metadataURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, err
	}

	md, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return md, nil
}

// GetInstanceID from metadata service
func GetInstanceID() (string, error) {
	metadataURL := fmt.Sprintf(metadataURLTemplate, defaultMetadataVersion)
	md, err := getMetadata(metadataURL)
	if err != nil {
		return "", err
	}
	var m metadata
	err = json.Unmarshal(md, &m)
	if err != nil {
		return "", err
	}

	return m.UUID, nil
}
