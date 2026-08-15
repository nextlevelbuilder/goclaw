package agent

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// minimalPNG is a 1x1 red PNG (67 bytes) — real PNG magic bytes + valid IHDR/IDAT.
// Used to verify that PNG magic bytes survive the write path.
var minimalPNG = func() []byte {
	// base64 of a minimal 1x1 transparent PNG
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	b, _ := base64.StdEncoding.DecodeString(b64)
	return b
}()

// TestPersistAssistantImages_BasicPNG verifies that a final PNG image is written
// to {workspace}/media/{sha256}.png and Message.MediaRefs has one entry.
func TestPersistAssistantImages_BasicPNG(t *testing.T) {
	workspace := t.TempDir()

	msg := &providers.Message{
		Role: "assistant",
		Images: []providers.ImageContent{{
			MimeType: "image/png",
			Data:     base64.StdEncoding.EncodeToString(minimalPNG),
			Partial:  false,
		}},
	}

	persistAssistantImages(msg, workspace)

	// Images must be cleared after persistence.
	if len(msg.Images) != 0 {
		t.Fatalf("expected Images cleared, got %d entries", len(msg.Images))
	}
	// One MediaRef must be added.
	if len(msg.MediaRefs) != 1 {
		t.Fatalf("expected 1 MediaRef, got %d", len(msg.MediaRefs))
	}
	ref := msg.MediaRefs[0]
	if ref.Kind != "image" {
		t.Errorf("MediaRef.Kind = %q, want %q", ref.Kind, "image")
	}
	if ref.MimeType != "image/png" {
		t.Errorf("MediaRef.MimeType = %q, want %q", ref.MimeType, "image/png")
	}
	if !strings.HasSuffix(ref.Path, ".png") {
		t.Errorf("MediaRef.Path %q must end with .png", ref.Path)
	}

	// File must exist on disk with PNG magic bytes.
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		t.Fatalf("could not read persisted file: %v", err)
	}
	if len(data) < 4 || string(data[:4]) != "\x89PNG" {
		t.Errorf("persisted file does not have PNG magic bytes, got %x", data[:min(4, len(data))])
	}

	// File must live inside workspace/media/.
	mediaDir := filepath.Join(workspace, "media")
	rel, err := filepath.Rel(mediaDir, ref.Path)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("persisted path %q is not inside workspace/media/", ref.Path)
	}
}

// TestPersistAssistantImages_Dedup verifies that writing the same image twice
// results in only one disk file. Both calls append a MediaRef (two refs, one file).
func TestPersistAssistantImages_Dedup(t *testing.T) {
	workspace := t.TempDir()
	imgData := base64.StdEncoding.EncodeToString(minimalPNG)

	msg1 := &providers.Message{
		Images: []providers.ImageContent{{MimeType: "image/png", Data: imgData}},
	}
	msg2 := &providers.Message{
		Images: []providers.ImageContent{{MimeType: "image/png", Data: imgData}},
	}

	persistAssistantImages(msg1, workspace)
	persistAssistantImages(msg2, workspace)

	mediaDir := filepath.Join(workspace, "media")
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	// Same hash → exactly one file on disk.
	if len(entries) != 1 {
		t.Errorf("expected 1 file on disk (dedup), got %d", len(entries))
	}
	// Each message gets its own MediaRef pointing to the same path.
	if len(msg1.MediaRefs) != 1 {
		t.Errorf("msg1: expected 1 MediaRef, got %d", len(msg1.MediaRefs))
	}
	if len(msg2.MediaRefs) != 1 {
		t.Errorf("msg2: expected 1 MediaRef, got %d", len(msg2.MediaRefs))
	}
	if msg1.MediaRefs[0].Path != msg2.MediaRefs[0].Path {
		t.Errorf("both msgs should reference same path; got %q and %q",
			msg1.MediaRefs[0].Path, msg2.MediaRefs[0].Path)
	}
}

// TestPersistAssistantImages_SkipsPartial verifies that images with Partial=true
// are not persisted and do not produce MediaRefs.
func TestPersistAssistantImages_SkipsPartial(t *testing.T) {
	workspace := t.TempDir()
	imgData := base64.StdEncoding.EncodeToString(minimalPNG)

	msg := &providers.Message{
		Images: []providers.ImageContent{
			{MimeType: "image/png", Data: imgData, Partial: true},  // skip
			{MimeType: "image/png", Data: imgData, Partial: false}, // persist
		},
	}

	persistAssistantImages(msg, workspace)

	mediaDir := filepath.Join(workspace, "media")
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	// Only the final (non-partial) image is written.
	if len(entries) != 1 {
		t.Errorf("expected 1 file (partial skipped), got %d", len(entries))
	}
	if len(msg.MediaRefs) != 1 {
		t.Errorf("expected 1 MediaRef (partial skipped), got %d", len(msg.MediaRefs))
	}
	if msg.Images != nil {
		t.Errorf("expected Images cleared, got %v", msg.Images)
	}
}

