/*
Copyright 2018 The Kubernetes Authors.

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

package validator

import (
	"fmt"
	"reflect"
	"regexp"
)

const (
	matchTag = "matches"
)

type (
	matchRegexpMap map[fieldIndex]*regexp.Regexp
)

func buildMatchRegexMap(t reflect.Type) matchRegexpMap {
	m := make(matchRegexpMap)

	for i := 0; i < t.NumField(); i++ {
		if pattern, ok := t.Field(i).Tag.Lookup(matchTag); ok {
			m[fieldIndex(i)] = regexp.MustCompile(pattern)
		}
	}

	return m
}

func valueSatisfiesPattern(value string, rx *regexp.Regexp, fName fieldName) error {
	if !rx.MatchString(value) {
		return fmt.Errorf("parameter '%s' does not match pattern %s", fName, rx.String())
	}

	return nil
}
