package util

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientset "k8s.io/client-go/kubernetes"
)

// CutString255 makes sure the string length doesn't exceed 255, which is usually the maximum string length in OpenStack.
func CutString255(original string) string {
	ret := original
	if len(original) > 255 {
		ret = original[:255]
	}
	return ret
}

// Sprintf255 formats according to a format specifier and returns the resulting string with a maximum length of 255 characters.
func Sprintf255(format string, args ...interface{}) string {
	return CutString255(fmt.Sprintf(format, args...))
}

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

	s1 := sets.New[string]()
	for _, s := range list1 {
		s1.Insert(s)
	}
	s2 := sets.New[string]()
	for _, s := range list2 {
		s2.Insert(s)
	}

	return s1.Equal(s2)
}

// StringToMap converts a string of comma-separated key-values into a map
func StringToMap(str string) map[string]string {
	// break up a "key1=val,key2=val2,key3=,key4" string into a list
	values := strings.Split(strings.TrimSpace(str), ",")
	keyValues := make(map[string]string, len(values))

	for _, kv := range values {
		kv := strings.SplitN(strings.TrimSpace(kv), "=", 2)

		k := kv[0]
		if len(kv) == 1 {
			if k != "" {
				// process "key=" or "key"
				keyValues[k] = ""
			}
			continue
		}

		// process "key=val" or "key=val=foo"
		keyValues[k] = kv[1]
	}

	return keyValues
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
		return fmt.Errorf("failed to serialize current service object: %v", err)
	}

	modJSON, err := json.Marshal(mod)
	if err != nil {
		return fmt.Errorf("failed to serialize modified service object: %v", err)
	}

	patch, err := strategicpatch.CreateTwoWayMergePatch(curJSON, modJSON, v1.Service{})
	if err != nil {
		return fmt.Errorf("failed to create 2-way merge patch: %v", err)
	}
	if len(patch) == 0 || string(patch) == "{}" {
		return nil
	}
	_, err = client.CoreV1().Services(cur.Namespace).Patch(ctx, cur.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch service object %s/%s: %v", cur.Namespace, cur.Name, err)
	}

	return nil
}

func SanitizeLabel(input string) string {
	// Replace non-alphanumeric characters (except '-', '_', '.') with '-'
	reg := regexp.MustCompile(`[^-a-zA-Z0-9_.]+`)
	sanitized := reg.ReplaceAllString(input, "-")

	// Ensure the label starts and ends with an alphanumeric character
	sanitized = strings.Trim(sanitized, "-_.")

	// Ensure the label is not longer than 63 characters
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	return sanitized
}

// SetMapIfNotEmpty sets the value of the key in the provided map if the value
// is not empty (i.e., it is not the zero value for that type) and returns a
// pointer to the new map. If the map is nil, it will be initialized with a new
// map.
func SetMapIfNotEmpty[K comparable, V comparable](m map[K]V, key K, value V) map[K]V {
	// Check if the value is the zero value for its type
	var zeroValue V
	if value == zeroValue {
		return m
	}

	// Initialize the map if it's nil
	if m == nil {
		m = make(map[K]V)
	}

	// Set the value in the map
	m[key] = value

	return m
}

// SplitTrim splits a string of values separated by sep rune into a slice of
// strings with trimmed spaces.
func SplitTrim(s string, sep rune) []string {
	f := func(c rune) bool {
		return unicode.IsSpace(c) || c == sep
	}
	return strings.FieldsFunc(s, f)
}

// UUID converts a string to a valid UUID string.
func UUID(s string) (string, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
