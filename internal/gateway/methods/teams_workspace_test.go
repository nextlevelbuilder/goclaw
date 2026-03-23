package methods

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveWorkspacePath_NormalFile(t *testing.T) {
	scope := t.TempDir()
	if err := os.WriteFile(filepath.Join(scope, "file.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	diskPath, err := resolveWorkspacePath(scope, "file.txt")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if filepath.Base(diskPath) != "file.txt" {
		t.Fatalf("expected file.txt, got: %s", diskPath)
	}
}

func TestResolveWorkspacePath_NestedFile(t *testing.T) {
	scope := t.TempDir()
	sub := filepath.Join(scope, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	diskPath, err := resolveWorkspacePath(scope, "sub/nested.txt")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if filepath.Base(diskPath) != "nested.txt" {
		t.Fatalf("expected nested.txt, got: %s", diskPath)
	}
}

func TestResolveWorkspacePath_TraversalBlocked(t *testing.T) {
	scope := t.TempDir()
	// Even though teams_workspace.go checks for ".." in the RPC handler,
	// resolveWorkspacePath should also catch it via boundary check.
	_, err := resolveWorkspacePath(scope, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestResolveWorkspacePath_SymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	scope := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Plant a symlink inside workspace pointing outside
	link := filepath.Join(scope, "evil_link")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolveWorkspacePath(scope, "evil_link")
	if err == nil {
		t.Fatal("expected error for symlink escaping workspace, got nil")
	}
}

func TestResolveWorkspacePath_DirSymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	scope := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Plant a directory symlink inside workspace pointing outside
	link := filepath.Join(scope, "evil_dir")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolveWorkspacePath(scope, "evil_dir/secret.txt")
	if err == nil {
		t.Fatal("expected error for dir symlink escape, got nil")
	}
}

func TestResolveWorkspacePath_SymlinkInsideAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	scope := t.TempDir()
	target := filepath.Join(scope, "real.txt")
	if err := os.WriteFile(target, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(scope, "good_link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolveWorkspacePath(scope, "good_link")
	if err != nil {
		t.Fatalf("expected success for symlink within workspace, got: %v", err)
	}
}

func TestResolveWorkspacePath_NonExistentFile(t *testing.T) {
	scope := t.TempDir()
	// Non-existent file in workspace should succeed (for new file creation)
	_, err := resolveWorkspacePath(scope, "new_file.txt")
	if err != nil {
		t.Fatalf("expected success for non-existent file, got: %v", err)
	}
}
