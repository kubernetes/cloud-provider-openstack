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

package shareoptions

import (
	"fmt"
	"reflect"
	"strings"
)

const (
	// Standalone field tags

	nameFieldTag     = "name"     // Option name, used for parsing from input parameters
	protocolFieldTag = "protocol" // Protocol constraint
	backendFieldTag  = "backend"  // Backend constraint
	valueFieldTag    = "value"    // Value options

	// Value tags passed in value:"..."

	optionalValueTag = "optional"  // Skip non-empty checks if empty
	defaultValueTag  = "default="  // Set the value to default=VALUE if empty
	coalesceValueTag = "coalesce=" // Set the value to the value of coalesce=OPTION_NAME if empty
	// This field is treated as optional if empty. Otherwise the following rules apply simultaneously:
	// Example: `name:"x" value:"requires=a|b|c,d,e,f|g"`
	// * exactly one of (a,b,c) must be non-empty
	// * d and e must be not-empty
	// * exactly one of (f,g) must be non-empty
	requiresValueTag = "requires="

	requiresValueGroupSeparator     = ","
	requiresValueExclGroupSeparator = "|"
)

type (
	fieldIndex int
	optionName string
	structName string

	nameIndexMap map[optionName]fieldIndex

	requirements struct {
		idx  fieldIndex     // Index of the field which states these requiremenets
		opts [][]fieldIndex // Requirements: Exactly one of opts[i][:] must be non-empty
	}

	requirementsMap map[optionName]requirements
	coalesceMap     map[fieldIndex]fieldIndex
)

var (
	nameIdxMap = make(map[structName]nameIndexMap)
	reqsMap    = make(map[structName]requirementsMap)
	coalMap    = make(map[structName]coalesceMap)
)

func processStruct(opts interface{}) {
	t := reflect.TypeOf(opts).Elem()
	sName := structName(t.Name())

	nameIdxMap[sName] = buildNameIndexMapFor(t)
	reqsMap[sName] = buildRequirementsFor(t)
	coalMap[sName] = buildCoalesceMapFor(t)
}

// Create mapping between option names and field indices
func buildNameIndexMapFor(t reflect.Type) nameIndexMap {
	m := make(nameIndexMap)

	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		tags := ft.Tag

		name, ok := tags.Lookup(nameFieldTag)
		if !ok {
			panic(fmt.Sprintf("missing name tag for field %s in struct %s", ft.Name, t.Name()))
		}

		m[optionName(name)] = fieldIndex(i)
	}

	return m
}

func parseRequiresTag(t string, indices nameIndexMap) [][]fieldIndex {
	groups := strings.Split(t, requiresValueGroupSeparator)
	opts := make([][]fieldIndex, len(groups))

	for i := range groups {
		exclGroup := strings.Split(groups[i], requiresValueExclGroupSeparator)
		opts[i] = make([]fieldIndex, len(exclGroup))

		for j := range exclGroup {
			opts[i][j] = indices[optionName(exclGroup[j])]
		}
	}

	return opts
}

func buildRequirementsFor(t reflect.Type) requirementsMap {
	m := make(requirementsMap)
	idxMap := nameIdxMap[structName(t.Name())]

	for name, idx := range idxMap {
		// Check for value:"requires=..." tags

		tags := t.Field(int(idx)).Tag
		value, ok := tags.Lookup(valueFieldTag)
		if !ok {
			continue
		}

		if !checkValueMode(value, requiresValueTag) {
			continue
		}

		// Parse the tag and store the result
		m[name] = requirements{idx: idx, opts: parseRequiresTag(value[len(requiresValueTag):], idxMap)}
	}

	return m
}

func buildCoalesceMapFor(t reflect.Type) coalesceMap {
	m := make(coalesceMap)
	idxMap := nameIdxMap[structName(t.Name())]

	// Check for value:"coalesce=..." tags
	for _, idx := range idxMap {
		tags := t.Field(int(idx)).Tag
		value, ok := tags.Lookup(valueFieldTag)
		if !ok {
			continue
		}

		if !checkValueMode(value, coalesceValueTag) {
			continue
		}

		m[idx] = idxMap[optionName(value[len(coalesceValueTag):])]
	}

	return m
}

func exclGroupUnsatisfiedError(name optionName, exclGroup []fieldIndex, t reflect.Type) error {
	if len(exclGroup) == 1 {
		return fmt.Errorf("parameter '%s' requires '%s'", name, t.Field(int(exclGroup[0])).Tag.Get(nameFieldTag))
	}

	names := make([]string, len(exclGroup))
	for i := range exclGroup {
		names[i] = t.Field(int(exclGroup[i])).Tag.Get(nameFieldTag)
	}

	return fmt.Errorf("parameter '%s' requires exactly one of %v parameters", name, names)
}

