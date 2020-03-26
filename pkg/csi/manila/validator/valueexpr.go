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
	valueTag           = "value"
	valueRequiredTag   = "required"
	valueRequiredIfTag = "requiredIf:"
	valueOptionalTag   = "optional"
	valueOptionalIfTag = "optionalIf:"
	valueDefaultTag    = "default:"
	valueCoalesceTag   = "coalesce:"

	valueExprDelim = '='
)

type valueExprType int

const (
	valueRequired valueExprType = iota + 1
	valueRequiredIf
	valueOptional
	valueOptionalIf
	valueDefault
)

type (
	valueExpression struct {
		exprType valueExprType
		fName    fieldName
		arg      string
	}

	valueConditionalExpression struct {
		r         *regexp.Regexp
		condField fieldName
	}

	valueRequiredMap    map[fieldIndex]bool
	valueConditionalMap map[fieldIndex]valueConditionalExpression
	valueDefaultMap     map[fieldIndex]string

	valueExpressions struct {
		required     valueRequiredMap
		condRequired valueConditionalMap
		condOptional valueConditionalMap
		defaultVal   valueDefaultMap
	}
)

func newValueExpressions() *valueExpressions {
	return &valueExpressions{
		required:     make(valueRequiredMap),
		condRequired: make(valueConditionalMap),
		condOptional: make(valueConditionalMap),
		defaultVal:   make(valueDefaultMap),
	}
}

func checkValueExpr(value, valueExprTag string) bool {
	return len(value) > len(valueExprTag) && value[:len(valueExprTag)] == valueExprTag
}

func invalidValueExprError(msg, value, sName string, fName fieldName) error {
	return fmt.Errorf("invalid value expression '%s' in '%s.%s': %s", value, sName, fName, msg)
}

func parseCondValueExpr(value, tag, sName string, nameIdxMap nameIndexMap, fName fieldName) (fieldName, string) {
	n, pattern, ok := splitOne(value[len(tag):], valueExprDelim)
	targetField := fieldName(n)

	if targetField == fName {
		panic(invalidValueExprError(fmt.Sprintf("field '%s' is not allowed in the expression", fName),
			value, sName, fName))
	}

	if !ok {
		panic(invalidValueExprError("parsing failed", value, sName, fName))
	}

	if _, ok := nameIdxMap[targetField]; !ok {
		panic(invalidValueExprError(fmt.Sprintf("no field named '%s' found", targetField), value, sName, fName))
	}

	return targetField, pattern
}

func parseValueExpr(value string, selfStructName string, fName fieldName, nameIdxMap nameIndexMap) valueExpression {
	var expr valueExpression

	if value == valueOptionalTag {
		expr.exprType = valueOptional
	} else if value == valueRequiredTag {
		expr.exprType = valueRequired
	} else if checkValueExpr(value, valueDefaultTag) {
		expr.exprType = valueDefault
		expr.arg = value[len(valueDefaultTag):]
	} else if checkValueExpr(value, valueRequiredIfTag) {
		expr.exprType = valueRequiredIf
		expr.fName, expr.arg = parseCondValueExpr(value, valueRequiredIfTag, selfStructName, nameIdxMap, fName)
	} else if checkValueExpr(value, valueOptionalIfTag) {
		expr.exprType = valueOptionalIf
		expr.fName, expr.arg = parseCondValueExpr(value, valueOptionalIfTag, selfStructName, nameIdxMap, fName)
	} else {
		panic(invalidValueExprError(fmt.Sprintf("unrecognized value expression"), value, selfStructName, fName))
	}

	return expr
}

func buildValueExpressions(t reflect.Type, idxNameMap indexNameMap, nameIdxMap nameIndexMap) *valueExpressions {
	vs := newValueExpressions()

	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)

		value, ok := ft.Tag.Lookup(valueTag)
		if !ok {
			continue
		}

		fIdx := fieldIndex(i)

		expr := parseValueExpr(value, t.Name(), idxNameMap[fieldIndex(i)], nameIdxMap)
		switch expr.exprType {
		case valueOptional:
			vs.required[fIdx] = false
		case valueRequired:
			vs.required[fIdx] = true
		case valueDefault:
			vs.defaultVal[fIdx] = expr.arg
		case valueRequiredIf:
			vs.condRequired[fIdx] = valueConditionalExpression{condField: expr.fName, r: regexp.MustCompile(expr.arg)}
		case valueOptionalIf:
			vs.condOptional[fIdx] = valueConditionalExpression{condField: expr.fName, r: regexp.MustCompile(expr.arg)}
		}
	}

	return vs
}

func resolveConditionalReq(condExpr valueConditionalExpression, data map[string]string) bool {
	condValue, ok := data[string(condExpr.condField)]
	if !ok {
		return false
	}

	return condExpr.r.MatchString(condValue)
}

func requiresValue(i fieldIndex, ve *valueExpressions, data map[string]string) bool {
	if required, ok := ve.required[i]; ok {
		return required
	}

	if condExpr, ok := ve.condRequired[i]; ok {
		return resolveConditionalReq(condExpr, data)
	}

	if condExpr, ok := ve.condOptional[i]; ok {
		return !resolveConditionalReq(condExpr, data)
	}

	// By default, all fields are required

	return true
}
