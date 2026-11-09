package protocol

import "testing"

func TestNormalizeMemberID(t *testing.T) {
	tests := map[string]string{
		"123456789_0": "123456789",
		"123456789":   "123456789",
		"abc_def":     "abc_def",
		"_0":          "_0",
		"12_":         "12_",
	}
	for in, want := range tests {
		if got := normalizeMemberID(in); got != want {
			t.Errorf("normalizeMemberID(%q) = %q, want %q", in, got, want)
		}
	}
}
