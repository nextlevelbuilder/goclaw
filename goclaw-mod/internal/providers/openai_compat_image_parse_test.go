package providers

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// pngMagic is the 8-byte PNG file signature per the PNG spec.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// decodeB64 decodes a standard base64 string and fails the test on error.
func decodeB64(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	return raw
}

// hasPNGMagic checks that raw starts with the 8-byte PNG signature.
func hasPNGMagic(raw []byte) bool {
	if len(raw) < 8 {
		return false
	}
	return bytes.Equal(raw[:8], pngMagic)
}

// --- parseDataURL unit tests ---

func TestParseDataURL_ValidPNG(t *testing.T) {
	b64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"
	url := "data:image/png;base64," + b64

	mime, got, err := parseDataURL(url)
	if err != nil {
		t.Fatalf("parseDataURL returned unexpected error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want %q", mime, "image/png")
	}
	if got != b64 {
		t.Errorf("b64Data mismatch: got %q, want %q", got, b64)
	}
	// Verify the decoded bytes start with PNG magic.
	raw := decodeB64(t, got)
	if !hasPNGMagic(raw) {
		t.Errorf("decoded bytes do not have PNG magic: first 8 = %x", raw[:8])
	}
}

func TestParseDataURL_ValidJPEG(t *testing.T) {
	// Minimal JPEG SOI marker (FF D8)
	raw := []byte{0xFF, 0xD8, 0x00}
	b64 := base64.StdEncoding.EncodeToString(raw)
	url := "data:image/jpeg;base64," + b64

	mime, got, err := parseDataURL(url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q, want %q", mime, "image/jpeg")
	}
	if got != b64 {
		t.Errorf("b64Data mismatch")
	}
}

func TestParseDataURL_MalformedNoBase64Prefix(t *testing.T) {
	_, _, err := parseDataURL("data:image/png," + "notbase64encoded")
	if err == nil {
		t.Fatal("expected error for missing ;base64 marker, got nil")
	}
}

func TestParseDataURL_MalformedNotDataURL(t *testing.T) {
	_, _, err := parseDataURL("https://example.com/image.png")
	if err == nil {
		t.Fatal("expected error for non-data URL, got nil")
	}
}

func TestParseDataURL_MalformedInvalidBase64(t *testing.T) {
	_, _, err := parseDataURL("data:image/png;base64,!!!not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64 payload, got nil")
	}
}

func TestParseDataURL_Empty(t *testing.T) {
	_, _, err := parseDataURL("")
	if err == nil {
		t.Fatal("expected error for empty string, got nil")
	}
}

// --- Non-stream fixture test ---

func TestOpenAICompatParseResponse_NonStreamImages(t *testing.T) {
	fixture, err := os.ReadFile("testdata/openai_compat_image_nonstream.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(fixture, &oaiResp); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-image-1")
	result := p.parseResponse(&oaiResp)

	if len(result.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(result.Images))
	}
	img := result.Images[0]
	if img.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want %q", img.MimeType, "image/png")
	}
	raw := decodeB64(t, img.Data)
	if !hasPNGMagic(raw) {
		t.Errorf("decoded image data does not start with PNG magic: %x", raw[:min8(raw)])
	}
	// Content should be preserved alongside images.
	if result.Content != "Here is your image." {
		t.Errorf("Content = %q, want %q", result.Content, "Here is your image.")
	}
}

// min8 returns the smaller of 8 and len(b) — used for safe slice in error messages.
func min8(b []byte) int {
	if len(b) < 8 {
		return len(b)
	}
	return 8
}

// --- Stream fixture test ---

// newStreamServer returns a test HTTP server that serves the SSE fixture file.
func newStreamServer(t *testing.T, fixturePath string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read SSE fixture: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write line-by-line to simulate chunked SSE.
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			_, _ = io.WriteString(w, scanner.Text()+"\n")
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
}

func TestOpenAICompatChatStream_Images(t *testing.T) {
	srv := newStreamServer(t, "testdata/openai_compat_image_stream.sse")
	defer srv.Close()

	p := NewOpenAIProvider("test", "key", srv.URL, "gpt-image-1")

	result, err := p.ChatStream(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "generate an image"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}

	// Content accumulated across chunks.
	if !strings.Contains(result.Content, "Here") {
		t.Errorf("Content = %q, want substring %q", result.Content, "Here")
	}

	if len(result.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(result.Images))
	}
	img := result.Images[0]
	if img.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want %q", img.MimeType, "image/png")
	}
	raw := decodeB64(t, img.Data)
	if !hasPNGMagic(raw) {
		t.Errorf("streamed image data does not start with PNG magic: %x", raw[:min8(raw)])
	}
}

// --- Mixed payload test (content + tool_calls + images all preserved) ---

func TestOpenAICompatParseResponse_Mixed(t *testing.T) {
	fixture, err := os.ReadFile("testdata/openai_compat_image_mixed.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(fixture, &oaiResp); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-image-1")
	result := p.parseResponse(&oaiResp)

	// Content preserved.
	if result.Content != "I'll generate that for you." {
		t.Errorf("Content = %q, want %q", result.Content, "I'll generate that for you.")
	}
	// Tool calls preserved.
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "log_generation" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", result.ToolCalls[0].Name, "log_generation")
	}
	// Images preserved.
	if len(result.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(result.Images))
	}
	if result.Images[0].MimeType != "image/png" {
		t.Errorf("Images[0].MimeType = %q, want %q", result.Images[0].MimeType, "image/png")
	}
}

// --- Regression: non-image response unaffected ---

func TestOpenAICompatParseResponse_NoImages_Unchanged(t *testing.T) {
	raw := `{
		"choices": [{
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7}
	}`

	var oaiResp openAIResponse
	if err := json.Unmarshal([]byte(raw), &oaiResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4o")
	result := p.parseResponse(&oaiResp)

	if result.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello!")
	}
	if len(result.Images) != 0 {
		t.Errorf("Images len = %d, want 0 for non-image response", len(result.Images))
	}
	if result.Usage == nil || result.Usage.TotalTokens != 7 {
		t.Errorf("Usage not populated correctly: %+v", result.Usage)
	}
}

// --- Malformed data URL in stream: skipped, stream continues ---

func TestOpenAICompatChatStream_MalformedImageSkipped(t *testing.T) {
	sseData := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"hi"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,!!!INVALID!!!"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, sseData)
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test", "key", srv.URL, "gpt-image-1")
	result, err := p.ChatStream(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, nil)
	// Stream must not error — malformed image is skipped, not fatal.
	if err != nil {
		t.Fatalf("ChatStream returned error for malformed image: %v", err)
	}
	if result.Content != "hi" {
		t.Errorf("Content = %q, want %q", result.Content, "hi")
	}
	// Malformed image dropped — no images in result.
	if len(result.Images) != 0 {
		t.Errorf("Images len = %d, want 0 (malformed entry skipped)", len(result.Images))
	}
}
