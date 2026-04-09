package googlechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// requestRecord captures an HTTP request for test assertions.
type requestRecord struct {
	Method string
	URL    string
	Body   map[string]any
}

// streamTestEnv holds test infrastructure for stream tests.
type streamTestEnv struct {
	ch  *Channel
	srv *httptest.Server
	mu  sync.Mutex
	recs []requestRecord
}

// getRecords returns a snapshot of recorded requests (thread-safe).
func (e *streamTestEnv) getRecords() []requestRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]requestRecord, len(e.recs))
	copy(out, e.recs)
	return out
}

// testStreamEnv creates a Channel with mock auth and HTTP server for stream tests.
func testStreamEnv(t *testing.T) *streamTestEnv {
	t.Helper()
	env := &streamTestEnv{}

	env.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		env.mu.Lock()
		env.recs = append(env.recs, requestRecord{
			Method: r.Method,
			URL:    r.URL.String(),
			Body:   body,
		})
		env.mu.Unlock()

		if r.Method == "POST" {
			json.NewEncoder(w).Encode(map[string]any{
				"name":   "spaces/test/messages/new-1",
				"thread": map[string]string{"name": "spaces/test/threads/t1"},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(env.srv.Close)

	env.ch = &Channel{
		auth: &ServiceAccountAuth{
			token:     "test-token",
			expiresAt: time.Now().Add(1 * time.Hour),
		},
		apiBase:           env.srv.URL,
		httpClient:        env.srv.Client(),
		dmStream:          true,
		groupStream:       false,
		longFormThreshold: longFormThresholdDefault,
	}

	return env
}

func TestChatStream_Dedup(t *testing.T) {
	env := testStreamEnv(t)
	cs := newChatStream(env.ch, "spaces/test/messages/m1")

	ctx := context.Background()

	// First update should send
	cs.update(ctx, "Hello")
	// Same text should be deduped
	cs.update(ctx, "Hello")
	// Wait for any flush timers
	time.Sleep(50 * time.Millisecond)

	recs := env.getRecords()
	patchCount := 0
	for _, r := range recs {
		if r.Method == "PATCH" {
			patchCount++
		}
	}
	if patchCount != 1 {
		t.Errorf("expected 1 PATCH (dedup), got %d", patchCount)
	}
}

func TestChatStream_Throttle(t *testing.T) {
	env := testStreamEnv(t)
	cs := newChatStream(env.ch, "spaces/test/messages/m1")
	ctx := context.Background()

	// First update sends immediately
	cs.update(ctx, "chunk1")
	// Second update within throttle window should be buffered
	cs.update(ctx, "chunk2")
	// Third update within throttle window should replace pending
	cs.update(ctx, "chunk3")

	// Only 1 PATCH should have been sent (chunk1)
	time.Sleep(50 * time.Millisecond) // let goroutines settle
	recs := env.getRecords()
	immediatePatch := 0
	for _, r := range recs {
		if r.Method == "PATCH" {
			immediatePatch++
		}
	}
	if immediatePatch != 1 {
		t.Errorf("expected 1 immediate PATCH, got %d", immediatePatch)
	}

	// Wait for flush timer to fire
	time.Sleep(defaultStreamThrottle + 200*time.Millisecond)

	recs = env.getRecords()
	totalPatch := 0
	for _, r := range recs {
		if r.Method == "PATCH" {
			totalPatch++
		}
	}
	// Should now have 2 PATCHes: immediate (chunk1) + timer flush (chunk3)
	if totalPatch != 2 {
		t.Errorf("expected 2 total PATCHes after timer, got %d", totalPatch)
	}
}

func TestChatStream_PendingFlush(t *testing.T) {
	env := testStreamEnv(t)
	cs := newChatStream(env.ch, "spaces/test/messages/m1")
	ctx := context.Background()

	// Send first to start throttle window
	cs.update(ctx, "first")
	// Buffer pending
	cs.update(ctx, "pending-text")
	// Stop should flush pending
	cs.stop(ctx)

	recs := env.getRecords()
	patchCount := 0
	var lastBody map[string]any
	for _, r := range recs {
		if r.Method == "PATCH" {
			patchCount++
			lastBody = r.Body
		}
	}
	if patchCount != 2 {
		t.Errorf("expected 2 PATCHes (initial + flush), got %d", patchCount)
	}
	if lastBody != nil {
		if text, ok := lastBody["text"].(string); ok {
			if !strings.Contains(text, "pending") {
				t.Errorf("final flush should contain pending text, got %q", text)
			}
		}
	}
}

func TestStreamEnabled_Config(t *testing.T) {
	ch := &Channel{dmStream: true, groupStream: false}
	if !ch.StreamEnabled(false) {
		t.Error("DM streaming should be enabled")
	}
	if ch.StreamEnabled(true) {
		t.Error("group streaming should be disabled")
	}

	ch2 := &Channel{dmStream: false, groupStream: true}
	if ch2.StreamEnabled(false) {
		t.Error("DM streaming should be disabled")
	}
	if !ch2.StreamEnabled(true) {
		t.Error("group streaming should be enabled")
	}
}

func TestCreateStream_ReusePlaceholder(t *testing.T) {
	env := testStreamEnv(t)
	ctx := context.Background()

	// Pre-store a placeholder
	env.ch.placeholders.Store("spaces/test", "spaces/test/messages/placeholder-1")

	stream, err := env.ch.CreateStream(ctx, "spaces/test", true)
	if err != nil {
		t.Fatal(err)
	}

	// Should NOT have created a new message (no POST)
	recs := env.getRecords()
	for _, r := range recs {
		if r.Method == "POST" {
			t.Error("should reuse placeholder, not POST new message")
		}
	}

	// Stream should have the placeholder message name
	cs := stream.(*chatStream)
	if cs.messageName != "spaces/test/messages/placeholder-1" {
		t.Errorf("stream should use placeholder message, got %q", cs.messageName)
	}

	// Placeholder should be consumed
	if _, ok := env.ch.placeholders.Load("spaces/test"); ok {
		t.Error("placeholder should be deleted after reuse")
	}
}

func TestCreateStream_CreateNew(t *testing.T) {
	env := testStreamEnv(t)
	ctx := context.Background()

	// No placeholder stored
	stream, err := env.ch.CreateStream(ctx, "spaces/test", true)
	if err != nil {
		t.Fatal(err)
	}

	// Should have created a new message (POST)
	recs := env.getRecords()
	postCount := 0
	for _, r := range recs {
		if r.Method == "POST" {
			postCount++
			// Verify "⏳" text
			if text, ok := r.Body["text"].(string); ok {
				if text != "⏳" {
					t.Errorf("new stream message should have ⏳ text, got %q", text)
				}
			}
		}
	}
	if postCount != 1 {
		t.Errorf("expected 1 POST, got %d", postCount)
	}

	// Stream should have server-returned name
	cs := stream.(*chatStream)
	if cs.messageName != "spaces/test/messages/new-1" {
		t.Errorf("stream should use server-returned name, got %q", cs.messageName)
	}
}

func TestFinalizeStream_HandoffToPlaceholders(t *testing.T) {
	env := testStreamEnv(t)
	ctx := context.Background()

	// Simulate active stream
	cs := newChatStream(env.ch, "spaces/test/messages/stream-1")

	// Stop and finalize
	cs.Stop(ctx)
	env.ch.FinalizeStream(ctx, "spaces/test", cs)

	// Message should be handed off to placeholders
	pName, ok := env.ch.placeholders.Load("spaces/test")
	if !ok {
		t.Fatal("placeholder should be stored for Send() pickup")
	}
	if pName.(string) != "spaces/test/messages/stream-1" {
		t.Errorf("placeholder should have stream message name, got %q", pName)
	}
}

func TestToolIteration_Reuse(t *testing.T) {
	env := testStreamEnv(t)
	ctx := context.Background()

	// Simulate: CreateStream (reuses placeholder) → chunks → Stop + FinalizeStream
	env.ch.placeholders.Store("spaces/test", "spaces/test/messages/original")
	stream, err := env.ch.CreateStream(ctx, "spaces/test", true)
	if err != nil {
		t.Fatal(err)
	}

	// Stream some text
	stream.Update(ctx, "partial response")

	// Tool call: stop and finalize (hands off to placeholders)
	stream.Stop(ctx)
	env.ch.FinalizeStream(ctx, "spaces/test", stream)

	// Message should be in placeholders
	pName, ok := env.ch.placeholders.Load("spaces/test")
	if !ok {
		t.Fatal("placeholder should exist after tool-phase FinalizeStream")
	}

	// Next iteration: CreateStream should reuse from placeholders
	stream2, err := env.ch.CreateStream(ctx, "spaces/test", false)
	if err != nil {
		t.Fatal(err)
	}

	cs2 := stream2.(*chatStream)
	if cs2.messageName != pName.(string) {
		t.Errorf("restarted stream should reuse message %q, got %q", pName, cs2.messageName)
	}
}

func TestSend_InPlaceEdit(t *testing.T) {
	env := testStreamEnv(t)
	ctx := context.Background()

	// Simulate stream handoff: placeholder exists
	env.ch.placeholders.Store("spaces/test", "spaces/test/messages/stream-1")

	msg := bus.OutboundMessage{
		ChatID:   "spaces/test",
		Content:  "Short final response.",
		Metadata: map[string]string{"peer_kind": "direct"},
	}

	err := env.ch.Send(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}

	recs := env.getRecords()
	patchCount := 0
	postCount := 0
	for _, r := range recs {
		switch r.Method {
		case "PATCH":
			patchCount++
		case "POST":
			postCount++
		}
	}

	if patchCount != 1 {
		t.Errorf("expected 1 PATCH (in-place edit), got %d", patchCount)
	}
	if postCount != 0 {
		t.Errorf("expected 0 POST (no new message), got %d", postCount)
	}

	// Placeholder should be consumed
	if _, ok := env.ch.placeholders.Load("spaces/test"); ok {
		t.Error("placeholder should be consumed after Send")
	}
}

func TestSend_FallbackDelete(t *testing.T) {
	env := testStreamEnv(t)
	env.ch.longFormThreshold = 50 // low threshold to trigger fallback
	ctx := context.Background()

	// Simulate stream handoff
	env.ch.placeholders.Store("spaces/test", "spaces/test/messages/stream-1")

	// Content exceeds longFormThreshold (50 chars)
	longContent := strings.Repeat("x", 100)
	msg := bus.OutboundMessage{
		ChatID:   "spaces/test",
		Content:  longContent,
		Metadata: map[string]string{"peer_kind": "direct"},
	}

	err := env.ch.Send(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}

	recs := env.getRecords()
	deleteCount := 0
	postCount := 0
	for _, r := range recs {
		switch r.Method {
		case "DELETE":
			deleteCount++
		case "POST":
			postCount++
		}
	}

	if deleteCount != 1 {
		t.Errorf("expected 1 DELETE (stream message), got %d", deleteCount)
	}
	if postCount == 0 {
		t.Error("expected POST for new message after fallback")
	}
}

func TestSend_PlaceholderUpdate(t *testing.T) {
	env := testStreamEnv(t)
	ctx := context.Background()

	// Pre-store placeholder
	env.ch.placeholders.Store("spaces/test", "spaces/test/messages/stream-1")

	msg := bus.OutboundMessage{
		ChatID:  "spaces/test",
		Content: "Running tool: search_code",
		Metadata: map[string]string{
			"peer_kind":          "direct",
			"placeholder_update": "true",
		},
	}

	err := env.ch.Send(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}

	// Should edit placeholder (PATCH) but NOT consume it
	recs := env.getRecords()
	patchCount := 0
	for _, r := range recs {
		if r.Method == "PATCH" {
			patchCount++
		}
	}
	if patchCount != 1 {
		t.Errorf("expected 1 PATCH for placeholder update, got %d", patchCount)
	}

	// Placeholder should still exist (not consumed)
	if _, ok := env.ch.placeholders.Load("spaces/test"); !ok {
		t.Error("placeholder should still exist after placeholder_update")
	}
}
