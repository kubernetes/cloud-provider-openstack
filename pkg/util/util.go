package util

import (
	"k8s.io/apimachinery/pkg/util/sets"
)

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
