/*
Copyright 2019 The Kubernetes Authors.

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
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

func TestUserAgentFlag(t *testing.T) {
	tests := []struct {
		name        string
		shouldParse bool
		flags       []string
		expected    []string
	}{
		{"no_flag", true, []string{}, nil},
		{"one_flag", true, []string{"--user-agent=cluster/abc-123"}, []string{"cluster/abc-123"}},
		{"multiple_flags", true, []string{"--user-agent=a/b", "--user-agent=c/d"}, []string{"a/b", "c/d"}},
		{"flag_with_space", true, []string{"--user-agent=a b"}, []string{"a b"}},
		{"flag_split_with_space", true, []string{"--user-agent=a", "b"}, []string{"a"}},
		{"empty_flag", false, []string{"--user-agent"}, nil},
	}

	for _, testCase := range tests {
		userAgentData = []string{}

		t.Run(testCase.name, func(t *testing.T) {
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			AddExtraFlags(fs)

			err := fs.Parse(testCase.flags)

			if testCase.shouldParse && err != nil {
				t.Errorf("Flags failed to parse")
			} else if !testCase.shouldParse && err == nil {
				t.Errorf("Flags should not have parsed")
			} else if testCase.shouldParse {
				if !reflect.DeepEqual(userAgentData, testCase.expected) {
					t.Errorf("userAgentData %#v did not match expected value %#v", userAgentData, testCase.expected)
				}
			}
		})
	}
}
