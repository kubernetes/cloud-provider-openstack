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
