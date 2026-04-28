package tools

// TDD red-state tests for send_file tool.
// NewSendFileTool does NOT exist yet — this file will fail to compile until
// phase 03 adds send_file.go. That compile failure is the intended red state.
//
// Run to confirm red state:
//   go test -v ./internal/tools/... -run TestSendFile 2>&1 | grep -i 'undefined\|SendFileTool'
// Run to confirm no regression:
//   go test ./internal/tools/... -run 'TestMessage|TestWriteFile|TestDeliveredMedia'

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkSendFileWorkspace creates a temp workspace with a small set of test files.
// Returns (workspaceCanonical, reportPDF, subFile, subDir).
func mkSendFileWorkspace(t *testing.T) (ws, reportPDF, subFile, subDir string) {
	t.Helper()
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	// workspace/report.pdf
	reportPDF = filepath.Join(workspaceCanonical, "report.pdf")
	if err := os.WriteFile(reportPDF, []byte("%PDF-1.4 body"), 0o644); err != nil {
		t.Fatal(err)
	}

	// workspace/subdir/file.txt
	subDir = filepath.Join(workspaceCanonical, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subFile = filepath.Join(subDir, "file.txt")
	if err := os.WriteFile(subFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	return workspaceCanonical, reportPDF, subFile, subDir
}

// TestSendFile_T1_HappyPath verifies that send_file on an existing file succeeds
// with Result.Media populated, correct path, filename, and MIME type.
func TestSendFile_T1_HappyPath(t *testing.T) {
	ws, reportPDF, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path": reportPDF,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected 1 Media entry, got %d", len(result.Media))
	}
	if result.Media[0].Path != reportPDF {
		t.Errorf("Media[0].Path = %q, want %q", result.Media[0].Path, reportPDF)
	}
	if result.Media[0].Filename != "report.pdf" {
		t.Errorf("Media[0].Filename = %q, want %q", result.Media[0].Filename, "report.pdf")
	}
	if result.Media[0].MimeType != "application/pdf" {
		t.Errorf("Media[0].MimeType = %q, want %q", result.Media[0].MimeType, "application/pdf")
	}
}

// TestSendFile_T2_WithCaption verifies that a caption appears in ForLLM or ForUser
// and that Media is still populated.
func TestSendFile_T2_WithCaption(t *testing.T) {
	ws, reportPDF, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path":    reportPDF,
		"caption": "Q4 report",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected 1 Media entry, got %d", len(result.Media))
	}
	caption := result.ForLLM + result.ForUser
	if !strings.Contains(caption, "Q4 report") {
		t.Errorf("expected caption %q in ForLLM/ForUser, got ForLLM=%q ForUser=%q",
			"Q4 report", result.ForLLM, result.ForUser)
	}
}

// TestSendFile_T3_MissingPath verifies that omitting the "path" param returns an error.
func TestSendFile_T3_MissingPath(t *testing.T) {
	ws, _, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{})

	if !result.IsError {
		t.Fatal("expected error for missing path, got success")
	}
	if !strings.Contains(strings.ToLower(result.ForLLM), "path") {
		t.Errorf("expected error to mention 'path', got: %s", result.ForLLM)
	}
}

// TestSendFile_T4_FileNotFound verifies that a non-existent path returns an error.
func TestSendFile_T4_FileNotFound(t *testing.T) {
	ws, _, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path": filepath.Join(ws, "nonexistent.pdf"),
	})

	if !result.IsError {
		t.Fatal("expected error for non-existent file, got success")
	}
	msg := strings.ToLower(result.ForLLM)
	if !strings.Contains(msg, "not found") && !strings.Contains(msg, "does not exist") &&
		!strings.Contains(msg, "no such file") {
		t.Errorf("expected error to mention 'not found' or similar, got: %s", result.ForLLM)
	}
}

// TestSendFile_T5_DirectoryRejected verifies that passing a directory path returns an error.
func TestSendFile_T5_DirectoryRejected(t *testing.T) {
	ws, _, _, subDir := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path": subDir,
	})

	if !result.IsError {
		t.Fatal("expected error for directory path, got success")
	}
	msg := strings.ToLower(result.ForLLM)
	if !strings.Contains(msg, "regular file") && !strings.Contains(msg, "directory") &&
		!strings.Contains(msg, "not a file") {
		t.Errorf("expected error to mention directory/regular-file, got: %s", result.ForLLM)
	}
}

// TestSendFile_T6_PathTraversalBlocked verifies that path traversal with restrict=true is blocked.
func TestSendFile_T6_PathTraversalBlocked(t *testing.T) {
	ws, _, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	// Try to escape workspace using traversal.
	traversalPath := filepath.Join(ws, "..", "..", "etc", "passwd")
	result := tool.Execute(ctx, map[string]any{
		"path": traversalPath,
	})

	if !result.IsError {
		t.Fatal("expected error for path traversal, got success")
	}
}