// TestPersistAssistantImages_EmptyWorkspace verifies that an empty workspace
// path is handled gracefully (no panic, no files written).
func TestPersistAssistantImages_EmptyWorkspace(t *testing.T) {
	imgData := base64.StdEncoding.EncodeToString(minimalPNG)
	msg := &providers.Message{
		Images: []providers.ImageContent{{MimeType: "image/png", Data: imgData}},
	}

	// Must not panic.
	persistAssistantImages(msg, "")

	// Images should NOT be cleared (no workspace = nothing happened).
	if len(msg.Images) == 0 {
		t.Error("Images should remain when workspace is empty (early return)")
	}
	if len(msg.MediaRefs) != 0 {
		t.Errorf("expected 0 MediaRefs when workspace is empty, got %d", len(msg.MediaRefs))
	}
}

// TestPersistAssistantImages_AllPartials verifies that a message with only
// partial frames produces no disk files and leaves MediaRefs empty.
func TestPersistAssistantImages_AllPartials(t *testing.T) {
	workspace := t.TempDir()
	imgData := base64.StdEncoding.EncodeToString(minimalPNG)

	msg := &providers.Message{
		Images: []providers.ImageContent{
			{MimeType: "image/png", Data: imgData, Partial: true},
			{MimeType: "image/png", Data: imgData, Partial: true},
		},
	}

	persistAssistantImages(msg, workspace)

	mediaDir := filepath.Join(workspace, "media")
	if _, err := os.Stat(mediaDir); err == nil {
		entries, _ := os.ReadDir(mediaDir)
		if len(entries) != 0 {
			t.Errorf("expected 0 files (all partial), got %d", len(entries))
		}
	}
	if len(msg.MediaRefs) != 0 {
		t.Errorf("expected 0 MediaRefs (all partial), got %d", len(msg.MediaRefs))
	}
}

// TestPersistAssistantImages_MultipleDistinct verifies that two different images
// (different content → different hashes) produce two separate disk files.
func TestPersistAssistantImages_MultipleDistinct(t *testing.T) {
	workspace := t.TempDir()

	// Create two distinct payloads by appending different bytes.
	raw1 := append(minimalPNG[:len(minimalPNG):len(minimalPNG)], 0x01)
	raw2 := append(minimalPNG[:len(minimalPNG):len(minimalPNG)], 0x02)

	msg := &providers.Message{
		Images: []providers.ImageContent{
			{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(raw1), Partial: false},
			{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(raw2), Partial: false},
		},
	}

	persistAssistantImages(msg, workspace)

	mediaDir := filepath.Join(workspace, "media")
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 files (distinct images), got %d", len(entries))
	}
	if len(msg.MediaRefs) != 2 {
		t.Errorf("expected 2 MediaRefs, got %d", len(msg.MediaRefs))
	}
	if msg.MediaRefs[0].Path == msg.MediaRefs[1].Path {
		t.Errorf("expected distinct paths for distinct images")
	}
}

// TestPersistAssistantImages_PromptNotPropagated verifies that persistAssistantImages
// does NOT set MediaRef.Prompt (it only handles image bytes; prompt threading happens
// in the tools layer via result.MediaPrompts and in finalize_stage via MediaResult.Prompt).
// This test documents the current contract so any future signature change is caught.
func TestPersistAssistantImages_PromptNotPropagated(t *testing.T) {
	workspace := t.TempDir()
	imgData := base64.StdEncoding.EncodeToString(minimalPNG)

	msg := &providers.Message{
		Images: []providers.ImageContent{{
			MimeType: "image/png",
			Data:     imgData,
			Partial:  false,
		}},
	}

	persistAssistantImages(msg, workspace)

	if len(msg.MediaRefs) != 1 {
		t.Fatalf("expected 1 MediaRef, got %d", len(msg.MediaRefs))
	}
	// persistAssistantImages has no access to prompts; Prompt must be empty here.
	// The pipeline's finalize_stage sets Prompt on MediaRefs built from tool
	// MediaResults (create_image path), not from Codex assistant image refs.
	ref := msg.MediaRefs[0]
	if ref.Prompt != "" {
		t.Errorf("expected MediaRef.Prompt empty from persistAssistantImages, got %q", ref.Prompt)
	}
	if ref.Kind != "image" {
		t.Errorf("MediaRef.Kind = %q, want image", ref.Kind)
	}
}

// TestPersistAssistantImages_PathInsideMediaDir verifies the hash-derived filename
// is exactly {sha256hex}.{ext} and lives directly under workspace/media/.
func TestPersistAssistantImages_PathInsideMediaDir(t *testing.T) {
	workspace := t.TempDir()
	imgData := base64.StdEncoding.EncodeToString(minimalPNG)

	msg := &providers.Message{
		Images: []providers.ImageContent{{MimeType: "image/png", Data: imgData}},
	}
	persistAssistantImages(msg, workspace)

	ref := msg.MediaRefs[0]
	base := filepath.Base(ref.Path)
	dir := filepath.Dir(ref.Path)

	// Must be directly in workspace/media/ (no subdirectory).
	wantDir := filepath.Join(workspace, "media")
	if dir != wantDir {
		t.Errorf("parent dir = %q, want %q", dir, wantDir)
	}
	// Filename must be {64hex}.png
	if len(base) != 64+4 { // 64 hex + ".png"
		t.Errorf("filename %q: expected {64hex}.png, len=%d", base, len(base))
	}
	if !strings.HasSuffix(base, ".png") {
		t.Errorf("filename %q must end with .png", base)
	}
	hashPart := strings.TrimSuffix(base, ".png")
	for _, c := range hashPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("filename %q: non-hex character %q in hash part", base, fmt.Sprintf("%c", c))
			break
		}
	}
}

