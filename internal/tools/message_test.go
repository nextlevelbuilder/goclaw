package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// outsidePath returns an absolute path that is guaranteed to be outside the
// given workspace and temp directories on any OS.  On Windows bare "/etc/..."
// is relative (no drive letter), so we prepend the volume name of the workspace
// to ensure filepath.IsAbs returns true.
func outsidePath(workspace, segments string) string {
	vol := filepath.VolumeName(workspace)
	return filepath.Join(vol+string(filepath.Separator), segments)
}

func TestResolveMediaPath(t *testing.T) {
	tmpDir := os.TempDir()

	// Create a temp workspace with a test file for workspace-relative tests.
	workspace := t.TempDir()
	docsDir := filepath.Join(workspace, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(docsDir, "report.pdf")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Normalize paths to canonical form (resolves macOS /var/folders → /private/var/folders symlink).
	// The resolvePath function uses filepath.EvalSymlinks, so test expectations must too.
	testFileCanonical, _ := filepath.EvalSymlinks(testFile)
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	t.Run("restricted", func(t *testing.T) {
		tool := NewMessageTool(workspaceCanonical, true)
		ctx := context.Background()

		tests := []struct {
			name   string
			input  string
			want   string
			wantOK bool
		}{
			// /tmp/ always allowed
			{"valid temp file", "MEDIA:" + filepath.Join(tmpDir, "test.png"), filepath.Join(tmpDir, "test.png"), true},
			{"valid nested temp", "MEDIA:" + filepath.Join(tmpDir, "sub", "file.txt"), filepath.Join(tmpDir, "sub", "file.txt"), true},

			// Workspace files allowed
			{"workspace absolute", "MEDIA:" + testFileCanonical, testFileCanonical, true},
			{"workspace relative", "MEDIA:docs/report.pdf", testFileCanonical, true},

			// Not a MEDIA: message
			{"no prefix", filepath.Join(tmpDir, "test.png"), "", false},
			{"empty after prefix", "MEDIA:", "", false},
			{"dot path", "MEDIA:.", "", false},
			{"empty string", "", "", false},
			{"just MEDIA", "MEDIA", "", false},

			// Outside workspace + outside /tmp/ → blocked
			{"outside workspace", "MEDIA:" + outsidePath(workspaceCanonical, "etc/passwd"), "", false},
			{"traversal attack", "MEDIA:" + filepath.Join(workspaceCanonical, "..", "etc", "passwd"), "", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, ok := tool.resolveMediaPath(ctx, tt.input)
				if ok != tt.wantOK {
					t.Errorf("resolveMediaPath(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				}
				if ok && got != tt.want {
					t.Errorf("resolveMediaPath(%q) = %q, want %q", tt.input, got, tt.want)
				}
			})
		}
	})

	// effectiveRestrict() always returns true (multi-tenant security hardening),
	// so even tools created with restrict=false behave as restricted.
	t.Run("unrestricted_tool_still_restricted", func(t *testing.T) {
		tool := NewMessageTool(workspaceCanonical, false)
		ctx := context.Background()

		tests := []struct {
			name   string
			input  string
			wantOK bool
		}{
			// Outside workspace → blocked (effectiveRestrict overrides to true)
			{"absolute outside workspace", "MEDIA:" + outsidePath(workspaceCanonical, "etc/hostname"), false},
			// Workspace-relative → allowed
			{"workspace relative", "MEDIA:docs/report.pdf", true},
			// /tmp/ → allowed (temp dir exception in restricted mode)
			{"temp file", "MEDIA:" + filepath.Join(tmpDir, "test.png"), true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, ok := tool.resolveMediaPath(ctx, tt.input)
				if ok != tt.wantOK {
					t.Errorf("resolveMediaPath(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				}
			})
		}
	})

	t.Run("context workspace override", func(t *testing.T) {
		// Tool has no workspace, but context provides one.
		tool := NewMessageTool("", true)
		ctx := WithToolWorkspace(context.Background(), workspaceCanonical)

		got, ok := tool.resolveMediaPath(ctx, "MEDIA:docs/report.pdf")
		if !ok {
			t.Fatal("expected ok=true for workspace-relative path with context workspace")
		}
		if got != testFileCanonical {
			t.Errorf("got %q, want %q", got, testFileCanonical)
		}
	})
}

func TestIsInTempDir(t *testing.T) {
	tmpDir := os.TempDir()
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"in tmp", filepath.Join(tmpDir, "test.png"), true},
		{"nested in tmp", filepath.Join(tmpDir, "sub", "file.txt"), true},
		{"tmp itself", tmpDir, false}, // only files inside, not the dir itself
		{"outside tmp", outsidePath(tmpDir, "etc/passwd"), false},
		{"relative path", "relative/path.txt", false},
		{"traversal", filepath.Join(tmpDir, "..", "etc", "passwd"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInTempDir(tt.path); got != tt.want {
				t.Errorf("isInTempDir(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
