package mcp

import (
	"testing"
)

func TestResolveEnvVars(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("USER", "testuser")

	tests := []struct {
		name    string
		input   map[string]string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "resolves allowed env prefix",
			input:   map[string]string{"X-User": "env:USER", "X-Custom": "literal"},
			want:    map[string]string{"X-User": "testuser", "X-Custom": "literal"},
			wantErr: false,
		},
		{
			name:    "resolves HOME env var",
			input:   map[string]string{"X-Home": "env:HOME"},
			want:    map[string]string{"X-Home": "/home/testuser"},
			wantErr: false,
		},
		{
			name:    "nil map",
			input:   nil,
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name:    "rejects non-allowlisted env var",
			input:   map[string]string{"Authorization": "env:AWS_SECRET_KEY"},
			wantErr: true,
		},
		{
			name:    "rejects sensitive env var",
			input:   map[string]string{"X-Token": "env:DATABASE_PASSWORD"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveEnvVars(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
