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
	"strings"
)

const (
	precludesTag = "precludes"

	preclsDelim = ","
)

type (
	precludesExpression []fieldIndex
	preclusionsMap      map[fieldIndex]precludesExpression
)

func parsePreclsExpr(precls string, nameIdxMap nameIndexMap, s structName, f fieldName) precludesExpression {
	ps := strings.Split(precls, preclsDelim)
	preclsExpr := make(precludesExpression, len(ps))

	for i := range ps {
		fIdx, ok := nameIdxMap[fieldName(ps[i])]
		if !ok {
			panic(fmt.Sprintf("invalid precludes expression in '%s.%s': no field named '%s' found",
				s, f, fieldName(ps[i])))
		}

		preclsExpr[i] = fIdx
	}

	return preclsExpr
}

func buildPreclusionsMap(t reflect.Type, idxNameMap indexNameMap, nameIdxMap nameIndexMap) preclusionsMap {
	m := make(preclusionsMap)

	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)

		precls, ok := ft.Tag.Lookup(precludesTag)
		if !ok {
			continue
		}

		fIdx := fieldIndex(i)
		m[fIdx] = parsePreclsExpr(precls, nameIdxMap, structName(t.Name()), idxNameMap[fIdx])
	}

	return m
}

func preclusionsSatisfied(fIdx fieldIndex, v reflect.Value, precls precludesExpression, idxNameMap indexNameMap) error {
	for i := range precls {
		if v.Field(int(precls[i])).String() != "" {
			return fmt.Errorf("parameter '%s' forbids '%s' to be set", idxNameMap[fIdx], idxNameMap[precls[i]])
		}
	}

	return nil
}
