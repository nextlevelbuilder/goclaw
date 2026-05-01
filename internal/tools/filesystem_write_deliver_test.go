package tools

// Characterization tests for write_file deliver=true/false behavior.
// These pin the delivery chain side-effects so phase 03 (send_file tool) cannot
// silently regress Result.Media population or DeliveredMedia.Mark() call.
//
// Gap noted: message.go (line 123) only READS IsDelivered — it does NOT call Mark.
// Mark is called exclusively by write_file (filesystem_write.go:236, 281).
// Phase 03 will add send_file which must also call Mark. Tests here lock that contract.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// TestWriteFileDeliverTrue_PopulatesResultMedia asserts that write_file with
// deliver=true sets Result.Media with the resolved path and filename.
func TestWriteFileDeliverTrue_PopulatesResultMedia(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	tool := NewWriteFileTool(workspaceCanonical, true)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"path":    "report.csv",
		"content": "col1,col2\n1,2\n",
		"deliver": true,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Fatalf("expected 1 Media entry, got %d", len(result.Media))
	}
	gotPath := result.Media[0].Path
	wantPath := filepath.Join(workspaceCanonical, "report.csv")
	if gotPath != wantPath {
		t.Errorf("Media[0].Path = %q, want %q", gotPath, wantPath)
	}
	if result.Media[0].Filename != "report.csv" {
		t.Errorf("Media[0].Filename = %q, want %q", result.Media[0].Filename, "report.csv")
	}
}

// TestWriteFileDeliverTrue_MarksDeliveredMedia asserts that write_file with
// deliver=true calls dm.Mark(resolved) so message tool's self-send guard can detect it.
func TestWriteFileDeliverTrue_MarksDeliveredMedia(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	tool := NewWriteFileTool(workspaceCanonical, true)

	dm := NewDeliveredMedia()
	ctx := WithDeliveredMedia(context.Background(), dm)

	result := tool.Execute(ctx, map[string]any{
		"path":    "output.pdf",
		"content": "%PDF-1.4",
		"deliver": true,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	resolvedPath := filepath.Join(workspaceCanonical, "output.pdf")
	if !dm.IsDelivered(resolvedPath) {
		t.Errorf("expected dm.IsDelivered(%q) = true after write_file deliver=true, got false", resolvedPath)
	}
}

// TestWriteFileDeliverFalse_NoMediaNoMark asserts that write_file with
// deliver=false leaves Result.Media empty and does NOT call dm.Mark.
func TestWriteFileDeliverFalse_NoMediaNoMark(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	tool := NewWriteFileTool(workspaceCanonical, true)

	dm := NewDeliveredMedia()
	ctx := WithDeliveredMedia(context.Background(), dm)

	result := tool.Execute(ctx, map[string]any{
		"path":    "temp.json",
		"content": `{"key":"val"}`,
		"deliver": false,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 0 {
		t.Errorf("expected 0 Media entries for deliver=false, got %d", len(result.Media))
	}

	resolvedPath := filepath.Join(workspaceCanonical, "temp.json")
	if dm.IsDelivered(resolvedPath) {
		t.Errorf("expected dm.IsDelivered(%q) = false for deliver=false, got true", resolvedPath)
	}
}

// TestWriteFileDeliverDefault_IsTrue asserts that omitting the deliver arg
// defaults to deliver=true (per filesystem_write.go:110).
func TestWriteFileDeliverDefault_IsTrue(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	tool := NewWriteFileTool(workspaceCanonical, true)

	dm := NewDeliveredMedia()
	ctx := WithDeliveredMedia(context.Background(), dm)

	// No "deliver" key in args — default should be true.
	result := tool.Execute(ctx, map[string]any{
		"path":    "data.txt",
		"content": "hello",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if len(result.Media) != 1 {
		t.Errorf("expected 1 Media entry when deliver omitted (default true), got %d", len(result.Media))
	}

	resolvedPath := filepath.Join(workspaceCanonical, "data.txt")
	if !dm.IsDelivered(resolvedPath) {
		t.Errorf("expected dm.IsDelivered(%q) = true when deliver omitted, got false", resolvedPath)
	}
}

// TestWriteFileThenMessageBlocked characterizes the full delivery chain:
// write_file(deliver=true) → message(MEDIA:same_path) → blocked.
// This is the key regression guard for phase 03 — if send_file breaks Mark(),
// the message block will stop working.
func TestWriteFileThenMessageBlocked(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	writeTool := NewWriteFileTool(workspaceCanonical, true)
	msgTool := NewMessageTool(workspaceCanonical, true)
	msgTool.SetMessageBus(nil) // no bus — we only care about the block error

	dm := NewDeliveredMedia()
	ctx := context.Background()
	ctx = WithDeliveredMedia(ctx, dm)
	ctx = WithToolChannel(ctx, "telegram")
	ctx = WithToolChatID(ctx, "chat-42")

	// Step 1: write_file delivers the file and marks it.
	writeResult := writeTool.Execute(ctx, map[string]any{
		"path":    "invoice.pdf",
		"content": "%PDF-1.4 body",
		"deliver": true,
	})
	if writeResult.IsError {
		t.Fatalf("write_file failed: %s", writeResult.ForLLM)
	}

	// Step 2: message(MEDIA:path) for same file must be blocked.
	resolvedPath := filepath.Join(workspaceCanonical, "invoice.pdf")
	msgResult := msgTool.Execute(ctx, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  "chat-42",
		"message": "MEDIA:" + resolvedPath,
	})
	if !msgResult.IsError {
		t.Fatal("expected message(MEDIA:path) to be blocked after write_file deliver=true, but it was allowed")
	}
}

// TestMessageMediaMarksDelivered verifies that message(MEDIA:path) calls dm.Mark()
// on the file it sends (patched in phase 03). This closes the cross-tool duplicate
// gap: send_file after message(MEDIA:) now correctly detects the duplicate.
func TestMessageMediaMarksDelivered(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	// Create a real file so MEDIA: resolution succeeds.
	filePath := filepath.Join(workspaceCanonical, "attachment.csv")
	if err := os.WriteFile(filePath, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	msgTool := NewMessageTool(workspaceCanonical, true)
	// Need a message bus for sendMedia to proceed past the nil-bus guard.
	msgTool.SetMessageBus(bus.New())

	dm := NewDeliveredMedia()
	ctx := context.Background()
	ctx = WithDeliveredMedia(ctx, dm)
	// Unbound session (no channel/chatID) — MEDIA send is allowed.

	result := msgTool.Execute(ctx, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  "chat-99",
		"message": "MEDIA:" + filePath,
	})

	// message(MEDIA:) should succeed and mark the file as delivered.
	if result.IsError {
		t.Fatalf("expected message(MEDIA:path) to succeed, got error: %s", result.ForLLM)
	}
	if !dm.IsDelivered(filePath) {
		t.Errorf("expected dm.IsDelivered(%q) = true after message(MEDIA:path), got false — "+
			"check message.go sendMedia: dm.Mark call missing", filePath)
	}
}
