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
	dependsTag = "dependsOn"

	depsGroupDelim     = ","
	depsExclGroupDelim = "|"
)

type (
	exclusiveDependsExpression []fieldIndex
	dependsExpression          []exclusiveDependsExpression
	dependenciesMap            map[fieldIndex]dependsExpression
)

func parseDepsExpr(deps string, nameIdxMap nameIndexMap, s structName, f fieldName) dependsExpression {
	groups := strings.Split(deps, depsGroupDelim)
	depsExpr := make(dependsExpression, len(groups))

	for i := range groups {
		exclGroup := strings.Split(groups[i], depsExclGroupDelim)
		depsExpr[i] = make(exclusiveDependsExpression, len(exclGroup))

		for j := range depsExpr[i] {
			fIdx, ok := nameIdxMap[fieldName(exclGroup[j])]
			if !ok {
				panic(fmt.Sprintf("invalid dependsOn expression in %s.%s: no field named '%s' found",
					s, f, fieldName(exclGroup[j])))
			}

			depsExpr[i][j] = fIdx
		}
	}

	return depsExpr
}

func buildDependenciesMap(t reflect.Type, idxNameMap indexNameMap, nameIdxMap nameIndexMap) dependenciesMap {
	m := make(dependenciesMap)

	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)

		deps, ok := ft.Tag.Lookup(dependsTag)
		if !ok {
			continue
		}

		fIdx := fieldIndex(i)
		m[fIdx] = parseDepsExpr(deps, nameIdxMap, structName(t.Name()), idxNameMap[fIdx])
	}

	return m
}

func dependenciesUnsatisfiedError(i fieldIndex, exclDeps exclusiveDependsExpression, idxNameMap indexNameMap) error {
	if len(exclDeps) == 1 {
		return fmt.Errorf("parameter '%s' requires '%s'", idxNameMap[i], idxNameMap[exclDeps[0]])
	}

	names := make([]fieldName, len(exclDeps))
	for i := range names {
		names[i] = idxNameMap[exclDeps[i]]
	}

	return fmt.Errorf("parameter '%s' requires exactly one of %v parameters", idxNameMap[i], names)
}

func dependenciesSatisfied(fIdx fieldIndex, v reflect.Value, deps dependsExpression, idxNameMap indexNameMap) error {
	for i := range deps {
		var values int

		for j := range deps[i] {
			if v.Field(int(deps[i][j])).String() != "" {
				values++
				if values > 1 {
					break
				}
			}
		}

		if values != 1 {
			return dependenciesUnsatisfiedError(fIdx, deps[i], idxNameMap)
		}
	}

	return nil
}
