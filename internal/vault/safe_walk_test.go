package vault

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeWalkWorkspace_BasicFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "notes/meeting.md", "hello")
	writeFile(t, dir, "report.txt", "world")
	writeFile(t, dir, "images/screenshot.png", "png-data")

	entries, stats, err := SafeWalkWorkspace(context.Background(), dir, DefaultWalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if stats.Eligible != 3 {
		t.Errorf("stats.Eligible = %d, want 3", stats.Eligible)
	}
	if stats.Truncated {
		t.Error("unexpected truncation")
	}
}

func TestSafeWalkWorkspace_SkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}
	dir := t.TempDir()
	writeFile(t, dir, "real.txt", "data")
	// Symlink file
	os.Symlink(filepath.Join(dir, "real.txt"), filepath.Join(dir, "link.txt"))
	// Symlink dir (pointing outside workspace)
	os.Symlink("/tmp", filepath.Join(dir, "escape"))

	entries, stats, err := SafeWalkWorkspace(context.Background(), dir, DefaultWalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (only real.txt)", len(entries))
	}
	if entries[0].RelPath != "real.txt" {
		t.Errorf("entry path = %q, want real.txt", entries[0].RelPath)
	}
	if stats.SkippedSymlinks != 2 {
		t.Errorf("stats.SkippedSymlinks = %d, want 2", stats.SkippedSymlinks)
	}
}

func TestSafeWalkWorkspace_ExcludedPaths(t *testing.T) {
	dir := t.TempDir()
	// Should be excluded
	writeFile(t, dir, "memory/session.json", "x")
	writeFile(t, dir, ".hidden/file.txt", "x")
	writeFile(t, dir, ".wrangler/config.json", "x")
	writeFile(t, dir, ".media/thumb.png", "x")
	writeFile(t, dir, "SOUL.md", "x")
	writeFile(t, dir, "IDENTITY.md", "x")
	writeFile(t, dir, "USER.md", "x")
	writeFile(t, dir, "BOOTSTRAP.md", "x")
	writeFile(t, dir, "AGENTS.md", "x")
	writeFile(t, dir, "TOOLS.md", "x")
	writeFile(t, dir, "CAPABILITIES.md", "x")
	writeFile(t, dir, "MEMORY.md", "x")
	writeFile(t, dir, "data.db", "x")
	writeFile(t, dir, "data.db-wal", "x")
	writeFile(t, dir, "data.db-shm", "x")
	writeFile(t, dir, "web-fetch/page.txt", "x") // excluded: external content
	// Should NOT be excluded
	writeFile(t, dir, "notes/meeting.md", "x")
	writeFile(t, dir, ".uploads/photo.jpg", "x")
	writeFile(t, dir, "soul-notes.md", "x")
	writeFile(t, dir, "deep/SOUL.md", "x") // not root level
	writeFile(t, dir, "teams/abc-123/doc.md", "x")

	entries, stats, err := SafeWalkWorkspace(context.Background(), dir, DefaultWalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.RelPath
		}
		t.Fatalf("got %d entries %v, want 6 non-excluded", len(entries), names)
	}
	if stats.SkippedExcluded == 0 {
		t.Error("expected some excluded files")
	}
}

func TestSafeWalkWorkspace_MaxFileLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		writeFile(t, dir, filepath.Join("files", string(rune('a'+i))+".txt"), "data")
	}

	opts := DefaultWalkOptions()
	opts.MaxFiles = 10
	entries, stats, err := SafeWalkWorkspace(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 10 {
		t.Fatalf("got %d entries, want <=10", len(entries))
	}
	if !stats.Truncated {
		t.Error("expected truncated=true")
	}
}

func TestSafeWalkWorkspace_MaxTotalBytes(t *testing.T) {
	dir := t.TempDir()
	// Create files that exceed total byte limit.
	bigContent := make([]byte, 1024) // 1KB each
	for i := 0; i < 10; i++ {
		writeFile(t, dir, filepath.Join("data", string(rune('a'+i))+".bin"), string(bigContent))
	}

	opts := DefaultWalkOptions()
	opts.MaxTotalBytes = 5 * 1024 // 5KB limit
	entries, stats, err := SafeWalkWorkspace(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) >= 10 {
		t.Fatalf("got %d entries, expected fewer than 10 due to size limit", len(entries))
	}
	if !stats.Truncated {
		t.Error("expected truncated=true")
	}
	_ = stats
}

func TestSafeWalkWorkspace_ContextCancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, dir, filepath.Join("files", string(rune('a'+i/26))+"_"+string(rune('a'+i%26))+".txt"), "data")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := SafeWalkWorkspace(ctx, dir, DefaultWalkOptions())
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestSafeWalkWorkspace_PerFileSizeSkip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "small.txt", "ok")
	// Create a file larger than MaxFileBytes
	bigContent := make([]byte, 100*1024) // 100KB
	writeFile(t, dir, "huge.bin", string(bigContent))

	opts := DefaultWalkOptions()
	opts.MaxFileBytes = 50 * 1024 // 50KB per-file limit
	entries, stats, err := SafeWalkWorkspace(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (only small.txt)", len(entries))
	}
	if stats.SkippedTooLarge != 1 {
		t.Errorf("stats.SkippedTooLarge = %d, want 1", stats.SkippedTooLarge)
	}
}

func TestIsExcludedPath(t *testing.T) {
	tests := []struct {
		path     string
		excluded bool
	}{
		{"memory/session-123.json", true},
		{"memory/deep/nested.md", true},
		{".hidden/file.txt", true},
		{".wrangler/config.json", true},
		{".media/thumb.png", true},
		{"SOUL.md", true},
		{"IDENTITY.md", true},
		{"USER.md", true},
		{"BOOTSTRAP.md", true},
		{"AGENTS.md", true},
		{"TOOLS.md", true},
		{"CAPABILITIES.md", true},
		{"MEMORY.md", true},
		{"AGENTS_CORE.md", true},
		{"AGENTS_TASK.md", true},
		{"data.db", true},
		{"data.db-wal", true},
		{"data.db-shm", true},
		// NOT excluded:
		{"notes/meeting.md", false},
		{"web-fetch/page.txt", true},
		{"agents/my-bot/web-fetch/data.txt", true},
		{"images/screenshot.png", false},
		{"teams/abc-123/doc.md", false},
		{"soul-notes.md", false},
		{"deep/SOUL.md", false},   // not root-level context file
		{".uploads/photo.jpg", false},
		{"report.pdf", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isExcludedPath(tt.path)
			if got != tt.excluded {
				t.Errorf("isExcludedPath(%q) = %v, want %v", tt.path, got, tt.excluded)
			}
		})
	}
}

// writeFile creates a file with the given relative path and content inside dir.
func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	abs := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
