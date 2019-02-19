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

// Validator validates input data and stores it in the output struct.
// The output struct may contain only string fields, where each field
// must be decorated with a `name` tag. An input value with matching name
// is then stored in its corresponding struct field.
// By default, the input data has to contain a value for each struct field.
//
// Available tags:
// * name:"FIELD-NAME" : key of the value in the input map
// * value: modifies value requirements:
//   value:"required" : field is required
//   value:"optional" : field is optional
//   value:"requiredIf:FIELD-NAME=REGEXP-PATTERN" : field is required if the value of FIELD-NAME matches REGEXP-PATTERN
//   value:"optionalIf:FIELD-NAME=REGEXP-PATTERN" : field is optional if the value of FIELD-NAME matches REGEXP-PATTERN
//   value:"default:VALUE" : field value defaults to VALUE
// * dependsOn:"FIELD-NAMES|,..." : if this field is not empty, the specified fields are required to be present
//   operator ',' acts as AND
//   operator '|' acts as XOR
//   e.g.: dependsOn:"f1|f2|f3,f4,f5" : if this field is not empty, exactly one of {f1,f2,f3} is required to be present,
//         and f4 and f5 is required to be present
// * precludes:"FIELD-NAMES,..." : if this field is not empty, all specified fields are required to be empty
// * matches:"REGEXP-PATTERN" : if this field is not empty, it's required to match REGEXP-PATTERN
type Validator struct {
	t reflect.Type

	nameIdxMap nameIndexMap
	idxNameMap indexNameMap

	valueExprs *valueExpressions
	depsMap    dependenciesMap
	preclsMap  preclusionsMap
	matchMap   matchRegexpMap
}

// New creates a new instance of Validator.
// stringStruct is the struct used for validation,
// must contain only string fields, must be the same type used in Validator.Populate.
// May panic on invalid struct tags.
func New(stringStruct interface{}) *Validator {
	t := reflect.TypeOf(stringStruct).Elem()

	nameIdxMap, idxNameMap := buildNameIdxMap(t)

	return &Validator{
		t:          t,
		nameIdxMap: nameIdxMap,
		idxNameMap: idxNameMap,
		valueExprs: buildValueExpressions(t, idxNameMap, nameIdxMap),
		depsMap:    buildDependenciesMap(t, idxNameMap, nameIdxMap),
		preclsMap:  buildPreclusionsMap(t, idxNameMap, nameIdxMap),
		matchMap:   buildMatchRegexMap(t),
	}
}

// Populate validates input data and populates the output struct.
func (v *Validator) Populate(data map[string]string, out interface{}) error {
	if reflect.TypeOf(out).Elem() != v.t {
		panic("destination type mismatch")
	}

	vOut := reflect.ValueOf(out).Elem()

	// Initialize unset entries with their default values

	for fIdx, defaultValue := range v.valueExprs.defaultVal {
		name := string(v.idxNameMap[fIdx])
		if _, ok := data[name]; !ok {
			data[name] = defaultValue
		}
	}

	// Populate the values

	for fIdx, fName := range v.idxNameMap {
		value, ok := data[string(fName)]
		if ok {
			if value == "" {
				return fmt.Errorf("parameter '%s' cannot be empty", fName)
			}

			// The value is present, populate the field and continue with the next one

			vOut.Field(int(fIdx)).SetString(value)
			continue
		}

		// Value not present, determine whether this field is required

		if requiresValue(fIdx, v.valueExprs, data) {
			return fmt.Errorf("missing required parameter '%s'", fName)
		}
	}

	// Ensure requirements

	for fIdx, deps := range v.depsMap {
		if vOut.Field(int(fIdx)).String() != "" {
			if err := dependenciesSatisfied(fIdx, vOut, deps, v.idxNameMap); err != nil {
				return err
			}
		}
	}

	// Ensure preclusions

	for fIdx, precls := range v.preclsMap {
		if vOut.Field(int(fIdx)).String() != "" {
			if err := preclusionsSatisfied(fIdx, vOut, precls, v.idxNameMap); err != nil {
				return err
			}
		}
	}

	// Ensure the values match their regexp patterns

	for fIdx, rx := range v.matchMap {
		if vOut.Field(int(fIdx)).String() != "" {
			if err := valueSatisfiesPattern(vOut.Field(int(fIdx)).String(), rx, v.idxNameMap[fIdx]); err != nil {
				return err
			}
		}
	}

	return nil
}
