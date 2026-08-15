package providers

import (
	"strings"
	"testing"
)

func TestSSEReader_DataPrefix(t *testing.T) {
	input := "data: {\"key\":\"value\"}\n"
	sc := NewSSEScanner(strings.NewReader(input))
	if !sc.Next() {
		t.Fatal("expected Next to return true")
	}
	if sc.Data() != `{"key":"value"}` {
		t.Errorf("data = %q", sc.Data())
	}
}

func TestSSEReader_DataPrefixNoSpace(t *testing.T) {
	input := "data:{\"key\":\"value\"}\n"
	sc := NewSSEScanner(strings.NewReader(input))
	if !sc.Next() {
		t.Fatal("expected Next to return true")
	}
	if sc.Data() != `{"key":"value"}` {
		t.Errorf("data = %q", sc.Data())
	}
}

func TestSSEReader_Done(t *testing.T) {
	input := "data: {\"chunk\":1}\ndata: [DONE]\n"
	sc := NewSSEScanner(strings.NewReader(input))
	if !sc.Next() {
		t.Fatal("expected first Next to return true")
	}
	if sc.Data() != `{"chunk":1}` {
		t.Errorf("first data = %q", sc.Data())
	}
	if sc.Next() {
		t.Error("expected Next to return false after [DONE]")
	}
	if sc.Err() != nil {
		t.Errorf("unexpected error: %v", sc.Err())
	}
}

func TestSSEReader_SkipNonData(t *testing.T) {
	input := ": comment\nevent: message_start\nid: 123\ndata: payload\n"
	sc := NewSSEScanner(strings.NewReader(input))
	if !sc.Next() {
		t.Fatal("expected Next to return true")
	}
	if sc.Data() != "payload" {
		t.Errorf("data = %q, want \"payload\"", sc.Data())
	}
	if sc.EventType() != "message_start" {
		t.Errorf("eventType = %q, want \"message_start\"", sc.EventType())
	}
}

func TestSSEReader_EmptyLine(t *testing.T) {
	input := "\n\ndata: hello\n\n"
	sc := NewSSEScanner(strings.NewReader(input))
	if !sc.Next() {
		t.Fatal("expected Next to return true")
	}
	if sc.Data() != "hello" {
		t.Errorf("data = %q", sc.Data())
	}
}

func TestSSEReader_MultipleChunks(t *testing.T) {
	input := "data: chunk1\ndata: chunk2\ndata: chunk3\n"
	sc := NewSSEScanner(strings.NewReader(input))
	var chunks []string
	for sc.Next() {
		chunks = append(chunks, sc.Data())
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	for i, want := range []string{"chunk1", "chunk2", "chunk3"} {
		if chunks[i] != want {
			t.Errorf("chunk[%d] = %q, want %q", i, chunks[i], want)
		}
	}
}

func TestSSEReader_LargePayload(t *testing.T) {
	// Create a payload larger than 64KB to test buffer sizing.
	large := strings.Repeat("x", 100*1024) // 100KB
	input := "data: " + large + "\n"
	sc := NewSSEScanner(strings.NewReader(input))
	if !sc.Next() {
		t.Fatal("expected Next to return true for large payload")
	}
	if len(sc.Data()) != 100*1024 {
		t.Errorf("data len = %d, want %d", len(sc.Data()), 100*1024)
	}
	if sc.Err() != nil {
		t.Errorf("unexpected error: %v", sc.Err())
	}
}

func TestSSEReader_EventTypeTracking(t *testing.T) {
	input := "event: content_block_start\ndata: start\nevent: content_block_delta\ndata: delta\n"
	sc := NewSSEScanner(strings.NewReader(input))

	if !sc.Next() {
		t.Fatal("expected first Next")
	}
	if sc.EventType() != "content_block_start" {
		t.Errorf("eventType = %q, want content_block_start", sc.EventType())
	}

	if !sc.Next() {
		t.Fatal("expected second Next")
	}
	if sc.EventType() != "content_block_delta" {
		t.Errorf("eventType = %q, want content_block_delta", sc.EventType())
	}
}

func TestSSEReader_EmptyData(t *testing.T) {
	input := "data: \n"
	sc := NewSSEScanner(strings.NewReader(input))
	if !sc.Next() {
		t.Fatal("expected Next to return true for empty data")
	}
	if sc.Data() != "" {
		t.Errorf("data = %q, want empty string", sc.Data())
	}
}

func TestSSEReader_ScannerError(t *testing.T) {
	input := "data: valid\n"
	r := strings.NewReader(input)
	sc := NewSSEScanner(r)
	if !sc.Next() {
		t.Fatal("expected first Next to succeed")
	}
	if sc.Data() != "valid" {
		t.Errorf("data = %q, want \"valid\"", sc.Data())
	}
	// After valid data, next call should return false (EOF)
	if sc.Next() {
		t.Error("expected Next to return false at EOF")
	}
	if sc.Err() != nil {
		t.Errorf("expected no error at EOF, got: %v", sc.Err())
	}
}

func TestSSEReader_EventTypePersistence(t *testing.T) {
	input := "event: message_start\ndata: line1\ndata: line2\n\nevent: new_event\ndata: line3\n"
	sc := NewSSEScanner(strings.NewReader(input))

	// First data block: should have event type "message_start"
	if !sc.Next() {
		t.Fatal("expected first Next")
	}
	if sc.EventType() != "message_start" {
		t.Errorf("first eventType = %q, want message_start", sc.EventType())
	}
	if sc.Data() != "line1" {
		t.Errorf("first data = %q, want line1", sc.Data())
	}

	// Second data line: event type should persist
	if !sc.Next() {
		t.Fatal("expected second Next")
	}
	if sc.EventType() != "message_start" {
		t.Errorf("second eventType = %q, want message_start", sc.EventType())
	}
	if sc.Data() != "line2" {
		t.Errorf("second data = %q, want line2", sc.Data())
	}

	// After new event line: event type should change
	if !sc.Next() {
		t.Fatal("expected third Next")
	}
	if sc.EventType() != "new_event" {
		t.Errorf("third eventType = %q, want new_event", sc.EventType())
	}
	if sc.Data() != "line3" {
		t.Errorf("third data = %q, want line3", sc.Data())
	}
}

func TestSSEReader_NoDataAfterDone(t *testing.T) {
	input := "data: valid\ndata: [DONE]\ndata: ignored\n"
	sc := NewSSEScanner(strings.NewReader(input))

	if !sc.Next() {
		t.Fatal("expected first Next")
	}
	if sc.Data() != "valid" {
		t.Errorf("first data = %q, want valid", sc.Data())
	}

	// Next should hit [DONE] and return false
	if sc.Next() {
		t.Error("expected Next to return false after [DONE]")
	}
	if sc.Err() != nil {
		t.Errorf("expected no error after [DONE], got: %v", sc.Err())
	}
}
