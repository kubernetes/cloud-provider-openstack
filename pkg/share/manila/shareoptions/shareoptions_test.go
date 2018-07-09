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
	"testing"

	"github.com/gophercloud/gophercloud"
)

func constraintsToString(c optionConstraints) string {
	if c.protocol == "" {
		c.protocol = "*"
	}

	if c.backend == "" {
		c.backend = "*"
	}

	return fmt.Sprintf("{protocol:\"%s\" backend:\"%s\"}", c.protocol, c.backend)
}

func testConstraints(t *testing.T, c optionConstraints, shouldPass []reflect.StructTag, shouldFail []reflect.StructTag) {
	for _, tag := range shouldPass {
		if !c.constraintsMet(tag) {
			t.Errorf("constraint %s should pass for {%s}", constraintsToString(c), tag)
		}
	}

	for _, tag := range shouldFail {
		if c.constraintsMet(tag) {
			t.Errorf("constraint %s should fail for {%s}", constraintsToString(c), tag)
		}
	}
}

func TestOptionConstraints(t *testing.T) {
	var s struct {
		A string
		B string `protocol:"p"`
		C string `backend:"b"`
		D string `protocol:"p" backend:"b"`
		E string `protocol:"x" backend:"b"`
		F string `protocol:"p" backend:"x"`
		G string `protocol:"x" backend:"x"`
	}

	var (
		st   = reflect.TypeOf(s)
		aTag = st.Field(0).Tag
		bTag = st.Field(1).Tag
		cTag = st.Field(2).Tag
		dTag = st.Field(3).Tag
		eTag = st.Field(4).Tag
		fTag = st.Field(5).Tag
		gTag = st.Field(6).Tag
	)

	ts := [...]struct {
		c          optionConstraints
		shouldPass []reflect.StructTag
		shouldFail []reflect.StructTag
	}{
		{
			c:          optionConstraints{},
			shouldPass: []reflect.StructTag{aTag},
			shouldFail: []reflect.StructTag{bTag, cTag, dTag, eTag, fTag, gTag},
		},
		{
			c:          optionConstraints{protocol: "p"},
			shouldPass: []reflect.StructTag{aTag, bTag},
			shouldFail: []reflect.StructTag{dTag, eTag, fTag, gTag},
		},
		{
			c:          optionConstraints{backend: "b"},
			shouldPass: []reflect.StructTag{aTag, cTag},
			shouldFail: []reflect.StructTag{dTag, eTag, fTag, gTag},
		},
		{
			c:          optionConstraints{protocol: "p", backend: "b"},
			shouldPass: []reflect.StructTag{aTag, bTag, cTag, dTag},
			shouldFail: []reflect.StructTag{eTag, fTag, gTag},
		},
	}

	for i := range ts {
		testConstraints(t, ts[i].c, ts[i].shouldPass, ts[i].shouldFail)
	}
}

func checkExtractFieldsCount(want int, got int) error {
	if want != got {
		return fmt.Errorf("reported wrong number of fields: want %d, got %d", want, got)
	}

	return nil
}

func TestExtractOptions(t *testing.T) {
	var s1 struct {
		A string `name:"a"`
	}

	var s2 struct {
		B string `name:"b"`
	}

	var s3 struct {
		A string `name:"a" protocol:"x"`
		C string `name:"c" protocol:"p"`
	}

	params := map[string]string{"a": "a-value", "c": "c-value"}

	var (
		n   int
		err error
	)

	// Check for field "a"

	n, err = extractParams(&optionConstraints{}, params, &s1)

	if err != nil {
		t.Errorf("failed to extract parameters: %v", err)
	}

	if err = checkExtractFieldsCount(1, n); err != nil {
		t.Error(err)
	}

	// Check for non-existent field "b"

	n, err = extractParams(&optionConstraints{}, params, &s2)

	if err == nil {
		t.Errorf("expected error from extracting parameters; contents: %+v", s2)
	}

	if err = checkExtractFieldsCount(0, n); err != nil {
		t.Error(err)
	}

	// Check for field "c" constrained with protocol="p"

	n, err = extractParams(&optionConstraints{protocol: "p"}, params, &s3)

	if err != nil {
		t.Errorf("failed to extract parameters with constraints: %v", err)
	}

	if err = checkExtractFieldsCount(2, n); err != nil {
		t.Error(err)
	}
}

func testExctractParamsAndCheckEquals(t *testing.T, obj, expected interface{}, params map[string]string) {
	// reset internal maps
	nameIdxMap = make(map[structName]nameIndexMap)
	reqsMap = make(map[structName]requirementsMap)
	coalMap = make(map[structName]coalesceMap)

	// build maps for this type
	processStruct(obj)

	if _, err := extractParams(&optionConstraints{}, params, obj); err != nil {
		t.Errorf("failed to extract parameters: %v", err)
	}

	if !reflect.DeepEqual(obj, expected) {
		t.Errorf("unexpected result: got %+v, expected %+v", obj, expected)
	}
}

func TestOptionsOptionalValue(t *testing.T) {
	type s struct {
		A string `name:"a" value:"optional"`
	}

	testExctractParamsAndCheckEquals(t, &s{}, &s{}, map[string]string{})
}

func TestOptionsDefaultValue(t *testing.T) {
	type s struct {
		A string `name:"a" value:"default=xxx"`
	}

	testExctractParamsAndCheckEquals(t, &s{}, &s{A: "xxx"}, map[string]string{})
}

func TestOptionsCoalesceValue(t *testing.T) {
	type s struct {
		A string `name:"a" value:"default=xxx"`
		B string `name:"b" value:"coalesce=a"`
	}

	testExctractParamsAndCheckEquals(t, &s{}, &s{A: "xxx", B: "xxx"}, map[string]string{})
}

func TestOptionRequiresValue(t *testing.T) {
	type s struct {
		A string `name:"a" value:"default=aaa"`
		B string `name:"b" value:"optional"`
		C string `name:"c" value:"default=ccc"`
		D string `name:"d" value:"optional"`
		E string `name:"e" value:"default=eee"`
		X string `name:"x" value:"requires=a|b,c,d|e"`
	}

	testExctractParamsAndCheckEquals(t, &s{}, &s{A: "aaa", C: "ccc", E: "eee", X: "xxx"}, map[string]string{"x": "xxx"})
}

func TestOpenStackOptionsToAuthOptions(t *testing.T) {
	osOptions := OpenStackOptions{
		OSAuthURL:     "OSAuthURL",
		OSUserID:      "OSUserID",
		OSUsername:    "OSUsername",
		OSPassword:    "OSPassword",
		OSProjectID:   "OSProjectID",
		OSProjectName: "OSProjectName",
		OSDomainID:    "OSDomainID",
		OSDomainName:  "OSDomainName",
	}

	authOptions := osOptions.ToAuthOptions()

	eq := reflect.DeepEqual(authOptions, &gophercloud.AuthOptions{
		IdentityEndpoint: "OSAuthURL",
		UserID:           "OSUserID",
		Username:         "OSUsername",
		Password:         "OSPassword",
		TenantID:         "OSProjectID",
		TenantName:       "OSProjectName",
		DomainID:         "OSDomainID",
		DomainName:       "OSDomainName",
	})

	if !eq {
		t.Error("bad conversion from OpenStackOptions to gophercloud.AuthOptions")
	}
}
