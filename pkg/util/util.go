package util

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientset "k8s.io/client-go/kubernetes"
)

// MyDuration is the encoding.TextUnmarshaler interface for time.Duration
type MyDuration struct {
	time.Duration
}

// UnmarshalText is used to convert from text to Duration
func (d *MyDuration) UnmarshalText(text []byte) error {
	res, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = res
	return nil
}

// StringListEqual compares two string list, returns true if they have the same items, order doesn't matter
func StringListEqual(list1, list2 []string) bool {
	if len(list1) == 0 && len(list2) == 0 {
		return true
	}

	if len(list1) != len(list2) {
		return false
	}

	s1 := sets.String{}
	for _, s := range list1 {
		s1.Insert(s)
	}
	s2 := sets.String{}
	for _, s := range list2 {
		s2.Insert(s)
	}

	return s1.Equal(s2)
}

// Contains searches if a string list contains the given string or not.
func Contains(list []string, strToSearch string) bool {
	for _, item := range list {
		if item == strToSearch {
			return true
		}
	}
	return false
}

// RoundUpSize calculates how many allocation units are needed to accommodate
// a volume of given size. E.g. when user wants 1500MiB volume, while AWS EBS
// allocates volumes in gibibyte-sized chunks,
// RoundUpSize(1500 * 1024*1024, 1024*1024*1024) returns '2'
// (2 GiB is the smallest allocatable volume that can hold 1500MiB)
func RoundUpSize(volumeSizeBytes int64, allocationUnitBytes int64) int64 {
	roundedUp := volumeSizeBytes / allocationUnitBytes
	if volumeSizeBytes%allocationUnitBytes > 0 {
		roundedUp++
	}
	return roundedUp
}

// PatchService makes patch request to the Service object.
func PatchService(ctx context.Context, client clientset.Interface, cur, mod *v1.Service) error {
	curJSON, err := json.Marshal(cur)
	if err != nil {
		return fmt.Errorf("failed to serialize current service object: %s", err)
	}

	modJSON, err := json.Marshal(mod)
	if err != nil {
		return fmt.Errorf("failed to serialize modified service object: %s", err)
	}

	patch, err := strategicpatch.CreateTwoWayMergePatch(curJSON, modJSON, v1.Service{})
	if err != nil {
		return fmt.Errorf("failed to create 2-way merge patch: %s", err)
	}
	if len(patch) == 0 || string(patch) == "{}" {
		return nil
	}
	_, err = client.CoreV1().Services(cur.Namespace).Patch(ctx, cur.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch service object %s/%s: %s", cur.Namespace, cur.Name, err)
	}

	return nil
}
