package tools

import (
	"fmt"
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
		name     string
		exitCode int
		output   string
		wantTag  string // expected [SANDBOX] substring, or "" for no hint
	}{
		{"binary not found", 127, "sh: git: not found", "not installed in the sandbox image"},
		{"success no hint", 0, "ok", ""},
		{"non-binary error no hint", 1, "segfault", ""},
		{"sh pattern", 1, "sh: python3: not found", "not installed in the sandbox image"},
		{"permission denied", 1, "bash: /workspace/file: Permission denied", "read-only"},
		{"network unreachable", 1, "connect: Network is unreachable", "networking is disabled"},
		{"dns failure", 1, "Temporary failure in name resolution", "networking is disabled"},
		{"read-only fs", 1, "mkdir: cannot create directory: Read-only file system", "outside the mounted workspace"},
		{"no space left", 1, "write: No space left on device", "resource limit"},
		{"cannot allocate memory", 1, "Cannot allocate memory", "resource limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaybeSandboxHint(tt.exitCode, tt.output)
			if tt.wantTag == "" {
				if got != "" {
					t.Errorf("MaybeSandboxHint(%d, %q) = %q, want empty", tt.exitCode, tt.output, got)
				}
			} else {
				if !strings.Contains(got, "[SANDBOX]") {
					t.Errorf("MaybeSandboxHint(%d, %q) missing [SANDBOX] tag, got %q", tt.exitCode, tt.output, got)
				}
				if !strings.Contains(got, tt.wantTag) {
					t.Errorf("MaybeSandboxHint(%d, %q) missing %q, got %q", tt.exitCode, tt.output, tt.wantTag, got)
				}
			}
		})
	}
}

func TestMaybeFsBridgeHint(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantTag string
	}{
		{"nil error", nil, ""},
		{"permission denied", fmt.Errorf("docker exec: permission denied"), "read-only"},
		{"read-only filesystem", fmt.Errorf("write /workspace/file: read-only file system"), "outside the mounted workspace"},
		{"no such file", fmt.Errorf("stat /workspace/missing.txt: no such file or directory"), "not found inside sandbox"},
		{"no space", fmt.Errorf("write: no space left on device"), "resource limit"},
		{"generic error", fmt.Errorf("unexpected EOF"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaybeFsBridgeHint(tt.err)
			if tt.wantTag == "" {
				if got != "" {
					t.Errorf("MaybeFsBridgeHint(%v) = %q, want empty", tt.err, got)
				}
			} else {
				if !strings.Contains(got, "[SANDBOX]") {
					t.Errorf("MaybeFsBridgeHint(%v) missing [SANDBOX] tag, got %q", tt.err, got)
				}
				if !strings.Contains(got, tt.wantTag) {
					t.Errorf("MaybeFsBridgeHint(%v) missing %q, got %q", tt.err, tt.wantTag, got)
				}
			}
		})
	}
}
