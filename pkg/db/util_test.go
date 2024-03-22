package db

import (
	"testing"
)

func TestExpandSlice(t *testing.T) {
	type testCase struct {
		name  string
		arr   []struct{}
		index int
		want  int
	}
	tests := []testCase{
		{
			name:  "nil slice",
			arr:   nil,
			index: 0,
			want:  1,
		},
		{
			name:  "Empty slice",
			arr:   []struct{}{},
			index: 0,
			want:  1,
		},
		{
			name:  "Add several slots",
			arr:   []struct{}{{}, {}},
			index: 4,
			want:  5,
		},
		{
			name:  "Slice already has exact length",
			arr:   []struct{}{{}},
			index: 0,
			want:  1,
		},
		{
			name:  "Slice already too big",
			arr:   []struct{}{{}, {}, {}},
			index: 0,
			want:  3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandSlice(tt.arr, tt.index); got == nil {
				t.Errorf("expandSlice() returned a nil value")
			} else if len(got) != tt.want {
				t.Errorf("expandSlice() length = %v, want %v", len(got), tt.want)
			}
		})
	}
}
