package util

import (
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	GIB = 1024 * 1024 * 1024
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