// TestSendFile_T7_RelativePathResolvesAgainstWorkspace verifies that a relative path
// like "subdir/file.txt" resolves against the workspace root.
func TestSendFile_T7_RelativePathResolvesAgainstWorkspace(t *testing.T) {
	ws, _, subFile, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path": "subdir/file.txt",
	})

	if result.IsError {
		t.Fatalf("expected success for relative path, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected 1 Media entry, got %d", len(result.Media))
	}
	// Canonical form comparison — both paths should point to same file.
	gotCanonical, _ := filepath.EvalSymlinks(result.Media[0].Path)
	wantCanonical, _ := filepath.EvalSymlinks(subFile)
	if gotCanonical != wantCanonical {
		t.Errorf("Media[0].Path resolved to %q, want %q", gotCanonical, wantCanonical)
	}
}

// TestSendFile_T8_AbsoluteInsideWorkspaceAllowed verifies that an absolute path
// inside the workspace succeeds with restrict=true.
func TestSendFile_T8_AbsoluteInsideWorkspaceAllowed(t *testing.T) {
	ws, reportPDF, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path": reportPDF, // absolute path inside workspace
	})

	if result.IsError {
		t.Fatalf("expected success for absolute path inside workspace, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected 1 Media entry, got %d", len(result.Media))
	}
}

// TestSendFile_T9_DuplicateBlocksSameToolCall verifies that calling send_file twice
// with the same path in the same ctx causes the second call to return an error.
func TestSendFile_T9_DuplicateBlocksSameToolCall(t *testing.T) {
	ws, reportPDF, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	dm := NewDeliveredMedia()
	ctx := WithDeliveredMedia(context.Background(), dm)

	// First call — should succeed.
	first := tool.Execute(ctx, map[string]any{
		"path": reportPDF,
	})
	if first.IsError {
		t.Fatalf("first send_file: expected success, got error: %s", first.ForLLM)
	}

	// Second call — same path, same ctx — must be blocked.
	second := tool.Execute(ctx, map[string]any{
		"path": reportPDF,
	})
	if !second.IsError {
		t.Fatal("second send_file (dup): expected error (already delivered), got success")
	}
	msg := strings.ToLower(second.ForLLM)
	if !strings.Contains(msg, "already") {
		t.Errorf("expected error to mention 'already delivered/sent', got: %s", second.ForLLM)
	}
}

