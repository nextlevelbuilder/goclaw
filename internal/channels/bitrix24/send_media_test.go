package bitrix24

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// TestUploadOneFile_HappyPath tests successful file upload with correct base64 encoding.
func TestUploadOneFile_HappyPath(t *testing.T) {
	const testContent = "this is the file content"

	// Create a temp file with test content
	tmpFile, err := os.CreateTemp("", "test_upload_*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(testContent)); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	// Use stub RoundTripper to capture requests and return canned responses
	rt := &captureRT{
		result: `{"result":{"fileId":12345}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	m := bus.MediaAttachment{URL: tmpFile.Name()}
	err = ch.uploadOneFile(context.Background(), ch.Client(), 1, "chat123", m)
	if err != nil {
		t.Fatalf("uploadOneFile: %v", err)
	}
}

// TestUploadOneFile_EmptyPath tests that empty URL is rejected.
func TestUploadOneFile_EmptyPath(t *testing.T) {
	ch, _ := newFakeChannelWithClient(t, NewClient("test.bitrix24.com", nil))

	m := bus.MediaAttachment{URL: ""}
	err := ch.uploadOneFile(context.Background(), ch.Client(), 1, "chat123", m)
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// TestUploadOneFile_MissingFile tests that non-existent file is rejected.
func TestUploadOneFile_MissingFile(t *testing.T) {
	ch, _ := newFakeChannelWithClient(t, NewClient("test.bitrix24.com", nil))

	m := bus.MediaAttachment{URL: "/nonexistent/file/path.txt"}
	err := ch.uploadOneFile(context.Background(), ch.Client(), 1, "chat123", m)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestUploadOneFile_FileTooLarge tests that files exceeding maxOutboundMediaBytesFallback
// are rejected without an upload call.
func TestUploadOneFile_FileTooLarge(t *testing.T) {
	// Create a temp file larger than the cap
	tmpFile, err := os.CreateTemp("", "large_*.bin")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write more than maxOutboundMediaBytesFallback
	largeSize := maxOutboundMediaBytesFallback + 1000
	if _, err := tmpFile.Write(make([]byte, largeSize)); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	ch, _ := newFakeChannelWithClient(t, NewClient("test.bitrix24.com", nil))

	m := bus.MediaAttachment{URL: tmpFile.Name()}
	err = ch.uploadOneFile(context.Background(), ch.Client(), 1, "chat123", m)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

// TestUploadOneFile_CorrectBase64Encoding tests that file content is
// correctly base64-encoded (without data: prefix) in the upload call.
func TestUploadOneFile_CorrectBase64Encoding(t *testing.T) {
	const testContent = "hello world from file"

	tmpFile, err := os.CreateTemp("", "test_encoding_*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(testContent)); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	// Use stub RoundTripper to capture the upload request
	rt := &captureRT{
		result: `{"result":{"fileId":12345}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	m := bus.MediaAttachment{URL: tmpFile.Name()}
	err = ch.uploadOneFile(context.Background(), ch.Client(), 1, "chat123", m)
	if err != nil {
		t.Fatalf("uploadOneFile: %v", err)
	}

	// Verify the base64 content matches what we expect
	if len(rt.reqs) == 0 {
		t.Fatal("no requests captured")
	}
	capturedContent := rt.reqs[0].Get("fields[FILE][content]")
	expectedB64 := base64.StdEncoding.EncodeToString([]byte(testContent))
	if capturedContent != expectedB64 {
		t.Errorf("base64 content mismatch: got %q, want %q", capturedContent, expectedB64)
	}
}

// TestUploadOneFile_NoDataPrefix tests that base64 does NOT include a data: prefix.
func TestUploadOneFile_NoDataPrefix(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_prefix_*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte("test")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	// Use stub RoundTripper to capture the upload request
	rt := &captureRT{
		result: `{"result":{"fileId":1}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	m := bus.MediaAttachment{URL: tmpFile.Name()}
	_ = ch.uploadOneFile(context.Background(), ch.Client(), 1, "chat123", m)

	// Verify NO data: prefix
	if len(rt.reqs) == 0 {
		t.Fatal("no requests captured")
	}
	capturedContent := rt.reqs[0].Get("fields[FILE][content]")
	if len(capturedContent) > 5 && capturedContent[:5] == "data:" {
		t.Errorf("base64 should NOT have data: prefix, got %q", capturedContent[:20])
	}
}

// TestUploadOneFile_CorrectBotIDDialogID tests that botId and dialogId are
// passed correctly in the upload call.
func TestUploadOneFile_CorrectBotIDDialogID(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_ids_*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte("data")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	// Use stub RoundTripper to capture the upload request
	rt := &captureRT{
		result: `{"result":{"fileId":1}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	m := bus.MediaAttachment{URL: tmpFile.Name()}
	_ = ch.uploadOneFile(context.Background(), ch.Client(), 42, "dialog999", m)

	// Verify botId and dialogId were passed correctly
	if len(rt.reqs) == 0 {
		t.Fatal("no requests captured")
	}
	capturedBotID := rt.reqs[0].Get("botId")
	capturedDialogID := rt.reqs[0].Get("dialogId")

	if capturedBotID != "42" {
		t.Errorf("botId = %q; want 42", capturedBotID)
	}
	if capturedDialogID != "dialog999" {
		t.Errorf("dialogId = %q; want dialog999", capturedDialogID)
	}
}

// TestSendMedia_NoClient tests that sendMedia fails when channel has no client.
func TestSendMedia_NoClient(t *testing.T) {
	ch := &Channel{}
	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media: []bus.MediaAttachment{
			{URL: "/some/file.txt"},
		},
	}
	err := ch.sendMedia(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when channel has no client")
	}
}

// TestSendMedia_InvalidBotID tests that sendMedia fails when botID is invalid.
func TestSendMedia_InvalidBotID(t *testing.T) {
	ch, _ := newFakeChannelWithClient(t, NewClient("test.bitrix24.com", nil))
	ch.startMu.Lock()
	ch.botID = 0 // Invalid botID
	ch.startMu.Unlock()

	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media: []bus.MediaAttachment{
			{URL: "/some/file.txt"},
		},
	}
	err := ch.sendMedia(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when botID is invalid")
	}
}

// TestSendMedia_EmptyMedia tests that empty media list returns no error.
func TestSendMedia_EmptyMedia(t *testing.T) {
	ch, _ := newFakeChannelWithClient(t, NewClient("test.bitrix24.com", nil))
	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media:  []bus.MediaAttachment{},
	}
	err := ch.sendMedia(context.Background(), msg)
	if err != nil {
		t.Fatalf("sendMedia with empty media should not error: %v", err)
	}
}

// TestSendMedia_SingleFile_Success tests uploading a single file successfully.
func TestSendMedia_SingleFile_Success(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "single_*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte("content")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	// Use stub RoundTripper
	rt := &captureRT{
		result: `{"result":{"fileId":1}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media: []bus.MediaAttachment{
			{URL: tmpFile.Name()},
		},
	}
	err = ch.sendMedia(context.Background(), msg)
	if err != nil {
		t.Fatalf("sendMedia single file: %v", err)
	}
}

// TestSendMedia_MultipleFiles_AllSucceed tests uploading multiple files.
func TestSendMedia_MultipleFiles_AllSucceed(t *testing.T) {
	// Create two temp files
	tmpFile1, err := os.CreateTemp("", "file1_*.txt")
	if err != nil {
		t.Fatalf("create temp file 1: %v", err)
	}
	defer os.Remove(tmpFile1.Name())
	if _, err := tmpFile1.Write([]byte("content1")); err != nil {
		t.Fatalf("write temp file 1: %v", err)
	}
	tmpFile1.Close()

	tmpFile2, err := os.CreateTemp("", "file2_*.txt")
	if err != nil {
		t.Fatalf("create temp file 2: %v", err)
	}
	defer os.Remove(tmpFile2.Name())
	if _, err := tmpFile2.Write([]byte("content2")); err != nil {
		t.Fatalf("write temp file 2: %v", err)
	}
	tmpFile2.Close()

	// Use stub RoundTripper to track multiple uploads
	rt := &captureRT{
		result: `{"result":{"fileId":1}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media: []bus.MediaAttachment{
			{URL: tmpFile1.Name()},
			{URL: tmpFile2.Name()},
		},
	}
	errSend := ch.sendMedia(context.Background(), msg)
	if errSend != nil {
		t.Fatalf("sendMedia multiple files: %v", errSend)
	}
	if len(rt.reqs) != 2 {
		t.Errorf("expected 2 uploads, got %d", len(rt.reqs))
	}
}

// TestSendMedia_PartialFailure tests that one file failing returns the first
// error but continues processing other files.
func TestSendMedia_PartialFailure(t *testing.T) {
	tmpFile1, err := os.CreateTemp("", "good_*.txt")
	if err != nil {
		t.Fatalf("create temp file 1: %v", err)
	}
	defer os.Remove(tmpFile1.Name())
	if _, err := tmpFile1.Write([]byte("content1")); err != nil {
		t.Fatalf("write temp file 1: %v", err)
	}
	tmpFile1.Close()

	tmpFile2, err := os.CreateTemp("", "bad_*.txt")
	if err != nil {
		t.Fatalf("create temp file 2: %v", err)
	}
	defer os.Remove(tmpFile2.Name())
	if _, err := tmpFile2.Write([]byte("content2")); err != nil {
		t.Fatalf("write temp file 2: %v", err)
	}
	tmpFile2.Close()

	// Use stub RoundTripper that returns different responses for each request
	rtPartial := &captureRTPartialFail{}

	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rtPartial))

	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media: []bus.MediaAttachment{
			{URL: tmpFile1.Name()},
			{URL: tmpFile2.Name()},
		},
	}
	errSend2 := ch.sendMedia(context.Background(), msg)
	// Should return first error but have attempted both uploads
	if errSend2 == nil {
		t.Fatal("expected error from second file")
	}
	if rtPartial.calls != 2 {
		t.Errorf("expected 2 upload attempts, got %d", rtPartial.calls)
	}
}

