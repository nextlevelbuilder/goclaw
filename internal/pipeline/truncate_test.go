package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncationConfigForWorkspaceStoresOverflowInsideWorkspace(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	cfg := TruncationConfigForWorkspace(workspaceDir)
	content := strings.Repeat("x", cfg.MaxResultChars+100)

	truncated, ref := TruncateResult(content, cfg)
	if ref == "" {
		t.Fatal("expected overflow reference to be created")
	}
	if !strings.HasPrefix(ref, filepath.Join(workspaceDir, "overflow")) {
		t.Fatalf("overflow ref %q not stored inside workspace", ref)
	}
	if !strings.Contains(truncated, ref) {
		t.Fatalf("truncated preview missing overflow ref %q", ref)
	}
	if _, err := os.Stat(ref); err != nil {
		t.Fatalf("expected overflow file to exist: %v", err)
	}
}
