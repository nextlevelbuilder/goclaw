package providers

import (
	"strings"
	"testing"
)

// Reproduces the image-generation failure: a single SSE data line carrying a
// multi-MB base64 image used to overflow bufio.Scanner ("token too long").
func TestSSEScannerHandlesMultiMBLine(t *testing.T) {
	big := strings.Repeat("A", 3*1024*1024) // 3MB, well over the old 1MB cap
	stream := "event: response.image_generation_call.partial_image\n" +
		"data: " + big + "\n" +
		"data: [DONE]\n"
	sc := NewSSEScanner(strings.NewReader(stream))
	if !sc.Next() {
		t.Fatalf("expected first data line; err=%v", sc.Err())
	}
	if len(sc.Data()) != len(big) {
		t.Fatalf("data len=%d want %d", len(sc.Data()), len(big))
	}
	if sc.EventType() != "response.image_generation_call.partial_image" {
		t.Fatalf("eventType=%q", sc.EventType())
	}
	if sc.Next() {
		t.Fatalf("expected [DONE] to end stream, got %q", sc.Data())
	}
	if sc.Err() != nil {
		t.Fatalf("unexpected err: %v", sc.Err())
	}
}

// Final data line arriving with EOF and no trailing newline must be delivered.
func TestSSEScannerFinalLineNoNewline(t *testing.T) {
	sc := NewSSEScanner(strings.NewReader("data: hello"))
	if !sc.Next() || sc.Data() != "hello" {
		t.Fatalf("data=%q err=%v", sc.Data(), sc.Err())
	}
	if sc.Next() {
		t.Fatalf("expected end")
	}
	if sc.Err() != nil {
		t.Fatalf("err=%v", sc.Err())
	}
}
