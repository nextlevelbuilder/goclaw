package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestBridge creates a ToolBridge with a real temp workspace.
// The returned dir is the symlink-resolved path so comparisons on macOS
// (where /var/folders is a symlink to /private/var/folders) don't fail.
func newTestBridge(t *testing.T, opts ...ToolBridgeOption) (*ToolBridge, string) {
	t.Helper()
	raw := t.TempDir()
	// Resolve symlinks so workspace in ToolBridge matches what EvalSymlinks returns
	dir, err := filepath.EvalSymlinks(raw)
	if err != nil {
		dir = raw
	}
	tb := NewToolBridge(dir, opts...)
	return tb, dir
}

// --- NewToolBridge / options ---

func TestNewToolBridge_Defaults(t *testing.T) {
	tb, _ := newTestBridge(t)
	if tb.workspace == "" {
		t.Error("workspace should not be empty")
	}
	if tb.permMode != "approve-all" {
		t.Errorf("expected default permMode 'approve-all', got %q", tb.permMode)
	}
	if tb.maxOutputBytes != 10*1024*1024 {
		t.Errorf("expected 10MB maxOutputBytes, got %d", tb.maxOutputBytes)
	}
}

func TestWithPermMode_Empty_KeepsDefault(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode(""))
	if tb.permMode != "approve-all" {
		t.Errorf("empty permMode should keep default, got %q", tb.permMode)
	}
}

func TestWithPermMode_Set(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("deny-all"))
	if tb.permMode != "deny-all" {
		t.Errorf("expected deny-all, got %q", tb.permMode)
	}
}

func TestWithDenyPatterns(t *testing.T) {
	pat := regexp.MustCompile(`rm -rf`)
	tb, _ := newTestBridge(t, WithDenyPatterns([]*regexp.Regexp{pat}))
	if len(tb.denyPatterns) != 1 {
		t.Errorf("expected 1 deny pattern, got %d", len(tb.denyPatterns))
	}
}

// --- resolvePath boundary tests ---

func TestResolvePath_WithinWorkspace(t *testing.T) {
	tb, dir := newTestBridge(t)
	// Create a file so EvalSymlinks succeeds
	f := filepath.Join(dir, "file.txt")
	os.WriteFile(f, []byte("x"), 0644)
	got, err := tb.resolvePath("file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, dir) {
		t.Errorf("expected path within %q, got %q", dir, got)
	}
}

func TestResolvePath_AbsoluteInsideWorkspace(t *testing.T) {
	tb, dir := newTestBridge(t)
	f := filepath.Join(dir, "abs.txt")
	os.WriteFile(f, []byte("x"), 0644)
	got, err := tb.resolvePath(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both got and f should resolve to the same real path
	wantReal, _ := filepath.EvalSymlinks(f)
	if wantReal == "" {
		wantReal = f
	}
	if got != wantReal {
		t.Errorf("expected %q, got %q", wantReal, got)
	}
}

func TestResolvePath_Escape_PathTraversal(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.resolvePath("../../etc/passwd")
	if err == nil {
		t.Error("expected access denied for path traversal")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' in error, got %q", err.Error())
	}
}

func TestResolvePath_AbsoluteOutsideWorkspace(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.resolvePath("/etc/passwd")
	if err == nil {
		t.Error("expected access denied for absolute path outside workspace")
	}
}

func TestResolvePath_NonExistentFile_AllowedForWrites(t *testing.T) {
	tb, dir := newTestBridge(t)
	// File doesn't exist yet — resolvePath falls back to cleaned path (no EvalSymlinks).
	// The result may use /private prefix on macOS; check it's within the resolved dir.
	got, err := tb.resolvePath("newfile.txt")
	if err != nil {
		t.Fatalf("unexpected error for non-existent file: %v", err)
	}
	// Resolve both sides for comparison
	gotReal, _ := filepath.EvalSymlinks(filepath.Dir(got))
	if gotReal == "" {
		gotReal = filepath.Dir(got)
	}
	if !strings.HasPrefix(gotReal, dir) && gotReal != dir {
		t.Errorf("expected path within workspace %q, got dir %q", dir, gotReal)
	}
}

// --- handlePermission ---

func TestHandlePermission_ApproveAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("approve-all"))
	resp, err := tb.handlePermission(RequestPermissionRequest{ToolName: "bash", Description: "run"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Outcome != "approved" {
		t.Errorf("expected 'approved', got %q", resp.Outcome)
	}
}

func TestHandlePermission_DenyAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("deny-all"))
	resp, err := tb.handlePermission(RequestPermissionRequest{ToolName: "any_tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Outcome != "denied" {
		t.Errorf("expected 'denied', got %q", resp.Outcome)
	}
}

