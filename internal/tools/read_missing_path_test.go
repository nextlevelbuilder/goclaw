package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// A missing absolute path outside the workspace must be reported as not found,
// not as an access-denied boundary violation.
func TestReadFileMissingOutsidePathReportsNotFound(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	missing := filepath.Join(outside, "nested", "does-not-exist.md")

	tool := NewReadFileTool(ws, true)
	res := tool.Execute(context.Background(), map[string]any{"path": missing})

	if res == nil || !res.IsError {
		t.Fatalf("expected error result, got %+v", res)
	}
	msg := strings.ToLower(res.ForLLM)
	if strings.Contains(msg, "outside workspace") {
		t.Errorf("missing file reported as access denied: %q", res.ForLLM)
	}
	if !strings.Contains(msg, "not found") && !strings.Contains(msg, "no such file") {
		t.Errorf("error does not indicate a missing file: %q", res.ForLLM)
	}
}