// Validate one-of requirement
// Example: value:"a|b|c" exactly one of (a,b,c) has to be non-empty
func validateExclusiveGroup(exclGroup []fieldIndex, t reflect.Type, v reflect.Value, name optionName) error {
	var hasValue bool

	for _, idx := range exclGroup {
		if v.Field(int(idx)).String() != "" {
			if hasValue {
				return exclGroupUnsatisfiedError(name, exclGroup, t)
			}

			hasValue = true
		}
	}

	if !hasValue {
		return exclGroupUnsatisfiedError(name, exclGroup, t)
	}

	return nil
}

// Validate value:"requires=..."
func validateRequirements(t reflect.Type, v reflect.Value) error {
	rm, ok := reqsMap[structName(t.Name())]
	if !ok {
		return nil
	}

	for name, req := range rm {
		if v.Field(int(req.idx)).String() == "" {
			// Skip empty parameters
			continue
		}

		for _, exclGroup := range req.opts {
			if err := validateExclusiveGroup(exclGroup, t, v, name); err != nil {
				return err
			}
		}
	}

	return nil
}

func coalesceOptions(t reflect.Type, v reflect.Value) {
	cm, ok := coalMap[structName(t.Name())]
	if !ok {
		return
	}

	for dst, src := range cm {
		dstField := v.Field(int(dst))
		if dstField.String() == "" {
			dstField.SetString(v.Field(int(src)).String())
		}
	}
}

func checkValueMode(valueTag, mode string) bool {
	if len(valueTag) < len(mode) {
		return false
	}

	return valueTag[:len(mode)] == mode
}

type optionConstraints struct {
	protocol, backend string
	allOptional       bool
}

func (c *optionConstraints) constraintsMet(tag reflect.StructTag) bool {
	return c.constrainBy(&optionConstraints{
		protocol: tag.Get(protocolFieldTag),
		backend:  tag.Get(backendFieldTag),
	})
}

func (c *optionConstraints) constrainBy(oc *optionConstraints) bool {
	if oc.protocol != "" && oc.protocol != c.protocol {
		return false
	}

	if oc.backend != "" && oc.backend != c.backend {
		return false
	}

	return true
}

// Parses valueTag and checks for all of its modes
// Returns false if not specified otherwise - indicating it shouldn't skip any further checks.
// In case "default=*" is non-empty, it will set *value to that default value in case *value is empty.
// In case "coalesce=*" is non-empty, it will return true if *value is empty as coalescing is done separately in coalesceOptions().
// In case "requires=*" is non-empty, it will return true if *value is empty.
// In case "optional" is non-empty, it will return true if *value is empty.
func handleValueOrSkip(name string, params map[string]string, valueTag string) bool {
	if valueTag == "" {
		return false
	}

	var (
		skip         bool
		defaultValue string
	)

	if valueTag == optionalValueTag {
		skip = true
	} else if checkValueMode(valueTag, defaultValueTag) {
		defaultValue = valueTag[len(defaultValueTag):]
	} else if checkValueMode(valueTag, coalesceValueTag) {
		// Field coalescing is done as a final step, to ensure all default values are in place.
		// For now, we'll skip such fields.
		skip = true
	} else if checkValueMode(valueTag, requiresValueTag) {
		skip = true
	} else {
		panic(fmt.Sprintf("invalid value tag '%s'", valueTag))
	}

	if params[name] == "" {
		if skip {
			return true
		}

		if defaultValue != "" {
			params[name] = defaultValue
		}
	}

	return false
}

func extractParam(name string, params map[string]string) (string, error) {
	value, found := params[name]

	if !found {
		return "", fmt.Errorf("missing required parameter '%s'", name)
	}

	if value == "" {
		return "", fmt.Errorf("parameter '%s' cannot be empty", name)
	}

	return value, nil
}

func extractParams(c *optionConstraints, params map[string]string, opts interface{}) (int, error) {
	t := reflect.TypeOf(opts).Elem()
	v := reflect.ValueOf(opts).Elem()
	n := 0

	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		fv := v.Field(i)

		name := ft.Tag.Get(nameFieldTag)

		if !c.constraintsMet(ft.Tag) || handleValueOrSkip(name, params, ft.Tag.Get(valueFieldTag)) {
			n++
			continue
		}

		value, err := extractParam(name, params)
		if err != nil && !c.allOptional {
			return 0, err
		}

		fv.SetString(value)
		n++
	}

	coalesceOptions(t, v)

	if err := validateRequirements(t, v); err != nil {
		return 0, err
	}

	return n, nil
}