// TestSendFile_T10_MarksDeliveredMedia verifies that after send_file succeeds,
// dm.IsDelivered(resolvedPath) returns true.
func TestSendFile_T10_MarksDeliveredMedia(t *testing.T) {
	ws, reportPDF, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	dm := NewDeliveredMedia()
	ctx := WithDeliveredMedia(context.Background(), dm)

	result := tool.Execute(ctx, map[string]any{
		"path": reportPDF,
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	if !dm.IsDelivered(reportPDF) {
		t.Errorf("expected dm.IsDelivered(%q) = true after send_file, got false", reportPDF)
	}
}

// TestSendFile_T11_SubsequentMessageMediaBlocked verifies that after send_file
// marks a path, a subsequent message(MEDIA:path) self-send is blocked.
func TestSendFile_T11_SubsequentMessageMediaBlocked(t *testing.T) {
	ws, reportPDF, _, _ := mkSendFileWorkspace(t)
	sendTool := NewSendFileTool(ws, true)
	msgTool := NewMessageTool(ws, true)
	msgTool.SetMessageBus(nil)

	dm := NewDeliveredMedia()
	ctx := context.Background()
	ctx = WithDeliveredMedia(ctx, dm)
	ctx = WithToolChannel(ctx, "telegram")
	ctx = WithToolChatID(ctx, "chat-42")

	// Step 1: send_file delivers and marks the file.
	sendResult := sendTool.Execute(ctx, map[string]any{
		"path": reportPDF,
	})
	if sendResult.IsError {
		t.Fatalf("send_file step failed: %s", sendResult.ForLLM)
	}

	// Step 2: message(MEDIA:path) for same file — must be blocked by self-send guard.
	msgResult := msgTool.Execute(ctx, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  "chat-42",
		"message": "MEDIA:" + reportPDF,
	})
	if !msgResult.IsError {
		t.Fatal("expected message(MEDIA:path) to be blocked after send_file, but it was allowed")
	}
}

// TestSendFile_T11b_BlocksAfterMessageMediaMarked verifies that send_file returns
// an error when the path was already marked by message(MEDIA:) success.
//
// NOTE: This test simulates the post-phase-03 state where message.go calls dm.Mark()
// on MEDIA: success. Currently message.go does NOT call Mark (documented gap in
// TestMessageMediaNoMark_DocumentedGap). Once phase 03 patches message.go, the
// manual dm.Mark call below can be replaced by running the message tool directly.
func TestSendFile_T11b_BlocksAfterMessageMediaMarked(t *testing.T) {
	ws, reportPDF, _, _ := mkSendFileWorkspace(t)
	tool := NewSendFileTool(ws, true)

	dm := NewDeliveredMedia()
	// Simulating post-phase-03 mark: once message.go is patched, this test can
	// call the message tool directly and remove this manual Mark call.
	dm.Mark(reportPDF)
	ctx := WithDeliveredMedia(context.Background(), dm)

	result := tool.Execute(ctx, map[string]any{
		"path": reportPDF,
	})
	if !result.IsError {
		t.Fatal("expected send_file to block path already marked by message(MEDIA:), got success")
	}
	msg := strings.ToLower(result.ForLLM)
	if !strings.Contains(msg, "already") {
		t.Errorf("expected error to mention 'already delivered/sent', got: %s", result.ForLLM)
	}
}

// TestSendFile_T11c_BlocksAfterWriteFileDeliverTrue verifies that send_file blocks
// when the path was already delivered via write_file(deliver=true).
func TestSendFile_T11c_BlocksAfterWriteFileDeliverTrue(t *testing.T) {
	ws, _, _, _ := mkSendFileWorkspace(t)
	writeTool := NewWriteFileTool(ws, true)
	sendTool := NewSendFileTool(ws, true)

	dm := NewDeliveredMedia()
	ctx := WithDeliveredMedia(context.Background(), dm)

	// Step 1: write_file deliver=true creates + marks the file.
	writeResult := writeTool.Execute(ctx, map[string]any{
		"path":    "data.csv",
		"content": "col1,col2\n1,2\n",
		"deliver": true,
	})
	if writeResult.IsError {
		t.Fatalf("write_file step failed: %s", writeResult.ForLLM)
	}
	resolvedPath := filepath.Join(ws, "data.csv")

	// Step 2: send_file for same path — must be blocked.
	sendResult := sendTool.Execute(ctx, map[string]any{
		"path": resolvedPath,
	})
	if !sendResult.IsError {
		t.Fatal("expected send_file to block path already delivered by write_file deliver=true, got success")
	}
	msg := strings.ToLower(sendResult.ForLLM)
	if !strings.Contains(msg, "already") {
		t.Errorf("expected error to mention 'already delivered/sent', got: %s", sendResult.ForLLM)
	}
}

// TestSendFile_T12_MimeDetection is a table-driven test for MIME type detection
// from file extension.
func TestSendFile_T12_MimeDetection(t *testing.T) {
	ws := t.TempDir()
	wsCanonical, _ := filepath.EvalSymlinks(ws)
	tool := NewSendFileTool(wsCanonical, true)

	cases := []struct {
		ext      string
		wantMime string
	}{
		{".pdf", "application/pdf"},
		{".png", "image/png"},
		{".unknown", "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			name := "testfile" + tc.ext
			fpath := filepath.Join(wsCanonical, name)
			if err := os.WriteFile(fpath, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()
			result := tool.Execute(ctx, map[string]any{
				"path": fpath,
			})
			if result.IsError {
				t.Fatalf("expected success for %s, got error: %s", tc.ext, result.ForLLM)
			}
			if len(result.Media) != 1 {
				t.Fatalf("expected 1 Media entry, got %d", len(result.Media))
			}
			if result.Media[0].MimeType != tc.wantMime {
				t.Errorf("MimeType for %s = %q, want %q", tc.ext, result.Media[0].MimeType, tc.wantMime)
			}
		})
	}
}

// TestSendFile_T14_DenyPathsBlocked verifies that send_file rejects paths covered by DenyPaths.
func TestSendFile_T14_DenyPathsBlocked(t *testing.T) {
	ws := t.TempDir()
	wsCanonical, _ := filepath.EvalSymlinks(ws)
	tool := NewSendFileTool(wsCanonical, true)
	tool.DenyPaths("memory.db", "config.json")

	// Create a denied file inside the workspace.
	deniedFile := filepath.Join(wsCanonical, "memory.db")
	if err := os.WriteFile(deniedFile, []byte("db-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path": "memory.db",
	})

	if !result.IsError {
		t.Fatal("expected error for denied path memory.db, got success")
	}
	msg := strings.ToLower(result.ForLLM)
	if !strings.Contains(msg, "denied") && !strings.Contains(msg, "restricted") {
		t.Errorf("expected error to mention 'denied' or 'restricted', got: %s", result.ForLLM)
	}
}

// TestSendFile_T13_BinaryFileLargeNoContentRead verifies that a large binary file
// is handled efficiently — the tool must not read the file contents, only stat + path.
// Test should complete well under 1 second even for 10MB.
func TestSendFile_T13_BinaryFileLargeNoContentRead(t *testing.T) {
	ws := t.TempDir()
	wsCanonical, _ := filepath.EvalSymlinks(ws)
	tool := NewSendFileTool(wsCanonical, true)

	// Write a 10MB random binary file.
	bigFile := filepath.Join(wsCanonical, "bigdata.bin")
	const size = 10 * 1024 * 1024
	data := make([]byte, size)
	_, _ = rand.Read(data)
	if err := os.WriteFile(bigFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path": bigFile,
	})

	if result.IsError {
		t.Fatalf("expected success for large binary, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected 1 Media entry, got %d", len(result.Media))
	}
	if result.Media[0].Path != bigFile {
		t.Errorf("Media[0].Path = %q, want %q", result.Media[0].Path, bigFile)
	}
}
