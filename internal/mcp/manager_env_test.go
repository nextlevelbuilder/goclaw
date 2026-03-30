package mcp

import (
	"testing"
)

func TestResolveEnvVars(t *testing.T) {
	t.Setenv("TEST_MCP_TOKEN", "secret123")

	tests := []struct {
		name  string
		input map[string]string
		want  map[string]string
	}{
		{
			"resolves env prefix",
			map[string]string{"Authorization": "env:TEST_MCP_TOKEN", "X-Custom": "literal"},
			map[string]string{"Authorization": "secret123", "X-Custom": "literal"},
		},
		{
			"missing env var resolves to empty",
			map[string]string{"X-Missing": "env:NONEXISTENT_VAR_XYZ"},
			map[string]string{"X-Missing": ""},
		},
		{
			"nil map",
			nil,
			map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveEnvVars(tt.input)
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
