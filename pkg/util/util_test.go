package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringToMap(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  map[string]string
	}{
		{
			name: "test1",
			in:   "k1=v1,k2=v2",
			out:  map[string]string{"k1": "v1", "k2": "v2"},
		},
		{
			name: "test2",
			in:   "k1=v1,k2=v2=true",
			out:  map[string]string{"k1": "v1", "k2": "v2=true"},
		},
		{
			name: "test3",
			in:   "k1,k2",
			out:  map[string]string{"k1": "", "k2": ""},
		},
		{
			name: "test4",
			in:   " k1=v1, k2 ",
			out:  map[string]string{"k1": "v1", "k2": ""},
		},
		{
			name: "test5",
			in:   "k3=v3,=emptykey",
			out:  map[string]string{"k3": "v3", "": "emptykey"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := StringToMap(test.in)

			assert.Equal(t, test.out, out)
		})
	}
}

func TestUnique(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		out  []string
	}{
		{name: "nil", in: nil, out: []string{}},
		{name: "empty", in: []string{}, out: []string{}},
		{name: "no duplicates preserves order", in: []string{"b", "a", "c"}, out: []string{"b", "a", "c"}},
		{name: "duplicates removed keeping first-seen order", in: []string{"a", "b", "a", "c", "b"}, out: []string{"a", "b", "c"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.out, Unique(test.in))
		})
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name        string
		dest        []string
		src         []string
		out         []string
		wantChanged bool
	}{
		{
			name:        "nil dest returns src",
			dest:        nil,
			src:         []string{"a", "b"},
			out:         []string{"a", "b"},
			wantChanged: true,
		},
		{
			name:        "empty dest returns src",
			dest:        []string{},
			src:         []string{"a"},
			out:         []string{"a"},
			wantChanged: true,
		},
		{
			name:        "all src already present is a no-op",
			dest:        []string{"a", "b", "c"},
			src:         []string{"a", "b"},
			out:         []string{"a", "b", "c"},
			wantChanged: false,
		},
		{
			name:        "missing src appended preserving order",
			dest:        []string{"b", "d"},
			src:         []string{"a", "c"},
			out:         []string{"b", "d", "a", "c"},
			wantChanged: true,
		},
		{
			name:        "empty src with existing dest is a no-op",
			dest:        []string{"a"},
			src:         []string{},
			out:         []string{"a"},
			wantChanged: false,
		},
		{
			name:        "empty dest and empty src is a no-op",
			dest:        []string{},
			src:         []string{},
			out:         []string{},
			wantChanged: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out, changed := Merge(test.dest, test.src)
			assert.Equal(t, test.wantChanged, changed)
			assert.Equal(t, test.out, out)
		})
	}
}
