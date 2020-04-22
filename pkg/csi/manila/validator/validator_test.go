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

import "testing"

func TestValueRequired(t *testing.T) {
	type s1 struct {
		A string `name:"a"`
	}

	if New(&s1{}).Populate(map[string]string{}, &s1{}) == nil {
		t.Error(`value:"required" should be enabled by default`)
	}

	type s2 struct {
		A string `name:"a" value:"required"`
	}

	if New(&s2{}).Populate(map[string]string{}, &s2{}) == nil {
		t.Error(`value:"required" violated, should fail on missing parameter`)
	}
}

func TestValueOptional(t *testing.T) {
	type s struct {
		A string `name:"a" value:"optional"`
	}

	if New(&s{}).Populate(map[string]string{}, &s{}) != nil {
		t.Error(`value:"optional" violated, should permit missing parameter`)
	}
}

func TestValueRequiredIf(t *testing.T) {
	type s struct {
		A string `name:"a" value:"optional"`
		B string `name:"b" value:"requiredIf:a=^FOO$"`
	}

	v := New(&s{})

	if v.Populate(map[string]string{}, &s{}) != nil {
		t.Error(`value:"optional" + value:"requiredIf:a=^FOO$" violated, should permit missing parameter`)
	}

	if v.Populate(map[string]string{"a": "xxx"}, &s{}) != nil {
		t.Error(`value:"optional" + value:"requiredIf:a=^FOO$" violated, requiredIf should not match`)
	}

	if v.Populate(map[string]string{"a": "FOO"}, &s{}) == nil {
		t.Error(`value:"optional" + value:"requiredIf:a=^FOO$" violated, requiredIf should match`)
	}

	if v.Populate(map[string]string{"a": "FOO", "b": "BAR"}, &s{}) != nil {
		t.Error(`value:"optional" + value:"requiredIf:a=^FOO$" violated, should succeed`)
	}
}

func TestValueOptionalIf(t *testing.T) {
	type s struct {
		A string `name:"a" value:"optional"`
		B string `name:"b" value:"optionalIf:a=^FOO$"`
	}

	v := New(&s{})

	if v.Populate(map[string]string{}, &s{}) == nil {
		t.Error(`value:"optional" + value:"optionalIf:a=^FOO$" violated, should fail on missing parameter`)
	}

	if v.Populate(map[string]string{"a": "xxx"}, &s{}) == nil {
		t.Error(`value:"optional" + value:"optionalIf:a=^FOO$" violated, should fail on mis-matched parameter`)
	}

	if v.Populate(map[string]string{"a": "FOO"}, &s{}) != nil {
		t.Error(`value:"optional" + value:"optionalIf:a=^FOO$" violated, should succeed on matching parameter`)
	}
}

func TestValueDefault(t *testing.T) {
	type s struct {
		A string `name:"a" value:"default:FOO"`
	}

	o := &s{}
	v := New(o)

	if v.Populate(map[string]string{}, o) != nil {
		t.Error(`value:"default:FOO" should succeed`)
	}

	if o.A != "FOO" {
		t.Errorf(`value:"default:FOO" : expected value "FOO", got "%s"`, o.A)
	}

	if v.Populate(map[string]string{"a": "xxx"}, o) != nil {
		t.Error(`setting a value should succeed`)
	}

	if o.A != "xxx" {
		t.Errorf(`value:"default:FOO" with value "xxx": expected value "xxx", got "%s"`, o.A)
	}
}

func TestDependsOn(t *testing.T) {
	type s struct {
		A string `name:"a" value:"optional"`
		B string `name:"b" value:"optional"`
		C string `name:"c" value:"optional"`
		D string `name:"d" value:"optional" dependsOn:"a|b,c"`
	}

	v := New(&s{})

	if v.Populate(map[string]string{}, &s{}) != nil {
		t.Error(`value:"optional" dependsOn:"a|b,c" violated, should permit missing parameter`)
	}

	if v.Populate(map[string]string{"d": "ddd"}, &s{}) == nil {
		t.Error(`value:"optional" dependsOn:"a|b,c" violated, should fail on unsatisfied dependencies`)
	}

	if v.Populate(map[string]string{"d": "ddd", "c": "ccc"}, &s{}) == nil {
		t.Error(`value:"optional" dependsOn:"a|b,c" violated, should fail on unsatisfied dependencies`)
	}

	if v.Populate(map[string]string{"d": "ddd", "c": "ccc", "b": "bbb", "a": "aaa"}, &s{}) == nil {
		t.Error(`value:"optional" dependsOn:"a|b,c" violated, should fail on unsatisfied dependencies`)
	}

	if v.Populate(map[string]string{"d": "ddd", "c": "ccc", "a": "aaa"}, &s{}) != nil {
		t.Error(`value:"optional" dependsOn:"a|b,c" violated, should succeed on satisfied dependencies`)
	}

	if v.Populate(map[string]string{"d": "ddd", "c": "ccc", "b": "bbb"}, &s{}) != nil {
		t.Error(`value:"optional" dependsOn:"a|b,c" violated, should succeed on satisfied dependencies`)
	}
}

func TestPrecludes(t *testing.T) {
	type s struct {
		A string `name:"a" value:"optional"`
		B string `name:"b" value:"optional"`
		C string `name:"c" precludes:"a,b"`
	}

	v := New(&s{})

	if v.Populate(map[string]string{"c": "ccc", "a": "aaa"}, &s{}) == nil {
		t.Error(`precludes:"a,b" violated, should fail on unsatisfied dependencies`)
	}

	if v.Populate(map[string]string{"c": "ccc", "b": "bbb"}, &s{}) == nil {
		t.Error(`precludes:"a,b" violated, should fail on unsatisfied dependencies`)
	}

	if v.Populate(map[string]string{"c": "ccc", "a": "aaa", "b": "bbb"}, &s{}) == nil {
		t.Error(`precludes:"a,b" violated, should fail on unsatisfied dependencies`)
	}

	if v.Populate(map[string]string{"c": "ccc"}, &s{}) != nil {
		t.Error(`precludes:"a,b" violated, should succeed on satisfied dependencies`)
	}
}

func TestMatches(t *testing.T) {
	type s struct {
		A string `name:"a" matches:"^(?i)true|false$"`
	}

	v := New(&s{})

	if v.Populate(map[string]string{"a": "xxx"}, &s{}) == nil {
		t.Error(`matches:"true|false" violated, should fail on mis-matched parameter`)
	}

	if v.Populate(map[string]string{"a": "false"}, &s{}) != nil {
		t.Error(`matches:"true|false" violated, should succeed on matching parameter`)
	}
}
