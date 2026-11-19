package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Regression tests for #1201: resolvePath must not fall back to the lexical
// path when EvalSymlinks fails, because os.MkdirAll / os.WriteFile follow
// symlinks and would then write outside the workspace.

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
}

func outsideDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to not exist (err=%v) — write escaped the workspace", path, err)
	}
}

// Dangling symlink inside the workspace pointing at a not-yet-existing file
// outside the workspace (the PoC from #1201).
func TestWriteFile_DanglingSymlinkOutside_Denied(t *testing.T) {
	tb, ws := newTestBridge(t)
	outside := filepath.Join(outsideDir(t), "pwned.txt")
	symlinkOrSkip(t, outside, filepath.Join(ws, "link.txt"))

	_, err := tb.writeFile(WriteTextFileRequest{Path: "link.txt", Content: "escaped"})
	if err == nil {
		t.Fatal("expected write through dangling symlink to be denied")
	}
	assertNotExist(t, outside)
}

// Same PoC through the public JSON-RPC entry point used by ACP agents.
func TestHandle_FsWriteTextFile_DanglingSymlinkOutside_Denied(t *testing.T) {
	tb, ws := newTestBridge(t)
	outside := filepath.Join(outsideDir(t), "pwned.txt")
	symlinkOrSkip(t, outside, filepath.Join(ws, "link.txt"))

	params, _ := json.Marshal(WriteTextFileRequest{Path: filepath.Join(ws, "link.txt"), Content: "escaped"})
	if _, err := tb.Handle(t.Context(), "fs/writeTextFile", params); err == nil {
		t.Fatal("expected fs/writeTextFile through dangling symlink to be denied")
	}
	assertNotExist(t, outside)
}

// A symlinked directory pointing outside: writing a new file beneath it makes
// EvalSymlinks fail (leaf missing) even though the parent resolves outside.
func TestWriteFile_NewFileUnderSymlinkedDirOutside_Denied(t *testing.T) {
	tb, ws := newTestBridge(t)
	out := outsideDir(t)
	symlinkOrSkip(t, out, filepath.Join(ws, "dirlink"))

	_, err := tb.writeFile(WriteTextFileRequest{Path: "dirlink/new.txt", Content: "escaped"})
	if err == nil {
		t.Fatal("expected write beneath symlinked outside directory to be denied")
	}
	assertNotExist(t, filepath.Join(out, "new.txt"))
}

// A dangling directory symlink: MkdirAll would create the outside directory
// tree before WriteFile runs.
func TestWriteFile_NestedUnderDanglingDirSymlink_Denied(t *testing.T) {
	tb, ws := newTestBridge(t)
	out := outsideDir(t)
	missing := filepath.Join(out, "created-by-agent")
	symlinkOrSkip(t, missing, filepath.Join(ws, "dl"))

	_, err := tb.writeFile(WriteTextFileRequest{Path: "dl/sub/f.txt", Content: "escaped"})
	if err == nil {
		t.Fatal("expected write beneath dangling outside directory symlink to be denied")
	}
	assertNotExist(t, missing)
}

// Chained dangling symlinks: link1 -> link2 (inside) -> outside missing file.
func TestWriteFile_ChainedDanglingSymlinkOutside_Denied(t *testing.T) {
	tb, ws := newTestBridge(t)
	outside := filepath.Join(outsideDir(t), "pwned.txt")
	symlinkOrSkip(t, outside, filepath.Join(ws, "link2"))
	symlinkOrSkip(t, "link2", filepath.Join(ws, "link1"))

	if _, err := tb.writeFile(WriteTextFileRequest{Path: "link1", Content: "escaped"}); err == nil {
		t.Fatal("expected write through chained dangling symlink to be denied")
	}
	assertNotExist(t, outside)
}

// Reads through a symlink to an existing outside file stay denied.
func TestReadFile_SymlinkToOutsideFile_Denied(t *testing.T) {
	tb, ws := newTestBridge(t)
	outside := filepath.Join(outsideDir(t), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(ws, "secret-link"))

	if _, err := tb.readFile(ReadTextFileRequest{Path: "secret-link"}); err == nil {
		t.Fatal("expected read through symlink to outside file to be denied")
	}
}

// Normal behaviour: a dangling symlink whose target stays inside the
// workspace is still writable, and the write lands on the target.
func TestWriteFile_DanglingSymlinkInsideWorkspace_Allowed(t *testing.T) {
	tb, ws := newTestBridge(t)
	if err := os.Mkdir(filepath.Join(ws, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, filepath.Join("out", "target.txt"), filepath.Join(ws, "alias.txt"))

	if _, err := tb.writeFile(WriteTextFileRequest{Path: "alias.txt", Content: "ok"}); err != nil {
		t.Fatalf("expected write through in-workspace dangling symlink to succeed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(ws, "out", "target.txt"))
	if err != nil || string(data) != "ok" {
		t.Fatalf("expected target to contain %q, got %q (err=%v)", "ok", data, err)
	}
}

// Normal behaviour: new nested directories inside the workspace still work,
// including beneath an in-workspace directory symlink.
func TestWriteFile_NestedUnderInWorkspaceDirSymlink_Allowed(t *testing.T) {
	tb, ws := newTestBridge(t)
	if err := os.Mkdir(filepath.Join(ws, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, "real", filepath.Join(ws, "alias"))

	if _, err := tb.writeFile(WriteTextFileRequest{Path: "alias/a/b/c.txt", Content: "ok"}); err != nil {
		t.Fatalf("expected nested write under in-workspace symlink to succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "real", "a", "b", "c.txt")); err != nil {
		t.Fatalf("expected file under real dir: %v", err)
	}
}

// Symlink loops must be rejected rather than resolved lexically.
func TestResolvePath_SymlinkLoop_Denied(t *testing.T) {
	tb, ws := newTestBridge(t)
	symlinkOrSkip(t, "loop-b", filepath.Join(ws, "loop-a"))
	symlinkOrSkip(t, "loop-a", filepath.Join(ws, "loop-b"))

	if _, err := tb.resolvePath("loop-a"); err == nil {
		t.Fatal("expected symlink loop to be denied")
	}
}
