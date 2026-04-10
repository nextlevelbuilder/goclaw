package pg

import (
	"reflect"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store/base"
)

func TestPGDialect_Placeholder(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "$1"},
		{2, "$2"},
		{5, "$5"},
		{10, "$10"},
	}
	for _, tt := range tests {
		got := pgDialect.Placeholder(tt.n)
		if got != tt.want {
			t.Errorf("Placeholder(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestPGDialect_TransformValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"string", "hello"},
		{"int", 42},
		{"float", 3.14},
		{"nil", nil},
	}
	for _, tt := range tests {
		got := pgDialect.TransformValue(tt.input)
		// PG dialect returns identity for all values
		if got != tt.input {
			t.Errorf("TransformValue(%v) = %v, want identity", tt.input, got)
		}
	}

	// Test that maps are returned as-is (identity transform)
	testMap := map[string]string{"key": "value"}
	gotMap := pgDialect.TransformValue(testMap)
	if !reflect.DeepEqual(gotMap, testMap) {
		t.Error("TransformValue should return map identity")
	}

	// Test that slices are returned as-is (identity transform)
	testSlice := []string{"a", "b"}
	gotSlice := pgDialect.TransformValue(testSlice)
	if !reflect.DeepEqual(gotSlice, testSlice) {
		t.Error("TransformValue should return slice identity")
	}
}

func TestPGDialect_SupportsReturning(t *testing.T) {
	if !pgDialect.SupportsReturning() {
		t.Errorf("SupportsReturning() = false, want true")
	}
}

func TestPGDialect_ImplementsInterface(t *testing.T) {
	var _ base.Dialect = pgDialect
}
