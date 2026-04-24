package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// pngHeader is the 8-byte PNG signature for validity checks.
var pngHeader = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// decodeBase64PNG decodes a base64 string and verifies it has a valid PNG header.
func decodeBase64PNG(t *testing.T, b64 string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}
	if len(raw) < 8 {
		t.Fatalf("decoded bytes too short for PNG header: %d bytes", len(raw))
	}
	for i, b := range pngHeader {
		if raw[i] != b {
			t.Fatalf("PNG header mismatch at byte %d: got 0x%02x want 0x%02x", i, raw[i], b)
		}
	}
	return raw
}

// loadFixtureEvents reads a JSON fixture file and unmarshals it as a slice of codexSSEEvent.
func loadFixtureEvents(t *testing.T, filename string) []codexSSEEvent {
	t.Helper()
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatalf("read fixture %q: %v", filename, err)
	}
	var events []codexSSEEvent
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatalf("unmarshal fixture %q: %v", filename, err)
	}
	return events
}

// serveFixtureAsSSE creates an httptest.Server that streams the given events
// as SSE data frames, followed by [DONE].
func serveFixtureAsSSE(t *testing.T, events []codexSSEEvent) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		for _, ev := range events {
			b, err := json.Marshal(ev)
			if err != nil {
				t.Errorf("marshal event: %v", err)
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

// TestCodexImagePartialThenDone verifies:
//  1. A partial_image event stores the partial in imageState and emits a Partial=true chunk.
//  2. The subsequent output_item.done (image_generation_call) records the final image.
//  3. ChatResponse.Images contains exactly one image with correct MIME type.
//  4. The base64 in ChatResponse.Images[0].Data decodes to a valid PNG.
func TestCodexImagePartialThenDone(t *testing.T) {
	events := loadFixtureEvents(t, "codex_native_image_partial_then_done.json")
	server := serveFixtureAsSSE(t, events)
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-image-2")
	p.retryConfig.Attempts = 1

	var partialChunks []ImageContent
	var finalChunks []ImageContent

	result, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Draw a cat"}},
	}, func(chunk StreamChunk) {
		for _, img := range chunk.Images {
			if img.Partial {
				partialChunks = append(partialChunks, img)
			} else {
				finalChunks = append(finalChunks, img)
			}
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	// Streaming: one partial chunk emitted.
	if len(partialChunks) != 1 {
		t.Errorf("partial chunks = %d, want 1", len(partialChunks))
	} else {
		if partialChunks[0].MimeType != "image/png" {
			t.Errorf("partial chunk MimeType = %q, want image/png", partialChunks[0].MimeType)
		}
		decodeBase64PNG(t, partialChunks[0].Data)
	}

	// Streaming: one final (non-partial) chunk emitted.
	if len(finalChunks) != 1 {
		t.Errorf("final chunks = %d, want 1", len(finalChunks))
	} else {
		if finalChunks[0].MimeType != "image/png" {
			t.Errorf("final chunk MimeType = %q, want image/png", finalChunks[0].MimeType)
		}
		decodeBase64PNG(t, finalChunks[0].Data)
	}

	// ChatResponse.Images: exactly one entry (deduplicated; not double-counted from response.completed).
	if len(result.Images) != 1 {
		t.Fatalf("result.Images length = %d, want 1", len(result.Images))
	}
	if result.Images[0].MimeType != "image/png" {
		t.Errorf("result.Images[0].MimeType = %q, want image/png", result.Images[0].MimeType)
	}
	decodeBase64PNG(t, result.Images[0].Data)
}

// TestCodexImageNonStream verifies that a single response.completed event
// containing image_generation_call items in output[] populates ChatResponse.Images.
func TestCodexImageNonStream(t *testing.T) {
	events := loadFixtureEvents(t, "codex_native_image_non_stream.json")
	server := serveFixtureAsSSE(t, events)
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-image-2")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Draw a landscape"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Text content from the message item is also captured.
	if result.Content != "Here is your image." {
		t.Errorf("Content = %q, want 'Here is your image.'", result.Content)
	}

	if len(result.Images) != 1 {
		t.Fatalf("result.Images length = %d, want 1", len(result.Images))
	}
	if result.Images[0].MimeType != "image/png" {
		t.Errorf("result.Images[0].MimeType = %q, want image/png", result.Images[0].MimeType)
	}
	decodeBase64PNG(t, result.Images[0].Data)

	// Usage captured from non-stream response.completed.
	if result.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if result.Usage.TotalTokens != 11 {
		t.Errorf("TotalTokens = %d, want 11", result.Usage.TotalTokens)
	}
}

// TestCodexImageDuplicatePartialDedup verifies that two identical partial_image
// events for the same item_id do not emit a second chunk (SHA256 dedup).
func TestCodexImageDuplicatePartialDedup(t *testing.T) {
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// Two identical partial frames.
		for range 2 {
			ev := codexSSEEvent{
				Type:              "response.image_generation_call.partial_image",
				ItemID:            "ig_dedup",
				OutputFormat:      "png",
				PartialImageB64:   b64,
				PartialImageIndex: 0,
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		// Final done.
		done := codexSSEEvent{
			Type: "response.output_item.done",
			Item: &codexItem{
				ID:           "ig_dedup",
				Type:         "image_generation_call",
				OutputFormat: "png",
				Result:       b64,
			},
		}
		db, _ := json.Marshal(done)
		fmt.Fprintf(w, "data: %s\n\n", db)
		flusher.Flush()

		completed := codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID:     "resp_dedup",
				Status: "completed",
				Usage:  &codexUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
			},
		}
		cb, _ := json.Marshal(completed)
		fmt.Fprintf(w, "data: %s\n\n", cb)
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-image-2")
	p.retryConfig.Attempts = 1

	var imageChunks []ImageContent
	result, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Draw"}},
	}, func(chunk StreamChunk) {
		imageChunks = append(imageChunks, chunk.Images...)
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	// Only 1 partial chunk (dedup skips the second identical frame) + 1 final chunk.
	partialCount := 0
	finalCount := 0
	for _, img := range imageChunks {
		if img.Partial {
			partialCount++
		} else {
			finalCount++
		}
	}
	if partialCount != 1 {
		t.Errorf("partial chunks emitted = %d, want 1 (duplicate suppressed)", partialCount)
	}
	if finalCount != 1 {
		t.Errorf("final chunks emitted = %d, want 1", finalCount)
	}

	// ChatResponse.Images: exactly one image.
	if len(result.Images) != 1 {
		t.Errorf("result.Images length = %d, want 1", len(result.Images))
	}
}

// TestCodexImageMixedTextAndImage verifies that text content, a function tool_call,
// and an image_generation_call in the same response are all preserved correctly.
func TestCodexImageMixedTextAndImage(t *testing.T) {
	const imgB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []codexSSEEvent{
			// Text delta.
			{Type: "response.output_text.delta", ItemID: "msg_1", Delta: "Here is "},
			{Type: "response.output_text.delta", ItemID: "msg_1", Delta: "the result."},
			// Message item done.
			{
				Type: "response.output_item.done",
				Item: &codexItem{ID: "msg_1", Type: "message", Role: "assistant"},
			},
			// Function call done.
			{
				Type: "response.output_item.done",
				Item: &codexItem{
					ID:        "fc_1",
					Type:      "function_call",
					CallID:    "call_abc",
					Name:      "web_search",
					Arguments: `{"query":"cats"}`,
				},
			},
			// Image generation call done.
			{
				Type: "response.output_item.done",
				Item: &codexItem{
					ID:           "ig_1",
					Type:         "image_generation_call",
					OutputFormat: "png",
					Result:       imgB64,
				},
			},
			// Completion.
			{
				Type: "response.completed",
				Response: &codexAPIResponse{
					ID:     "resp_mixed",
					Status: "completed",
					Usage:  &codexUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
				},
			},
		}

		for _, ev := range events {
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Search and draw"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Text preserved.
	if result.Content != "Here is the result." {
		t.Errorf("Content = %q, want 'Here is the result.'", result.Content)
	}

	// Function tool call preserved.
	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", result.FinishReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls length = %d, want 1", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "web_search" {
		t.Errorf("ToolCalls[0].Name = %q, want web_search", result.ToolCalls[0].Name)
	}

	// Image preserved.
	if len(result.Images) != 1 {
		t.Fatalf("result.Images length = %d, want 1", len(result.Images))
	}
	if result.Images[0].MimeType != "image/png" {
		t.Errorf("result.Images[0].MimeType = %q, want image/png", result.Images[0].MimeType)
	}
	decodeBase64PNG(t, result.Images[0].Data)
}

// TestCodexMimeFromFormat verifies the mimeFromFormat helper covers all documented formats.
func TestCodexMimeFromFormat(t *testing.T) {
	cases := []struct {
		format string
		want   string
	}{
		{"png", "image/png"},
		{"jpg", "image/jpeg"},
		{"jpeg", "image/jpeg"},
		{"webp", "image/webp"},
		{"", "image/png"},
		{"unknown", "image/png"},
	}
	for _, tc := range cases {
		got := mimeFromFormat(tc.format)
		if got != tc.want {
			t.Errorf("mimeFromFormat(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}
