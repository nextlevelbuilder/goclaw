package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The quota used to count a scope's ENTIRE history, so a team that had worked for
// months hit a permanent ceiling during ordinary work: a live workspace reached
// 103 files accumulated since April and write_file began failing with
// "workspace file limit reached (103/100)". Only recent writes may count.
func TestCountRecentScopeFilesIgnoresOldFiles(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-30 * 24 * time.Hour)
	for i := 0; i < 120; i++ {
		path := filepath.Join(dir, "old-"+string(rune('a'+i%26))+"-"+time.Duration(i).String()+".md")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if got := CountRecentScopeFiles(dir); got != 0 {
		t.Fatalf("120 month-old files counted as %d recent, want 0", got)
	}

	// Fresh writes still count, so a looping agent is still stopped.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "new-"+time.Duration(i).String()+".md"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := CountRecentScopeFiles(dir); got != 5 {
		t.Fatalf("recent count = %d, want 5", got)
	}
}

func TestCountRecentScopeFilesWindowBoundary(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "inside.md")
	outside := filepath.Join(dir, "outside.md")
	for _, p := range []string{inside, outside} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Just inside the 7-day window, and clearly outside it.
	justInside := time.Now().Add(-scopeQuotaWindow + time.Hour)
	wayOutside := time.Now().Add(-scopeQuotaWindow - time.Hour)
	if err := os.Chtimes(inside, justInside, justInside); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(outside, wayOutside, wayOutside); err != nil {
		t.Fatal(err)
	}
	if got := CountRecentScopeFiles(dir); got != 1 {
		t.Fatalf("boundary count = %d, want 1 (only the file inside the window)", got)
	}
}

func TestCountRecentScopeFilesSkipsDirsAndMissingScope(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web-fetch"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CountRecentScopeFiles(dir); got != 1 {
		t.Fatalf("count = %d, want 1 (directories are not files)", got)
	}
	// A scope that does not exist yet is empty, not an error — preserves the
	// pre-existing fail-open behaviour on first write into a new workspace.
	if got := CountRecentScopeFiles(filepath.Join(dir, "does-not-exist")); got != 0 {
		t.Fatalf("missing scope count = %d, want 0", got)
	}
}

// Regression guard on the real numbers from the live workspace: 95 files older
// than a week plus 8 written today must be allowed, where the old total-count
// quota rejected them at 103/100.
func TestCountRecentScopeFilesAllowsLiveWorkspaceShape(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-14 * 24 * time.Hour)
	for i := 0; i < 95; i++ {
		path := filepath.Join(dir, "hist-"+time.Duration(i).String()+".md")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 8; i++ {
		if err := os.WriteFile(filepath.Join(dir, "today-"+time.Duration(i).String()+".md"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := CountRecentScopeFiles(dir)
	if got >= maxFilesPerScope {
		t.Fatalf("live workspace shape still blocked: recent=%d limit=%d", got, maxFilesPerScope)
	}
	if got != 8 {
		t.Fatalf("recent count = %d, want 8", got)
	}
}