func TestHandlePermission_ApproveReads_ReadTool(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("approve-reads"))
	cases := []string{"readFile", "glob_files", "search_code", "list_dir", "grep_search", "view_file"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			resp, err := tb.handlePermission(RequestPermissionRequest{ToolName: name})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Outcome != "approved" {
				t.Errorf("expected approved for %q, got %q", name, resp.Outcome)
			}
		})
	}
}

func TestHandlePermission_ApproveReads_WriteTool(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("approve-reads"))
	resp, err := tb.handlePermission(RequestPermissionRequest{ToolName: "write_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Outcome != "denied" {
		t.Errorf("expected denied for write_file, got %q", resp.Outcome)
	}
}

func TestHandlePermission_DefaultMode_Approves(t *testing.T) {
	// permMode = "" defaults to "approve-all" behaviour (unknown → approve)
	tb := &ToolBridge{permMode: "unknown-mode"}
	resp, err := tb.handlePermission(RequestPermissionRequest{ToolName: "anything"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Outcome != "approved" {
		t.Errorf("expected approved, got %q", resp.Outcome)
	}
}

// --- readFile / writeFile ---

func TestReadFile_Success(t *testing.T) {
	tb, dir := newTestBridge(t)
	f := filepath.Join(dir, "read.txt")
	os.WriteFile(f, []byte("hello content"), 0644)

	resp, err := tb.readFile(ReadTextFileRequest{Path: "read.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello content" {
		t.Errorf("expected 'hello content', got %q", resp.Content)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.readFile(ReadTextFileRequest{Path: "missing.txt"})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadFile_PathEscape(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.readFile(ReadTextFileRequest{Path: "../../etc/passwd"})
	if err == nil {
		t.Error("expected path escape to be denied")
	}
}

func TestWriteFile_Success(t *testing.T) {
	tb, dir := newTestBridge(t)
	resp, err := tb.writeFile(WriteTextFileRequest{Path: "out.txt", Content: "written"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "out.txt"))
	if string(got) != "written" {
		t.Errorf("expected 'written', got %q", string(got))
	}
}

func TestWriteFile_CreatesSubdirs(t *testing.T) {
	tb, dir := newTestBridge(t)
	_, err := tb.writeFile(WriteTextFileRequest{Path: "sub/dir/file.txt", Content: "nested"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "sub", "dir", "file.txt"))
	if string(got) != "nested" {
		t.Errorf("expected 'nested', got %q", string(got))
	}
}

func TestWriteFile_PathEscape(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.writeFile(WriteTextFileRequest{Path: "../../evil.txt", Content: "x"})
	if err == nil {
		t.Error("expected path escape to be denied")
	}
}

// --- Handle dispatch ---

func TestHandle_FsReadTextFile(t *testing.T) {
	tb, dir := newTestBridge(t)
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("file data"), 0644)

	params, _ := json.Marshal(ReadTextFileRequest{Path: "test.txt"})
	result, err := tb.Handle(context.Background(), "fs/readTextFile", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := result.(*ReadTextFileResponse)
	if resp.Content != "file data" {
		t.Errorf("expected 'file data', got %q", resp.Content)
	}
}

func TestHandle_FsReadTextFile_DenyAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("deny-all"))
	params, _ := json.Marshal(ReadTextFileRequest{Path: "x.txt"})
	_, err := tb.Handle(context.Background(), "fs/readTextFile", params)
	if err == nil {
		t.Error("expected deny-all to reject read")
	}
}

func TestHandle_FsWriteTextFile(t *testing.T) {
	tb, dir := newTestBridge(t)
	params, _ := json.Marshal(WriteTextFileRequest{Path: "w.txt", Content: "hello"})
	_, err := tb.Handle(context.Background(), "fs/writeTextFile", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "w.txt"))
	if string(got) != "hello" {
		t.Errorf("expected 'hello', got %q", string(got))
	}
}

func TestHandle_FsWriteTextFile_DenyAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("deny-all"))
	params, _ := json.Marshal(WriteTextFileRequest{Path: "x.txt", Content: "x"})
	_, err := tb.Handle(context.Background(), "fs/writeTextFile", params)
	if err == nil {
		t.Error("expected deny-all to reject write")
	}
}

func TestHandle_FsWriteTextFile_ApproveReads(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("approve-reads"))
	params, _ := json.Marshal(WriteTextFileRequest{Path: "x.txt", Content: "x"})
	_, err := tb.Handle(context.Background(), "fs/writeTextFile", params)
	if err == nil {
		t.Error("expected approve-reads to reject write")
	}
}

