package pg

import (
	"encoding/json"
	"testing"
)

func TestIsEmptyOrNullJSONUpdate(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{name: "nil interface", in: nil, want: true},
		{name: "nil raw message", in: json.RawMessage(nil), want: true},
		{name: "json null raw message", in: json.RawMessage(`null`), want: true},
		{name: "json null bytes", in: []byte(` null `), want: true},
		{name: "empty string", in: "", want: true},
		{name: "json null string", in: " null ", want: true},
		{name: "object raw message", in: json.RawMessage(`{"enabled":true}`), want: false},
		{name: "empty object", in: []byte(`{}`), want: false},
		{name: "map", in: map[string]any{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyOrNullJSONUpdate(tt.in); got != tt.want {
				t.Fatalf("isEmptyOrNullJSONUpdate(%T) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
