package upgrade

import (
	"os"
	"testing"
)

func TestIsStaleWorkspace(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"docker era app workspace", "/app/workspace/clax", true},
		{"docker era root", "/app/workspace", true},
		{"docker era trailing slash", "/app/workspace/", true},
		{"tilde literal", "~/.goclaw/x-workspace", true},
		{"tilde only", "~", true},
		{"absolute host path", "/var/lib/goclaw/workspace/clax", false},
		{"current dot", ".", false},
		{"empty", "", false},
		{"whitespace", "   ", false},
		{"custom absolute (preserve)", "/srv/agents/clax", false},
		{"relative non-tilde", "data/workspace", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStaleWorkspace(tc.in); got != tc.want {
				t.Fatalf("isStaleWorkspace(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveWorkspaceBase_EnvWins(t *testing.T) {
	t.Setenv("GOCLAW_WORKSPACE", "/var/lib/goclaw/workspace")
	if got := resolveWorkspaceBase(); got != "/var/lib/goclaw/workspace" {
		t.Fatalf("resolveWorkspaceBase() = %q, want /var/lib/goclaw/workspace", got)
	}
}

func TestResolveWorkspaceBase_EnvTilde(t *testing.T) {
	t.Setenv("GOCLAW_WORKSPACE", "~/custom/ws")
	home, _ := os.UserHomeDir()
	want := home + "/custom/ws"
	if got := resolveWorkspaceBase(); got != want {
		t.Fatalf("resolveWorkspaceBase() = %q, want %q", got, want)
	}
}

func TestResolveWorkspaceBase_StripsTrailingSlash(t *testing.T) {
	t.Setenv("GOCLAW_WORKSPACE", "/var/lib/goclaw/workspace/")
	if got := resolveWorkspaceBase(); got != "/var/lib/goclaw/workspace" {
		t.Fatalf("resolveWorkspaceBase() = %q, want trimmed", got)
	}
}

func TestResolveWorkspaceBase_FallbackOnMissingConfig(t *testing.T) {
	t.Setenv("GOCLAW_WORKSPACE", "")
	t.Setenv("GOCLAW_CONFIG", "/nonexistent/path/config.json")
	// Should not return empty even if config file missing — falls back through
	// config.Load's IsNotExist branch (which returns defaults) or to default.
	if got := resolveWorkspaceBase(); got == "" {
		t.Fatal("resolveWorkspaceBase() returned empty for missing config; expected fallback")
	}
}
