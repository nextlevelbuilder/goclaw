package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
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
	partnerRaw := t.TempDir()
	partner, err := filepath.EvalSymlinks(partnerRaw)
	if err != nil {
		t.Fatal(err)
	}
	partnerPath := writeTempFile(t, partner, "partner-result.png")

	// Outside-workspace file: should be rejected by containment check.
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "leak.pdf")
	if err := os.WriteFile(outsidePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	hardlinkPath := filepath.Join(partner, "hardlink-result.png")
	if err := os.Link(outsidePath, hardlinkPath); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}
	symlinkRoot := filepath.Join(t.TempDir(), "delegate")
	if err := os.Symlink(outsideDir, symlinkRoot); err != nil {
		t.Skipf("symlinks not supported: %v", err)
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
		roots     []string
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
			roots:     []string{ws},
			wantPaths: []string{reportPath},
		},
		{
			name:      "multiple tokens deduped",
			content:   "First: MEDIA:a.mp3\nSecond: MEDIA:b.mp3\nAgain: MEDIA:a.mp3",
			roots:     []string{ws},
			wantPaths: []string{audioA, audioB},
		},
		{
			name:      "markdown wrapped and punctuation stripped",
			content:   `![chart](MEDIA:charts/q4.png). See "MEDIA:deliver/report.pdf".`,
			roots:     []string{ws},
			wantPaths: []string{chartPath, reportPath},
		},
		{
			name:    "hallucinated path dropped (file missing)",
			content: "MEDIA:not-real.pdf",
			roots:   []string{ws},
		},
		{
			name:    "path traversal escape blocked",
			content: "MEDIA:../leak.pdf",
			roots:   []string{ws},
		},
		{
			name:    "absolute path outside workspace blocked",
			content: "MEDIA:" + outsidePath,
			roots:   []string{ws},
		},
		{
			name:      "absolute collaboration path allowed",
			content:   "MEDIA:" + partnerPath,
			roots:     []string{ws, partner},
			wantPaths: []string{partnerPath},
		},
		{
			name:    "hardlink in collaboration path blocked",
			content: "MEDIA:" + hardlinkPath,
			roots:   []string{ws, partner},
		},
		{
			name:    "symlinked collaboration root blocked",
			content: "MEDIA:" + outsidePath,
			roots:   []string{ws, symlinkRoot},
		},
		{
			name:    "relative path does not search collaboration roots",
			content: "MEDIA:partner-result.png",
			roots:   []string{ws, partner},
		},
		{
			name:    "absolute path with no workspace dropped",
			content: "MEDIA:" + reportPath,
		},
		{
			name:    "symlink leaf rejected by Lstat",
			content: "MEDIA:shortcut-to-leak.pdf",
			roots:   []string{ws},
		},
		{
			name:    "ancestor symlink escape blocked (P0)",
			content: "MEDIA:shared/victim.pdf",
			roots:   []string{ws},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMediaFromContent(tt.content, tt.roots)
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

func TestMediaEgressRoots(t *testing.T) {
	ctx := tools.WithToolWorkspace(context.Background(), "/workspace/parent")
	ctx = tools.WithToolTeamWorkspace(ctx, "/workspace/team/chat")
	ctx = tools.WithToolTeamRoot(ctx, "/workspace/team")
	ctx = tools.WithTenantAllowedPaths(ctx, []string{"/workspace/tenant-export"})
	loop := NewLoop(LoopConfig{})

	got := loop.mediaEgressRoots(ctx)
	want := []string{
		"/workspace/parent",
		"/workspace/team/chat",
		"/workspace/team",
		"/workspace/tenant-export",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("mediaEgressRoots() = %v, want %v", got, want)
	}
}

// TestConfineToWorkspace exercises the shared media path-containment boundary
// directly. It is the single guard that both feeders of MediaResult.Path rely
// on, so a regression here would reopen the outbound-exfiltration hole (H2).
func TestConfineToWorkspace(t *testing.T) {
	wsRaw := t.TempDir()
	ws, err := filepath.EvalSymlinks(wsRaw)
	if err != nil {
		t.Fatal(err)
	}
	insidePath := writeTempFile(t, ws, "deliver/report.pdf")

	// File outside the workspace (stands in for /etc/passwd).
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsidePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Leaf symlink inside ws pointing outside: must be rejected by Lstat.
	leafSymlink := filepath.Join(ws, "shortcut.txt")
	symlinkSupported := os.Symlink(outsidePath, leafSymlink) == nil

	// Ancestor dir symlink inside ws pointing outside.
	symDirParent := t.TempDir()
	if err := os.WriteFile(filepath.Join(symDirParent, "victim.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ancestorSym := filepath.Join(ws, "shared")
	if symlinkSupported {
		if err := os.Symlink(symDirParent, ancestorSym); err != nil {
			symlinkSupported = false
		}
	}

	tests := []struct {
		name      string
		path      string
		workspace string
		wantOK    bool
		wantPath  string
		symlink   bool // requires symlink support
	}{
		{name: "relative inside workspace", path: "deliver/report.pdf", workspace: ws, wantOK: true, wantPath: insidePath},
		{name: "absolute inside workspace", path: insidePath, workspace: ws, wantOK: true, wantPath: insidePath},
		{name: "absolute outside workspace rejected", path: outsidePath, workspace: ws, wantOK: false},
		{name: "traversal escape rejected", path: "../secret.txt", workspace: ws, wantOK: false},
		{name: "missing file rejected", path: "nope.pdf", workspace: ws, wantOK: false},
		{name: "directory rejected", path: "deliver", workspace: ws, wantOK: false},
		{name: "empty workspace rejected", path: insidePath, workspace: "", wantOK: false},
		{name: "empty path rejected", path: "", workspace: ws, wantOK: false},
		{name: "leaf symlink rejected", path: "shortcut.txt", workspace: ws, wantOK: false, symlink: true},
		{name: "ancestor symlink escape rejected", path: "shared/victim.txt", workspace: ws, wantOK: false, symlink: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.symlink && !symlinkSupported {
				t.Skip("symlinks not supported on this platform")
			}
			got, ok := confineToWorkspace(tt.path, tt.workspace)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (got path %q)", ok, tt.wantOK, got)
			}
			if tt.wantOK && got != tt.wantPath {
				t.Errorf("path = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

// TestParseMediaResultConfinedToWorkspace reproduces the processToolResult sink
// (parseMediaResult → confineToWorkspace) and asserts that a tool emitting a
// MEDIA: path outside the agent workspace is dropped, not shipped to a channel.
// This is the regression guard for H2: MEDIA:/etc/passwd must never become an
// outbound MediaResult.
func TestParseMediaResultConfinedToWorkspace(t *testing.T) {
	wsRaw := t.TempDir()
	ws, err := filepath.EvalSymlinks(wsRaw)
	if err != nil {
		t.Fatal(err)
	}
	insidePath := writeTempFile(t, ws, "chart.png")

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "passwd")
	if err := os.WriteFile(outsidePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// confineSink mirrors the loop_tools.go branch: parse, then confine.
	confineSink := func(toolOutput string) (MediaResult, bool) {
		mr := parseMediaResult(toolOutput)
		if mr == nil {
			return MediaResult{}, false
		}
		cleaned, ok := confineToWorkspace(mr.Path, ws)
		if !ok {
			return MediaResult{}, false
		}
		mr.Path = cleaned
		return *mr, true
	}

	t.Run("inside workspace shipped", func(t *testing.T) {
		got, ok := confineSink("MEDIA:" + insidePath)
		if !ok {
			t.Fatal("expected in-workspace media to be shipped")
		}
		if got.Path != insidePath {
			t.Errorf("path = %q, want %q", got.Path, insidePath)
		}
	})

	t.Run("outside workspace dropped", func(t *testing.T) {
		if _, ok := confineSink("MEDIA:" + outsidePath); ok {
			t.Fatal("expected out-of-workspace media to be dropped")
		}
	})

	t.Run("traversal dropped", func(t *testing.T) {
		if _, ok := confineSink("MEDIA:../passwd"); ok {
			t.Fatal("expected traversal media to be dropped")
		}
	})
}
