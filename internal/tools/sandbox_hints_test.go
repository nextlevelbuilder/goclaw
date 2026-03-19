package tools

import (
	"strings"
	"testing"
)

func TestIsBinaryNotFound(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		output   string
		want     bool
	}{
		{"exit 127 empty output", 127, "", true},
		{"exit 127 with sh not found", 127, "sh: git: not found", true},
		{"exit 127 with bash error", 127, "bash: python3: command not found", true},
		{"exit 1 permission denied", 1, "permission denied", false},
		{"exit 0 success", 0, "ok", false},
		{"exit 1 sh not found pattern", 1, "sh: node: not found", true},
		{"exit 1 command not found pattern", 1, "bash: command not found: curl", true},
		{"exit 1 unrelated not found", 1, "file not found: config.yaml", false},
		{"exit 2 no such file", 2, "No such file or directory", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBinaryNotFound(tt.exitCode, tt.output)
			if got != tt.want {
				t.Errorf("isBinaryNotFound(%d, %q) = %v, want %v", tt.exitCode, tt.output, got, tt.want)
			}
		})
	}
}

func TestMaybeSandboxHint(t *testing.T) {
	tests := []struct {
		name      string
		exitCode  int
		output    string
		wantHint  bool
	}{
		{"binary not found returns hint", 127, "sh: git: not found", true},
		{"success returns empty", 0, "ok", false},
		{"non-binary error returns empty", 1, "segfault", false},
		{"sh pattern returns hint", 1, "sh: python3: not found", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaybeSandboxHint(tt.exitCode, tt.output)
			if tt.wantHint {
				if !strings.Contains(got, "[SANDBOX]") {
					t.Errorf("MaybeSandboxHint(%d, %q) = %q, want hint containing [SANDBOX]", tt.exitCode, tt.output, got)
				}
			} else {
				if got != "" {
					t.Errorf("MaybeSandboxHint(%d, %q) = %q, want empty", tt.exitCode, tt.output, got)
				}
			}
		})
	}
}
