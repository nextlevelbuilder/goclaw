package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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

	t.Run("restricted", func(t *testing.T) {
		tool := NewMessageTool(workspace, true)
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
			{"workspace absolute", "MEDIA:" + testFile, testFile, true},
			{"workspace relative", "MEDIA:docs/report.pdf", testFile, true},

			// Not a MEDIA: message
			{"no prefix", filepath.Join(tmpDir, "test.png"), "", false},
			{"empty after prefix", "MEDIA:", "", false},
			{"dot path", "MEDIA:.", "", false},
			{"empty string", "", "", false},
			{"just MEDIA", "MEDIA", "", false},

			// Outside workspace + outside /tmp/ → blocked
			{"outside workspace", "MEDIA:/etc/passwd", "", false},
			{"traversal attack", "MEDIA:" + filepath.Join(workspace, "..", "etc", "passwd"), "", false},
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

	t.Run("unrestricted", func(t *testing.T) {
		tool := NewMessageTool(workspace, false)
		ctx := context.Background()

		tests := []struct {
			name   string
			input  string
			wantOK bool
		}{
			{"any absolute path", "MEDIA:/etc/hostname", true},
			{"workspace relative", "MEDIA:docs/report.pdf", true},
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
		ctx := WithToolWorkspace(context.Background(), workspace)

		got, ok := tool.resolveMediaPath(ctx, "MEDIA:docs/report.pdf")
		if !ok {
			t.Fatal("expected ok=true for workspace-relative path with context workspace")
		}
		if got != testFile {
			t.Errorf("got %q, want %q", got, testFile)
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
		{"outside tmp", "/etc/passwd", false},
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
