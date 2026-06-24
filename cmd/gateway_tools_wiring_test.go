package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// applyUserAllowedPaths must grant the filesystem tools access to paths outside
// the agent workspace. cmd/gateway.go relies on this to re-apply
// system_configs['allowed_paths'] after the overlay (tool wiring runs first) —
// the AllowPaths analogue of the rate-limiter re-apply in #1111.
func TestApplyUserAllowedPaths_GrantsExternalPathToReadFile(t *testing.T) {
	ws := t.TempDir()
	ext := t.TempDir()
	extFile := filepath.Join(ext, "kb.md")
	if err := os.WriteFile(extFile, []byte("knowledge"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := tools.NewRegistry()
	reg.Register(tools.NewReadFileTool(ws, true))
	read, ok := reg.Get("read_file")
	if !ok {
		t.Fatal("read_file not registered")
	}

	// Before granting: a path outside the workspace is denied.
	if res := read.Execute(context.Background(), map[string]any{"path": extFile}); !res.IsError {
		t.Fatalf("expected access denied before allow, got: %s", res.ForLLM)
	}

	applyUserAllowedPaths(reg, []string{ext})

	// After granting: the external path is readable.
	if res := read.Execute(context.Background(), map[string]any{"path": extFile}); res.IsError {
		t.Fatalf("expected success after allow, got error: %s", res.ForLLM)
	}

	// The grant is scoped — an unrelated external path stays denied.
	other := t.TempDir()
	otherFile := filepath.Join(other, "secret.md")
	if err := os.WriteFile(otherFile, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := read.Execute(context.Background(), map[string]any{"path": otherFile}); !res.IsError {
		t.Fatalf("expected unrelated external path to stay denied, got: %s", res.ForLLM)
	}
}

// Applying twice (initial wiring + the gateway re-apply after ApplySystemConfigs)
// must keep the grant working and must not error.
func TestApplyUserAllowedPaths_RepeatedApplyIsSafe(t *testing.T) {
	ws := t.TempDir()
	ext := t.TempDir()
	extFile := filepath.Join(ext, "kb.md")
	if err := os.WriteFile(extFile, []byte("knowledge"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := tools.NewRegistry()
	reg.Register(tools.NewReadFileTool(ws, true))
	read, ok := reg.Get("read_file")
	if !ok {
		t.Fatal("read_file not registered")
	}

	applyUserAllowedPaths(reg, []string{ext}) // initial wiring (from config.json)
	applyUserAllowedPaths(reg, []string{ext}) // re-apply after system_configs overlay

	if res := read.Execute(context.Background(), map[string]any{"path": extFile}); res.IsError {
		t.Fatalf("expected success after repeated allow, got error: %s", res.ForLLM)
	}
}

// An empty allow list is a no-op and must not panic (the common case when no
// allowed_paths are configured).
func TestApplyUserAllowedPaths_EmptyIsNoop(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.NewReadFileTool(t.TempDir(), true))
	applyUserAllowedPaths(reg, nil)
	applyUserAllowedPaths(reg, []string{})
}