func TestHandle_PermissionRequest(t *testing.T) {
	tb, _ := newTestBridge(t)
	params, _ := json.Marshal(RequestPermissionRequest{ToolName: "bash"})
	result, err := tb.Handle(context.Background(), "permission/request", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := result.(*RequestPermissionResponse)
	if resp.Outcome != "approved" {
		t.Errorf("expected approved, got %q", resp.Outcome)
	}
}

func TestHandle_UnknownMethod(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.Handle(context.Background(), "unknown/method", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for unknown method")
	}
	if !strings.Contains(err.Error(), "unknown method") {
		t.Errorf("expected 'unknown method' in error, got %q", err.Error())
	}
}

func TestHandle_MalformedParams(t *testing.T) {
	tb, _ := newTestBridge(t)
	methods := []string{
		"fs/readTextFile", "fs/writeTextFile",
		"terminal/output", "terminal/release",
		"terminal/waitForExit", "terminal/kill",
		"permission/request",
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			_, err := tb.Handle(context.Background(), m, json.RawMessage(`{invalid json`))
			if err == nil {
				t.Errorf("expected error for malformed params on %q", m)
			}
		})
	}
}

func TestHandle_TerminalCreate_DenyAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("deny-all"))
	params, _ := json.Marshal(CreateTerminalRequest{Command: "ls"})
	_, err := tb.Handle(context.Background(), "terminal/create", params)
	if err == nil {
		t.Error("expected deny-all to reject terminal/create")
	}
}

func TestHandle_TerminalCreate_ApproveReads(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("approve-reads"))
	params, _ := json.Marshal(CreateTerminalRequest{Command: "ls"})
	_, err := tb.Handle(context.Background(), "terminal/create", params)
	if err == nil {
		t.Error("expected approve-reads to reject terminal/create")
	}
}

func TestHandle_TerminalOutput_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	params, _ := json.Marshal(TerminalOutputRequest{TerminalID: "nonexistent"})
	_, err := tb.Handle(context.Background(), "terminal/output", params)
	if err == nil {
		t.Error("expected error for nonexistent terminal")
	}
}

func TestHandle_TerminalRelease_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	params, _ := json.Marshal(ReleaseTerminalRequest{TerminalID: "nonexistent"})
	result, err := tb.Handle(context.Background(), "terminal/release", params)
	// releaseTerminal returns success even for nonexistent ID
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestHandle_TerminalKill_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	params, _ := json.Marshal(KillTerminalRequest{TerminalID: "nonexistent"})
	_, err := tb.Handle(context.Background(), "terminal/kill", params)
	if err == nil {
		t.Error("expected error for nonexistent terminal kill")
	}
}

func TestHandle_TerminalKill_DenyAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithPermMode("deny-all"))
	params, _ := json.Marshal(KillTerminalRequest{TerminalID: "t1"})
	_, err := tb.Handle(context.Background(), "terminal/kill", params)
	if err == nil {
		t.Error("expected deny-all to reject terminal/kill")
	}
}

func TestHandle_TerminalWaitForExit_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	params, _ := json.Marshal(WaitForTerminalExitRequest{TerminalID: "nonexistent"})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := tb.Handle(ctx, "terminal/waitForExit", params)
	if err == nil {
		t.Error("expected error for nonexistent terminal waitForExit")
	}
}

// --- Close kills all active terminals ---

func TestToolBridge_Close_KillsTerminals(t *testing.T) {
	tb, _ := newTestBridge(t)

	// Manually insert a fake terminal with a cancel function
	cancelled := false
	var mu sync.Mutex
	fakeTerm := &Terminal{
		id:     "t1",
		exited: make(chan struct{}),
		cancel: func() {
			mu.Lock()
			cancelled = true
			mu.Unlock()
		},
	}
	tb.terminals.Store("t1", fakeTerm)

	err := tb.Close()
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}

	mu.Lock()
	wasCancelled := cancelled
	mu.Unlock()

	if !wasCancelled {
		t.Error("expected terminal cancel to be called on Close")
	}
}

func TestToolBridge_Close_EmptyTerminals(t *testing.T) {
	tb, _ := newTestBridge(t)
	err := tb.Close()
	if err != nil {
		t.Fatalf("Close on empty bridge failed: %v", err)
	}
}
