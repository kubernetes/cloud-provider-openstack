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
)

const (
	nameFieldTag     = "name"
	protocolFieldTag = "protocol"
	backendFieldTag  = "backend"
)

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

func extractParam(name string, params map[string]string) (string, error) {
	value, found := params[name]

	if !found {
		return "", fmt.Errorf("missing required parameter %s", name)
	}

	if value == "" {
		return "", fmt.Errorf("parameter %s cannot be empty", name)
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

		name, hasName := ft.Tag.Lookup(nameFieldTag)
		if !hasName {
			panic(fmt.Sprintf("missing name tag for field %s in struct %s", ft.Name, ft.Type.Name()))
		}

		if !c.constraintsMet(ft.Tag) {
			continue
		}

		value, err := extractParam(name, params)
		if err != nil && !c.allOptional {
			return 0, err
		}

		fv.SetString(value)
		n++
	}

	return n, nil
}
