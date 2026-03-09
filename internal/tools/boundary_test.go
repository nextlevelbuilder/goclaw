package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// helper to create a temp workspace with files
func setupWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Create a normal file
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create a subdirectory
	sub := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolvePath_NormalFile(t *testing.T) {
	ws := setupWorkspace(t)
	resolved, err := resolvePath("hello.txt", ws, true)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if filepath.Base(resolved) != "hello.txt" {
		t.Fatalf("expected hello.txt, got: %s", resolved)
	}
}

func TestResolvePath_NestedFile(t *testing.T) {
	ws := setupWorkspace(t)
	resolved, err := resolvePath("subdir/nested.txt", ws, true)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if filepath.Base(resolved) != "nested.txt" {
		t.Fatalf("expected nested.txt, got: %s", resolved)
	}
}

func TestResolvePath_AbsolutePath(t *testing.T) {
	ws := setupWorkspace(t)
	absPath := filepath.Join(ws, "hello.txt")
	resolved, err := resolvePath(absPath, ws, true)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if resolved != absPath {
		// canonical path might differ if ws has symlinks (e.g. /tmp on macOS)
		realAbs, _ := filepath.EvalSymlinks(absPath)
		if resolved != realAbs {
			t.Fatalf("expected %s or %s, got: %s", absPath, realAbs, resolved)
		}
	}
}

func TestResolvePath_TraversalBlocked(t *testing.T) {
	ws := setupWorkspace(t)
	_, err := resolvePath("../../etc/passwd", ws, true)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestResolvePath_AbsoluteEscapeBlocked(t *testing.T) {
	ws := setupWorkspace(t)
	_, err := resolvePath("/etc/passwd", ws, true)
	if err == nil {
		t.Fatal("expected error for absolute path outside workspace, got nil")
	}
}

func TestResolvePath_SymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create a file outside workspace
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink inside workspace pointing outside
	link := filepath.Join(ws, "evil_link")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolvePath("evil_link", ws, true)
	if err == nil {
		t.Fatal("expected error for symlink escaping workspace, got nil")
	}
}

func TestResolvePath_SymlinkInsideWorkspaceAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create symlink pointing to a file within workspace
	target := filepath.Join(ws, "hello.txt")
	link := filepath.Join(ws, "good_link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolvePath("good_link", ws, true)
	if err != nil {
		t.Fatalf("expected success for symlink within workspace, got: %v", err)
	}

	// Should resolve to canonical path of target
	realTarget, _ := filepath.EvalSymlinks(target)
	if resolved != realTarget {
		t.Fatalf("expected %s, got: %s", realTarget, resolved)
	}
}

func TestResolvePath_BrokenSymlinkBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create symlink pointing to non-existent file outside workspace
	link := filepath.Join(ws, "broken_link")
	if err := os.Symlink("/nonexistent/secret", link); err != nil {
		t.Fatal(err)
	}

	_, err := resolvePath("broken_link", ws, true)
	if err == nil {
		t.Fatal("expected error for broken symlink outside workspace, got nil")
	}
}

func TestResolvePath_DirSymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create a directory symlink pointing outside workspace
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "evil_dir")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolvePath("evil_dir/secret.txt", ws, true)
	if err == nil {
		t.Fatal("expected error for directory symlink escape, got nil")
	}
}

func TestResolvePath_NonExistentFileInWorkspace(t *testing.T) {
	ws := setupWorkspace(t)
	resolved, err := resolvePath("new_file.txt", ws, true)
	if err != nil {
		t.Fatalf("expected success for non-existent file in workspace, got: %v", err)
	}
	if filepath.Dir(resolved) == "" {
		t.Fatal("expected resolved path to have directory")
	}
}

func TestResolvePath_UnrestrictedAllowsEscape(t *testing.T) {
	ws := setupWorkspace(t)
	// restrict=false should allow any path
	resolved, err := resolvePath("/etc/hosts", ws, false)
	if err != nil {
		t.Fatalf("expected success with restrict=false, got: %v", err)
	}
	if resolved != "/etc/hosts" {
		t.Fatalf("expected /etc/hosts, got: %s", resolved)
	}
}

func TestCheckHardlink_NormalFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "normal.txt")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkHardlink(f); err != nil {
		t.Fatalf("expected no error for normal file, got: %v", err)
	}
}

func TestCheckHardlink_HardlinkedFileBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hardlinks behave differently on Windows")
	}
	dir := t.TempDir()
	original := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(original, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "hardlink.txt")
	if err := os.Link(original, hardlink); err != nil {
		t.Fatal(err)
	}

	// Both original and hardlink should be rejected (nlink=2)
	if err := checkHardlink(original); err == nil {
		t.Fatal("expected error for hardlinked file (original), got nil")
	}
	if err := checkHardlink(hardlink); err == nil {
		t.Fatal("expected error for hardlinked file (link), got nil")
	}
}

func TestCheckHardlink_DirectoryAllowed(t *testing.T) {
	dir := t.TempDir()
	// Directories naturally have nlink > 1, should be exempt
	if err := checkHardlink(dir); err != nil {
		t.Fatalf("expected no error for directory, got: %v", err)
	}
}

func TestCheckHardlink_NonExistent(t *testing.T) {
	if err := checkHardlink("/nonexistent/path"); err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}
}

func TestCheckDeniedPath(t *testing.T) {
	ws := setupWorkspace(t)
	wsReal, _ := filepath.EvalSymlinks(ws)

	denied := filepath.Join(wsReal, ".goclaw", "secrets")
	if err := os.MkdirAll(filepath.Dir(denied), 0755); err != nil {
		t.Fatal(err)
	}

	err := checkDeniedPath(denied, ws, []string{".goclaw"})
	if err == nil {
		t.Fatal("expected error for denied path, got nil")
	}

	// Non-denied path should pass
	err = checkDeniedPath(filepath.Join(wsReal, "hello.txt"), ws, []string{".goclaw"})
	if err != nil {
		t.Fatalf("expected no error for non-denied path, got: %v", err)
	}
}

func TestIsPathInside(t *testing.T) {
	tests := []struct {
		child, parent string
		want          bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/bc", "/a/b", false}, // not a child, just prefix match
		{"/a", "/a/b", false},
		{"/x/y", "/a/b", false},
	}
	for _, tt := range tests {
		got := isPathInside(tt.child, tt.parent)
		if got != tt.want {
			t.Errorf("isPathInside(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
		}
	}
}
