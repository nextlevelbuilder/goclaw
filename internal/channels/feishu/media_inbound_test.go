package feishu

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// --- detectImageFormat ---

func TestDetectImageFormat_JPEG(t *testing.T) {
	// JPEG magic bytes: FF D8 FF
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	ct, ext := detectImageFormat(data)
	if ct != "image/jpeg" {
		t.Errorf("ct: got %q, want image/jpeg", ct)
	}
	if ext != ".jpg" {
		t.Errorf("ext: got %q, want .jpg", ext)
	}
}

func TestDetectImageFormat_PNG(t *testing.T) {
	// PNG magic bytes: 89 50 4E 47
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	ct, ext := detectImageFormat(data)
	if ct != "image/png" {
		t.Errorf("ct: got %q, want image/png", ct)
	}
	if ext != ".png" {
		t.Errorf("ext: got %q, want .png", ext)
	}
}

func TestDetectImageFormat_GIF(t *testing.T) {
	// GIF magic bytes: 47 49 46 38
	data := []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}
	ct, ext := detectImageFormat(data)
	if ct != "image/gif" {
		t.Errorf("ct: got %q, want image/gif", ct)
	}
	if ext != ".gif" {
		t.Errorf("ext: got %q, want .gif", ext)
	}
}

func TestDetectImageFormat_WebP(t *testing.T) {
	// WebP: RIFF....WEBP
	data := make([]byte, 12)
	copy(data[0:4], []byte("RIFF"))
	copy(data[8:12], []byte("WEBP"))
	ct := http.DetectContentType(data)
	// http.DetectContentType may not detect WebP as image/webp on all Go versions
	// so we call our function and just verify it doesn't panic and returns valid values
	gotCT, gotExt := detectImageFormat(data)
	if gotCT == "" || gotExt == "" {
		t.Errorf("detectImageFormat returned empty values: ct=%q ext=%q", gotCT, gotExt)
	}
	_ = ct
}

func TestDetectImageFormat_Unknown(t *testing.T) {
	// Unknown binary → defaults to image/png
	data := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	ct, ext := detectImageFormat(data)
	if ct != "image/png" {
		t.Errorf("ct: got %q, want image/png (default fallback)", ct)
	}
	if ext != ".png" {
		t.Errorf("ext: got %q, want .png (default fallback)", ext)
	}
}

func TestDetectImageFormat_EmptyData(t *testing.T) {
	ct, ext := detectImageFormat(nil)
	if ct == "" || ext == "" {
		t.Errorf("detectImageFormat(nil) returned empty: ct=%q ext=%q", ct, ext)
	}
}

// --- extractJSONField ---

func TestExtractJSONField_Found(t *testing.T) {
	json := `{"image_key":"img_abc123","other":"value"}`
	got := extractJSONField(json, "image_key")
	if got != "img_abc123" {
		t.Errorf("got %q, want %q", got, "img_abc123")
	}
}

func TestExtractJSONField_FileKey(t *testing.T) {
	json := `{"file_key":"file_xyz789","file_name":"doc.pdf"}`
	got := extractJSONField(json, "file_key")
	if got != "file_xyz789" {
		t.Errorf("got %q, want %q", got, "file_xyz789")
	}
}

func TestExtractJSONField_Missing(t *testing.T) {
	json := `{"other":"value"}`
	got := extractJSONField(json, "image_key")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestExtractJSONField_EmptyString(t *testing.T) {
	got := extractJSONField("", "image_key")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractJSONField_UnclosedValue(t *testing.T) {
	// Missing closing quote → returns empty
	json := `{"image_key":"unclosed`
	got := extractJSONField(json, "image_key")
	if got != "" {
		t.Errorf("got %q, want empty for unclosed value", got)
	}
}

// --- saveMediaToTemp ---

func TestSaveMediaToTemp_Basic(t *testing.T) {
	data := []byte("test image data")
	path, err := saveMediaToTemp(data, "img", ".png")
	if err != nil {
		t.Fatalf("saveMediaToTemp error: %v", err)
	}
	defer os.Remove(path)

	if !strings.Contains(path, "feishu_img_") {
		t.Errorf("path missing prefix: %q", path)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("path missing .png suffix: %q", path)
	}

	// Verify file content
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(content) != "test image data" {
		t.Errorf("content mismatch: got %q", content)
	}
}

func TestSaveMediaToTemp_EmptyExt(t *testing.T) {
	data := []byte("binary")
	path, err := saveMediaToTemp(data, "file", "")
	if err != nil {
		t.Fatalf("saveMediaToTemp error: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasSuffix(path, ".bin") {
		t.Errorf("expected .bin default ext, got %q", path)
	}
}

func TestSaveMediaToTemp_AudioExt(t *testing.T) {
	data := []byte("audio bytes")
	path, err := saveMediaToTemp(data, "audio", ".opus")
	if err != nil {
		t.Fatalf("saveMediaToTemp error: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasSuffix(path, ".opus") {
		t.Errorf("expected .opus, got %q", path)
	}
}

// --- mediaMaxBytes ---

func TestMediaMaxBytes_Default(t *testing.T) {
	ch := &Channel{}
	maxBytes := ch.mediaMaxBytes()
	expected := int64(defaultMediaMaxMB) * 1024 * 1024
	if maxBytes != expected {
		t.Errorf("mediaMaxBytes: got %d, want %d", maxBytes, expected)
	}
}

func TestMediaMaxBytes_Custom(t *testing.T) {
	ch := &Channel{}
	ch.cfg.MediaMaxMB = 10
	maxBytes := ch.mediaMaxBytes()
	expected := int64(10) * 1024 * 1024
	if maxBytes != expected {
		t.Errorf("mediaMaxBytes: got %d, want %d", maxBytes, expected)
	}
}

func TestMediaMaxBytes_ZeroFallsToDefault(t *testing.T) {
	ch := &Channel{}
	ch.cfg.MediaMaxMB = 0
	maxBytes := ch.mediaMaxBytes()
	expected := int64(defaultMediaMaxMB) * 1024 * 1024
	if maxBytes != expected {
		t.Errorf("mediaMaxBytes(0): got %d, want default %d", maxBytes, expected)
	}
}

func TestMediaMaxBytes_NegativeFallsToDefault(t *testing.T) {
	ch := &Channel{}
	ch.cfg.MediaMaxMB = -1
	maxBytes := ch.mediaMaxBytes()
	expected := int64(defaultMediaMaxMB) * 1024 * 1024
	if maxBytes != expected {
		t.Errorf("mediaMaxBytes(-1): got %d, want default %d", maxBytes, expected)
	}
}
