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

func TestSplitTrim(t *testing.T) {
	type args struct {
		s   string
		sep rune
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "csv style",
			args: args{
				s:   "10.0.0.0/8, my-string-data",
				sep: ',',
			},
			want: []string{
				"10.0.0.0/8",
				"my-string-data",
			},
		},
		{
			name: "csv with (with a trim space separation)",
			args: args{
				s:   "10.0.0.0/8 my-string-data",
				sep: ',',
			},
			want: []string{
				"10.0.0.0/8",
				"my-string-data",
			},
		},
		{
			name: "double comma",
			args: args{
				s:   ",10.0.0.0/8, , 192.168.0.0/24,,",
				sep: ',',
			},
			want: []string{
				"10.0.0.0/8",
				"192.168.0.0/24",
			},
		},
		{
			name: "empty string with comma",
			args: args{
				s:   " , ",
				sep: ',',
			},
			want: []string{},
		},
		{
			name: "empty string",
			args: args{
				s:   "",
				sep: ',',
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, SplitTrim(tt.args.s, tt.args.sep), "SplitTrim(%v, %v)", tt.args.s, tt.args.sep)
		})
	}
}
