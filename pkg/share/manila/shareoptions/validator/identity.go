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
)

const (
	nameFieldTag = "name"
)

type (
	fieldIndex int
	fieldName  string
	structName string

	nameIndexMap map[fieldName]fieldIndex
	indexNameMap map[fieldIndex]fieldName
)

func buildNameIdxMap(t reflect.Type) (nameIndexMap, indexNameMap) {
	nameIdxMap := make(nameIndexMap)
	idxNameMap := make(indexNameMap)

	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		tags := ft.Tag

		name, ok := tags.Lookup(nameFieldTag)
		if !ok {
			panic(fmt.Sprintf("missing name tag for field %s in struct %s", ft.Name, t.Name()))
		}

		nameIdxMap[fieldName(name)] = fieldIndex(i)
		idxNameMap[fieldIndex(i)] = fieldName(name)
	}

	return nameIdxMap, idxNameMap
}
