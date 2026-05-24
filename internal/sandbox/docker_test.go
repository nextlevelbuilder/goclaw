package sandbox

import (
	"strings"
	"testing"
)

func TestLimitedBuffer_UnderLimit(t *testing.T) {
	lb := &limitedBuffer{max: 100}
	n, err := lb.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
	if lb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", lb.String())
	}
	if lb.truncated {
		t.Error("should not be truncated")
	}
}

func TestLimitedBuffer_AtLimit(t *testing.T) {
	lb := &limitedBuffer{max: 5}
	lb.Write([]byte("hello"))
	if lb.truncated {
		t.Error("exactly at limit should not be truncated")
	}
	if lb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", lb.String())
	}
}

func TestLimitedBuffer_OverLimit(t *testing.T) {
	lb := &limitedBuffer{max: 5}
	n, err := lb.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should report all bytes as "written" (consumed) even though truncated
	if n != 11 {
		t.Errorf("expected 11 (full input consumed), got %d", n)
	}
	if lb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", lb.String())
	}
	if !lb.truncated {
		t.Error("should be truncated")
	}
}

func TestLimitedBuffer_MultipleWrites(t *testing.T) {
	lb := &limitedBuffer{max: 10}
	lb.Write([]byte("aaaa"))
	lb.Write([]byte("bbbb"))
	lb.Write([]byte("cccc")) // should be partially truncated

	if lb.buf.Len() != 10 {
		t.Errorf("expected 10 bytes, got %d", lb.buf.Len())
	}
	if !lb.truncated {
		t.Error("should be truncated after exceeding max")
	}
	if lb.String() != "aaaabbbbcc" {
		t.Errorf("expected 'aaaabbbbcc', got %q", lb.String())
	}
}

func TestLimitedBuffer_DiscardAfterTruncation(t *testing.T) {
	lb := &limitedBuffer{max: 3}
	lb.Write([]byte("abc"))
	lb.Write([]byte("def")) // should be silently discarded

	if lb.String() != "abc" {
		t.Errorf("expected 'abc', got %q", lb.String())
	}
	if !lb.truncated {
		t.Error("should be truncated")
	}
}

func TestDefaultConfig_MaxOutputBytes(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxOutputBytes != 1<<20 {
		t.Errorf("expected 1MB default, got %d", cfg.MaxOutputBytes)
	}
}

func TestSanitizeKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"agent:main:telegram:direct:123", "agent-main-telegram-direct-123"},
		{"simple", "simple"},
		{"has/slash", "has-slash"},
		{"has space", "has-space"},
		{strings.Repeat("x", 100), strings.Repeat("x", 50)},
	}
	for _, tc := range tests {
		got := sanitizeKey(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeKey(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestResolveScopeKey(t *testing.T) {
	tests := []struct {
		scope    Scope
		key      string
		expected string
	}{
		{ScopeShared, "agent:main:telegram:direct:123", "shared"},
		{ScopeAgent, "agent:main:telegram:direct:123", "agent:main"},
		{ScopeSession, "agent:main:telegram:direct:123", "agent:main:telegram:direct:123"},
		{ScopeSession, "", "default"},
	}
	for _, tc := range tests {
		cfg := Config{Scope: tc.scope}
		got := cfg.ResolveScopeKey(tc.key)
		if got != tc.expected {
			t.Errorf("scope=%s key=%q → %q, want %q", tc.scope, tc.key, got, tc.expected)
		}
	}
}

func TestFsBridgeResolvePathRejectsWorkspaceEscapes(t *testing.T) {
	bridge := NewFsBridge("container-test", "/workspace/agent-a")

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "inside relative", path: "notes/a.txt", want: "/workspace/agent-a/notes/a.txt"},
		{name: "inside absolute", path: "/workspace/agent-a/notes/a.txt", want: "/workspace/agent-a/notes/a.txt"},
		{name: "relative parent escape", path: "../agent-b/secret.txt", want: "/workspace/agent-a"},
		{name: "absolute sibling escape", path: "/workspace/agent-b/secret.txt", want: "/workspace/agent-a"},
		{name: "root escape", path: "/etc/passwd", want: "/workspace/agent-a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bridge.resolvePath(tt.path); got != tt.want {
				t.Fatalf("resolvePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFsBridgePathWithinUsesPathBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		root   string
		target string
		want   bool
	}{
		{name: "root itself", root: "/workspace/agent-a", target: "/workspace/agent-a", want: true},
		{name: "child path", root: "/workspace/agent-a", target: "/workspace/agent-a/file.txt", want: true},
		{name: "sibling with shared prefix", root: "/workspace/agent-a", target: "/workspace/agent-a-b/file.txt", want: false},
		{name: "parent path", root: "/workspace/agent-a", target: "/workspace", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fsBridgePathWithin(tt.root, tt.target); got != tt.want {
				t.Fatalf("fsBridgePathWithin(%q, %q) = %v, want %v", tt.root, tt.target, got, tt.want)
			}
		})
	}
}

func TestFsBridgeWriteFileCommandPreservesOverwriteTruncation(t *testing.T) {
	args := fsBridgeWriteDDArgs("/workspace/file.txt", false)
	for _, arg := range args {
		if arg == "conv=notrunc" || arg == "oflag=append" {
			t.Fatalf("overwrite command must truncate, got append-only arg %q in %v", arg, args)
		}
	}
}

func TestFsBridgeWriteFileCommandUsesNoTruncOnlyForAppend(t *testing.T) {
	args := fsBridgeWriteDDArgs("/workspace/file.txt", true)
	if !containsString(args, "conv=notrunc") {
		t.Fatalf("append command missing conv=notrunc: %v", args)
	}
	if !containsString(args, "oflag=append") {
		t.Fatalf("append command missing oflag=append: %v", args)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
