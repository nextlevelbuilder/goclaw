package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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

func TestExtractEmbeddedMedia(t *testing.T) {
	tmpDir := os.TempDir()

	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	// Create test files in workspace.
	docsDir := filepath.Join(workspaceCanonical, "docs")
	os.MkdirAll(docsDir, 0o755)
	reportFile := filepath.Join(docsDir, "report.docx")
	os.WriteFile(reportFile, []byte("test"), 0o644)
	reportCanonical, _ := filepath.EvalSymlinks(reportFile)

	tool := NewMessageTool(workspaceCanonical, true)
	ctx := context.Background()

	t.Run("no MEDIA: in message", func(t *testing.T) {
		msg := "Hello, here is your report!"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)
		if cleaned != msg {
			t.Errorf("expected unchanged message, got %q", cleaned)
		}
		if len(media) != 0 {
			t.Errorf("expected no media, got %d", len(media))
		}
	})

	t.Run("embedded MEDIA: in multi-line message", func(t *testing.T) {
		msg := "Here is the file:\nMEDIA:" + reportCanonical + "\nPlease download!"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Here is the file:\nPlease download!" {
			t.Errorf("unexpected cleaned text: %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
		if media[0].URL != reportCanonical {
			t.Errorf("media URL = %q, want %q", media[0].URL, reportCanonical)
		}
		wantMime := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		if media[0].ContentType != wantMime {
			t.Errorf("content type = %q, want %q", media[0].ContentType, wantMime)
		}
	})

	t.Run("MEDIA: mid-sentence keeps surrounding text", func(t *testing.T) {
		msg := "Here is your report MEDIA:" + reportCanonical + " please review"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Here is your report  please review" {
			t.Errorf("surrounding text lost: %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
	})

	t.Run("multiple MEDIA: on same line", func(t *testing.T) {
		img := filepath.Join(tmpDir, "photo.png")
		msg := "MEDIA:" + reportCanonical + " MEDIA:" + img
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "" {
			t.Errorf("expected empty cleaned text, got %q", cleaned)
		}
		if len(media) != 2 {
			t.Fatalf("expected 2 media from same line, got %d", len(media))
		}
	})

	t.Run("MEDIA: path outside workspace is stripped but no attachment", func(t *testing.T) {
		msg := "File:\nMEDIA:" + outsidePath(workspaceCanonical, "etc/passwd") + "\nDone"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "File:\nDone" {
			t.Errorf("MEDIA: line not stripped: %q", cleaned)
		}
		if len(media) != 0 {
			t.Errorf("expected no media for outside-workspace path, got %d", len(media))
		}
	})

	t.Run("message with only MEDIA: lines", func(t *testing.T) {
		msg := "MEDIA:" + reportCanonical
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "" {
			t.Errorf("expected empty cleaned text, got %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
	})

	t.Run("audio_as_voice tag stripped", func(t *testing.T) {
		msg := "[[audio_as_voice]]\nMEDIA:" + filepath.Join(tmpDir, "voice.ogg") + "\nExtra text"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Extra text" {
			t.Errorf("unexpected cleaned text: %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
	})

	t.Run("multiple MEDIA: paths", func(t *testing.T) {
		img := filepath.Join(tmpDir, "photo.png")
		msg := "Files:\nMEDIA:" + reportCanonical + "\nMEDIA:" + img + "\nEnjoy!"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Files:\nEnjoy!" {
			t.Errorf("unexpected cleaned text: %q", cleaned)
		}
		if len(media) != 2 {
			t.Fatalf("expected 2 media, got %d", len(media))
		}
	})
}

func TestMimeFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/tmp/file.png", "image/png"},
		{"/tmp/file.jpg", "image/jpeg"},
		{"/tmp/file.mp4", "video/mp4"},
		{"/tmp/file.ogg", "audio/ogg"},
		{"/tmp/file.pdf", "application/pdf"},
		{"/tmp/file.doc", "application/msword"},
		{"/tmp/file.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"/tmp/file.xls", "application/vnd.ms-excel"},
		{"/tmp/file.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"/tmp/file.unknown", "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			if got := mimeFromPath(tt.path); got != tt.want {
				t.Errorf("mimeFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestValidateChannelTenant(t *testing.T) {
	tenantA := uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")
	tenantB := uuid.MustParse("019d1135-7087-7aa9-a2c7-cdaf7af851b1")

	tool := NewMessageTool("", true)

	t.Run("no checker configured allows all", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		if err := tool.validateChannelTenant(ctx, "telegram", "123"); err != nil {
			t.Errorf("expected nil, got error: %s", err.ForLLM)
		}
	})

	// Wire a mock checker.
	channels := map[string]uuid.UUID{
		"telegram":       tenantA,
		"tenant-b-tg":   tenantB,
	}
	tool.SetChannelTenantChecker(func(name string) (uuid.UUID, bool) {
		tid, ok := channels[name]
		return tid, ok
	})

	t.Run("same tenant allows", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		if err := tool.validateChannelTenant(ctx, "telegram", "123"); err != nil {
			t.Errorf("expected nil for same tenant, got: %s", err.ForLLM)
		}
	})

	t.Run("cross tenant blocks", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		err := tool.validateChannelTenant(ctx, "tenant-b-tg", "456")
		if err == nil {
			t.Fatal("expected error for cross-tenant send, got nil")
		}
		if !err.IsError {
			t.Error("expected IsError=true")
		}
	})

	t.Run("channel not found blocks", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		err := tool.validateChannelTenant(ctx, "nonexistent", "789")
		if err == nil {
			t.Fatal("expected error for missing channel, got nil")
		}
	})

	t.Run("nil channel tenant allows (legacy)", func(t *testing.T) {
		channels["legacy-ch"] = uuid.Nil
		ctx := store.WithTenantID(context.Background(), tenantA)
		if err := tool.validateChannelTenant(ctx, "legacy-ch", "123"); err != nil {
			t.Errorf("expected nil for legacy channel, got: %s", err.ForLLM)
		}
	})

	t.Run("nil context tenant allows (master/system)", func(t *testing.T) {
		ctx := context.Background() // no tenant in context
		if err := tool.validateChannelTenant(ctx, "tenant-b-tg", "456"); err != nil {
			t.Errorf("expected nil for master context, got: %s", err.ForLLM)
		}
	})
}
