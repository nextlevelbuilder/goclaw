package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempFile drops a zero-byte file at workspace/relPath, creating dirs.
func writeTempFile(t *testing.T, workspace, relPath string) string {
	t.Helper()
	full := filepath.Join(workspace, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return full
}

func TestExtractMediaFromContent(t *testing.T) {
	wsRaw := t.TempDir()
	// Resolve workspace symlinks up front (macOS has /var → /private/var) so
	// expected paths match what the extractor returns after EvalSymlinks.
	ws, err := filepath.EvalSymlinks(wsRaw)
	if err != nil {
		t.Fatal(err)
	}
	reportPath := writeTempFile(t, ws, "deliver/report.pdf")
	audioA := writeTempFile(t, ws, "a.mp3")
	audioB := writeTempFile(t, ws, "b.mp3")
	chartPath := writeTempFile(t, ws, "charts/q4.png")

	// Outside-workspace file: should be rejected by containment check.
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "leak.pdf")
	if err := os.WriteFile(outsidePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink inside workspace pointing to outside: must be rejected by
	// EvalSymlinks-then-Rel containment. Covers the P0 ancestor-symlink
	// escape the lexical-only Rel check would have allowed.
	symlinkFile := filepath.Join(ws, "shortcut-to-leak.pdf")
	if err := os.Symlink(outsidePath, symlinkFile); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	// Ancestor symlink case: dir symlink inside ws pointing outside.
	symDirParent := t.TempDir()
	if err := os.WriteFile(filepath.Join(symDirParent, "victim.pdf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ancestorSym := filepath.Join(ws, "shared")
	if err := os.Symlink(symDirParent, ancestorSym); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	tests := []struct {
		name      string
		content   string
		workspace string
		wantPaths []string
	}{
		{
			name:    "empty content",
			content: "",
		},
		{
			name:    "no media prefix",
			content: "Just a regular response with no attachments.",
		},
		{
			name:      "relative path resolved + exists",
			content:   "MEDIA:deliver/report.pdf",
			workspace: ws,
			wantPaths: []string{reportPath},
		},
		{
			name:      "multiple tokens deduped",
			content:   "First: MEDIA:a.mp3\nSecond: MEDIA:b.mp3\nAgain: MEDIA:a.mp3",
			workspace: ws,
			wantPaths: []string{audioA, audioB},
		},
		{
			name:      "markdown wrapped and punctuation stripped",
			content:   `![chart](MEDIA:charts/q4.png). See "MEDIA:deliver/report.pdf".`,
			workspace: ws,
			wantPaths: []string{chartPath, reportPath},
		},
		{
			name:      "hallucinated path dropped (file missing)",
			content:   "MEDIA:not-real.pdf",
			workspace: ws,
		},
		{
			name:      "path traversal escape blocked",
			content:   "MEDIA:../leak.pdf",
			workspace: ws,
		},
		{
			name:      "absolute path outside workspace blocked",
			content:   "MEDIA:" + outsidePath,
			workspace: ws,
		},
		{
			name:    "absolute path with no workspace dropped",
			content: "MEDIA:" + reportPath,
		},
		{
			name:      "symlink leaf rejected by Lstat",
			content:   "MEDIA:shortcut-to-leak.pdf",
			workspace: ws,
		},
		{
			name:      "ancestor symlink escape blocked (P0)",
			content:   "MEDIA:shared/victim.pdf",
			workspace: ws,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMediaFromContent(tt.content, tt.workspace)
			if len(got) != len(tt.wantPaths) {
				t.Fatalf("count = %d, want %d; got=%+v", len(got), len(tt.wantPaths), got)
			}
			for i, want := range tt.wantPaths {
				if got[i].Path != want {
					t.Errorf("path[%d] = %q, want %q", i, got[i].Path, want)
				}
			}
		})
	}
}