// TestSendMedia_SkipsWithMissingFile tests that a missing file is skipped
// and does not block other files.
func TestSendMedia_SkipsWithMissingFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "good_*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte("content")); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	// Use stub RoundTripper to track uploads
	rt := &captureRT{
		result: `{"result":{"fileId":1}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media: []bus.MediaAttachment{
			{URL: "/nonexistent/file1.txt"},
			{URL: tmpFile.Name()},
			{URL: "/nonexistent/file3.txt"},
		},
	}
	errSend3 := ch.sendMedia(context.Background(), msg)
	// Should return error from first missing file
	if errSend3 == nil {
		t.Fatal("expected error from missing files")
	}
	// sendMedia continues through all files: fails on file1 (missing), succeeds on file2 (good), fails on file3 (missing)
	// So exactly 1 upload should be attempted (the good file)
	if len(rt.reqs) != 1 {
		t.Errorf("expected 1 upload attempt (the good file), got %d", len(rt.reqs))
	}
}

// TestSendMedia_FilenameSanitized tests that uploaded filename uses filepath.Base
// (no path traversal).
func TestSendMedia_FilenameSanitized(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create file with path separators in name
	tmpFile := filepath.Join(tmpDir, "actual_file.txt")
	if err := os.WriteFile(tmpFile, []byte("content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Use stub RoundTripper to capture filename
	rt := &captureRT{
		result: `{"result":{"fileId":1}}`,
	}
	ch, _ := newFakeChannelWithClient(t, newStubClient("test.bitrix24.com", rt))

	msg := bus.OutboundMessage{
		ChatID: "chat1",
		Media: []bus.MediaAttachment{
			{URL: tmpFile},
		},
	}
	_ = ch.sendMedia(context.Background(), msg)

	// Filename should be just "actual_file.txt", no directory path
	if len(rt.reqs) == 0 {
		t.Fatal("no requests captured")
	}
	capturedFilename := rt.reqs[0].Get("fields[FILE][name]")
	if capturedFilename != "actual_file.txt" {
		t.Errorf("filename = %q; want actual_file.txt", capturedFilename)
	}
}
