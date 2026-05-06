package max

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// =====================================================================
// Helpers
// =====================================================================

// mockStreamBackend captures POST /messages, PUT /messages, DELETE /messages
// calls so streaming tests can assert on the exact sequence of API actions.
type mockStreamBackend struct {
	server *httptest.Server

	mu    sync.Mutex
	calls []streamCall
}

type streamCall struct {
	Method string
	Path   string
	Query  map[string]string
	Body   map[string]any
}

func newStreamBackend(t *testing.T) *mockStreamBackend {
	t.Helper()
	m := &mockStreamBackend{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		call := streamCall{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  flattenQueryStream(r.URL.Query()),
			Body:   map[string]any{},
		}
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &call.Body)
		}
		m.calls = append(m.calls, call)

		// Default success response for SendMessage / EditMessage.
		var mid string
		if call.Method == http.MethodPut {
			// EditMessage echoes the message_id from the query.
			mid = call.Query["message_id"]
		} else {
			// SendMessage allocates a new mid based on call count.
			mid = "mid.test_" + strconv.Itoa(len(m.calls))
		}

		chatID := call.Query["chat_id"]
		if chatID == "" {
			chatID = "0"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"message":{"timestamp":1,"message":{"mid":"`+mid+`","seq":1}},
			"chat_id":`+chatID+`,
			"recipient_id":`+chatID+`,
			"message_id":"`+mid+`"
		}`)
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockStreamBackend) snapshot() []streamCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]streamCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func flattenQueryStream(v map[string][]string) map[string]string {
	out := make(map[string]string, len(v))
	for k, vs := range v {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// streamChannel returns a Channel pointed at the mock backend, suitable
// for stream tests.
func streamChannel(t *testing.T, m *mockStreamBackend) *Channel {
	t.Helper()
	creds := instanceCreds{BotToken: "tok", BotID: 256747471, Username: "test"}
	cfg := instanceConfig{Mode: "polling", PollingTimeout: 30, DMPolicy: "open"}
	c, err := New("max-stream-test", creds, cfg, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.client = NewClient("tok", WithBaseURL(m.server.URL), WithMaxRetries(1))
	return c
}

// =====================================================================
// StreamEnabled — config defaults
// =====================================================================

func TestStreamEnabled_DefaultDM(t *testing.T) {
	c := streamChannel(t, newStreamBackend(t))
	if !c.StreamEnabled(false) {
		t.Error("expected DM streaming ON by default")
	}
}

func TestStreamEnabled_DefaultGroup(t *testing.T) {
	c := streamChannel(t, newStreamBackend(t))
	if c.StreamEnabled(true) {
		t.Error("expected group streaming OFF by default")
	}
}

func TestStreamEnabled_DMOverride(t *testing.T) {
	c := streamChannel(t, newStreamBackend(t))
	off := false
	c.cfg.DMStream = &off
	if c.StreamEnabled(false) {
		t.Error("DMStream=false should disable DM streaming")
	}
}

func TestStreamEnabled_GroupOverride(t *testing.T) {
	c := streamChannel(t, newStreamBackend(t))
	on := true
	c.cfg.GroupStream = &on
	if !c.StreamEnabled(true) {
		t.Error("GroupStream=true should enable group streaming")
	}
}

func TestReasoningStreamEnabled_AlwaysFalse(t *testing.T) {
	c := streamChannel(t, newStreamBackend(t))
	if c.ReasoningStreamEnabled() {
		t.Error("ReasoningStreamEnabled should be false (Опция 2)")
	}
}

// =====================================================================
// CreateStream — placeholder lifecycle
// =====================================================================

func TestCreateStream_SendsPlaceholder(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, err := c.CreateStream(context.Background(), "188289857", true)
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	if stream == nil {
		t.Fatal("stream is nil")
	}

	calls := m.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 SendMessage for placeholder, got %d", len(calls))
	}
	c0 := calls[0]
	if c0.Method != http.MethodPost || c0.Path != "/messages" {
		t.Errorf("expected POST /messages, got %s %s", c0.Method, c0.Path)
	}
	if c0.Query["chat_id"] != "188289857" {
		t.Errorf("chat_id = %q", c0.Query["chat_id"])
	}
	gotText, _ := c0.Body["text"].(string)
	if gotText != streamPlaceholderText {
		t.Errorf("placeholder text = %q, want %q", gotText, streamPlaceholderText)
	}
	// Plain text — no `format` field.
	if _, hasFormat := c0.Body["format"]; hasFormat {
		t.Error("placeholder should be plain text (no format field)")
	}
}

func TestCreateStream_InvalidChatID(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	_, err := c.CreateStream(context.Background(), "not-a-number", true)
	if err == nil {
		t.Fatal("expected error for invalid chat ID")
	}

	if calls := m.snapshot(); len(calls) != 0 {
		t.Errorf("no API calls expected on bad chatID, got %d", len(calls))
	}
}

// =====================================================================
// Stream.Update — throttle and dedup
// =====================================================================

func TestStream_Update_ThrottledNoEdit(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, err := c.CreateStream(context.Background(), "188289857", true)
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}

	// Immediately after CreateStream, lastEdit was set — throttle window is open.
	// Update should NOT trigger an edit.
	stream.Update(context.Background(), "первая часть")

	if calls := m.snapshot(); len(calls) != 1 {
		// Only the initial placeholder POST; no edit.
		t.Errorf("expected 1 call (placeholder), got %d", len(calls))
	}
}

func TestStream_Update_EditAfterThrottle(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)

	// Force throttle to elapse by manually backing up lastEdit.
	ms := stream.(*maxStream)
	ms.mu.Lock()
	ms.lastEdit = time.Now().Add(-2 * streamThrottleInterval)
	ms.mu.Unlock()

	stream.Update(context.Background(), "часть текста")

	calls := m.snapshot()
	// Expect placeholder POST + 1 edit.
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Method != http.MethodPut {
		t.Errorf("call[1] should be PUT (edit), got %s", calls[1].Method)
	}
	gotText, _ := calls[1].Body["text"].(string)
	if gotText != "часть текста" {
		t.Errorf("edit text = %q", gotText)
	}
}

func TestStream_Update_DedupSkipsIdenticalText(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)
	ms := stream.(*maxStream)

	// Force throttle to elapse, send once, then send same text — dedup should skip.
	ms.mu.Lock()
	ms.lastEdit = time.Now().Add(-2 * streamThrottleInterval)
	ms.mu.Unlock()

	stream.Update(context.Background(), "одинаковый текст")

	ms.mu.Lock()
	ms.lastEdit = time.Now().Add(-2 * streamThrottleInterval)
	ms.mu.Unlock()

	stream.Update(context.Background(), "одинаковый текст") // same → dedup

	calls := m.snapshot()
	// placeholder + 1 edit; dedup skipped the second edit
	if len(calls) != 2 {
		t.Errorf("expected 2 calls (placeholder + 1 edit), got %d", len(calls))
	}
}

func TestStream_Update_AccumulatesPendingDuringThrottle(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)
	ms := stream.(*maxStream)

	// First Update during throttle — pending stored, no edit.
	stream.Update(context.Background(), "first")

	// Second Update with newer text — supersedes.
	stream.Update(context.Background(), "first second")

	// Still throttled — no edits sent yet.
	if calls := m.snapshot(); len(calls) != 1 {
		t.Errorf("expected 1 call (placeholder), got %d", len(calls))
	}

	// Verify pending was overwritten with the latest text.
	ms.mu.Lock()
	pending := ms.pending
	ms.mu.Unlock()
	if pending != "first second" {
		t.Errorf("pending = %q, want 'first second'", pending)
	}
}

// =====================================================================
// Stream.Stop — final flush
// =====================================================================

func TestStream_Stop_FlushesPending(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)

	// Buffer text during throttle.
	stream.Update(context.Background(), "buffered")

	if err := stream.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	calls := m.snapshot()
	// placeholder + final flush edit
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[1].Method != http.MethodPut {
		t.Errorf("final flush should be PUT, got %s", calls[1].Method)
	}
}

func TestStream_Stop_Idempotent(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)
	stream.Update(context.Background(), "x")

	if err := stream.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := stream.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}

	calls := m.snapshot()
	// Only one final flush even after two Stop calls.
	if len(calls) > 2 {
		t.Errorf("Stop is not idempotent: got %d calls", len(calls))
	}
}

func TestStream_Update_AfterStopIsNoOp(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)
	_ = stream.Stop(context.Background())

	pre := len(m.snapshot())

	// Update after Stop should not generate API calls.
	stream.Update(context.Background(), "late text")

	post := len(m.snapshot())
	if pre != post {
		t.Errorf("Update after Stop fired API calls: %d → %d", pre, post)
	}
}

// =====================================================================
// MessageID — interface contract
// =====================================================================

func TestStream_MessageID_AlwaysZero(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)
	if got := stream.MessageID(); got != 0 {
		t.Errorf("MessageID() = %d, want 0 (Max uses string mids)", got)
	}
}

// =====================================================================
// FinalizeStream — handoff to placeholders
// =====================================================================

func TestFinalizeStream_StoresMessageID(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)

	// Force an Update through so lastSent is non-empty — required for
	// FinalizeStream to hand off rather than delete the placeholder.
	ms := stream.(*maxStream)
	ms.mu.Lock()
	ms.lastEdit = time.Now().Add(-2 * streamThrottleInterval)
	ms.mu.Unlock()
	stream.Update(context.Background(), "some content")

	c.FinalizeStream(context.Background(), "188289857", stream)

	v, ok := c.placeholders.Load("188289857")
	if !ok {
		t.Fatal("expected placeholder stored after FinalizeStream with content")
	}
	stored, _ := v.(string)
	if !strings.HasPrefix(stored, "mid.test_") {
		t.Errorf("stored = %q, expected mid.test_*", stored)
	}
}

// TestFinalizeStream_DeletesOrphanPlaceholder — Day 5b regression.
// When a stream is created (placeholder posted) but never receives any
// successful Update (e.g. agent crash before first chunk), FinalizeStream
// must DELETE the placeholder rather than hand off to placeholders. Otherwise
// "💭 Thinking..." lives in the chat indefinitely if no Send follows.
func TestFinalizeStream_DeletesOrphanPlaceholder(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	stream, _ := c.CreateStream(context.Background(), "188289857", true)
	// No Update calls — stream has no content.

	c.FinalizeStream(context.Background(), "188289857", stream)

	// Placeholders map must be empty — no handoff happened.
	if _, ok := c.placeholders.Load("188289857"); ok {
		t.Error("placeholder should NOT be stored when stream had no content")
	}

	// Backend must have received a DELETE for the placeholder mid.
	calls := m.snapshot()
	hasDelete := false
	for _, call := range calls {
		if call.Method == http.MethodDelete && call.Path == "/messages" {
			hasDelete = true
			break
		}
	}
	if !hasDelete {
		t.Errorf("expected DELETE /messages for orphan placeholder; calls=%v", calls)
	}
}

func TestFinalizeStream_NoMessageIDIsNoOp(t *testing.T) {
	c := streamChannel(t, newStreamBackend(t))

	// A bare maxStream with no messageID set.
	stream := &maxStream{client: c.client, chatID: 999}
	c.FinalizeStream(context.Background(), "999", stream)

	if _, ok := c.placeholders.Load("999"); ok {
		t.Error("placeholders should be empty when stream had no messageID")
	}
}

func TestFinalizeStream_WrongTypeIsNoOp(t *testing.T) {
	c := streamChannel(t, newStreamBackend(t))

	// A different ChannelStream impl — should be type-asserted away.
	c.FinalizeStream(context.Background(), "999", &bogusStream{})

	if _, ok := c.placeholders.Load("999"); ok {
		t.Error("placeholders should not be modified for non-maxStream")
	}
}

// bogusStream is a ChannelStream not from this package; used to test the
// type assertion in FinalizeStream.
type bogusStream struct{}

func (bogusStream) Update(context.Context, string) {}
func (bogusStream) Stop(context.Context) error     { return nil }
func (bogusStream) MessageID() int                 { return 0 }

// Compile-time check.
var _ channels.ChannelStream = (*bogusStream)(nil)

// =====================================================================
// Send — placeholder handoff (the critical end-to-end path)
// =====================================================================

func TestSend_EditsExistingPlaceholder(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	// Simulate: a prior streaming run left a placeholder.
	c.placeholders.Store("188289857", "mid.placeholder.abc")

	err := c.Send(context.Background(), bus.OutboundMessage{
		Channel: "max-stream-test",
		ChatID:  "188289857",
		Content: "Final formatted **answer**.",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	calls := m.snapshot()
	// Expect one PUT (edit placeholder), no POST.
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (edit), got %d", len(calls))
	}
	if calls[0].Method != http.MethodPut {
		t.Errorf("expected PUT, got %s", calls[0].Method)
	}
	if calls[0].Query["message_id"] != "mid.placeholder.abc" {
		t.Errorf("message_id = %q", calls[0].Query["message_id"])
	}
	if got, _ := calls[0].Body["format"].(string); got != "markdown" {
		t.Errorf("final edit format = %q, want 'markdown'", got)
	}
}

func TestSend_NoPlaceholderSendsFresh(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	// No placeholder pre-stored → normal path.
	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "188289857",
		Content: "Hello",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	calls := m.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != http.MethodPost {
		t.Errorf("expected POST, got %s", calls[0].Method)
	}
}

func TestSend_PlaceholderConsumedOnce(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	// Pre-store a placeholder to simulate FinalizeStream'd state.
	c.placeholders.Store("188289857", "mid.placeholder.abc")

	// First Send should EDIT the placeholder.
	if err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "188289857",
		Content: "first answer",
	}); err != nil {
		t.Fatalf("first Send: %v", err)
	}

	// Placeholder should be consumed (deleted from map). After editing the
	// finalized placeholder, we deliberately don't re-store the mid — the
	// next Send into this chat must produce a fresh message, not overwrite
	// the user-visible answer.
	if _, ok := c.placeholders.Load("188289857"); ok {
		t.Error("placeholder should be consumed after edit")
	}

	// Second Send should SEND a fresh message (no edit).
	if err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "188289857",
		Content: "second answer",
	}); err != nil {
		t.Fatalf("second Send: %v", err)
	}

	calls := m.snapshot()

	putCount := 0
	postCount := 0
	for _, c := range calls {
		switch c.Method {
		case http.MethodPut:
			putCount++
		case http.MethodPost:
			postCount++
		}
	}
	if putCount != 1 {
		t.Errorf("expected 1 PUT (first edit), got %d", putCount)
	}
	if postCount != 1 {
		t.Errorf("expected 1 POST (second fresh send), got %d", postCount)
	}
}

// =====================================================================
// CreateStream + Update + Stop + FinalizeStream + Send — full lifecycle
// =====================================================================

func TestStreamingFullLifecycle(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	// 1. CreateStream → placeholder POST
	stream, err := c.CreateStream(context.Background(), "188289857", true)
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}

	// 2. Update with throttle bypass to force one edit.
	ms := stream.(*maxStream)
	ms.mu.Lock()
	ms.lastEdit = time.Now().Add(-2 * streamThrottleInterval)
	ms.mu.Unlock()
	stream.Update(context.Background(), "in-progress text")

	// 3. Stop — final flush of any pending text (none here, latest already sent).
	if err := stream.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// 4. FinalizeStream — places the messageID on c.placeholders.
	c.FinalizeStream(context.Background(), "188289857", stream)

	// 5. Send the final formatted answer — this should EDIT the placeholder.
	err = c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "188289857",
		Content: "Final **markdown** answer.",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	calls := m.snapshot()

	// Expected sequence:
	//   POST /messages (placeholder)
	//   PUT  /messages?message_id=... (streaming edit)
	//   PUT  /messages?message_id=... (Send replaces with markdown)
	if len(calls) < 3 {
		t.Fatalf("expected >= 3 API calls, got %d", len(calls))
	}

	postCount := 0
	putCount := 0
	for _, c := range calls {
		if c.Method == http.MethodPost {
			postCount++
		}
		if c.Method == http.MethodPut {
			putCount++
		}
	}
	if postCount != 1 {
		t.Errorf("expected exactly 1 POST (placeholder), got %d", postCount)
	}
	if putCount < 2 {
		t.Errorf("expected >= 2 PUTs (streaming + final), got %d", putCount)
	}

	// The final PUT must use markdown.
	last := calls[len(calls)-1]
	if last.Method != http.MethodPut {
		t.Errorf("last call should be PUT, got %s", last.Method)
	}
	if got, _ := last.Body["format"].(string); got != "markdown" {
		t.Errorf("final edit format = %q, want 'markdown'", got)
	}
}

// =====================================================================
// Concurrent streams — documents known limitation
// =====================================================================

// TestStreaming_ConcurrentRuns_DoNotInterfere — Day 5b regression check.
//
// When two agent runs are active in the same chat simultaneously, each
// gets its own ChannelStream from CreateStream. Each posts an independent
// "💭 Thinking..." placeholder. This is correct (each run is independent).
//
// HOWEVER, the placeholder handoff via c.placeholders is keyed only on
// chatID — so the second FinalizeStream overwrites the first. The first
// Send then accidentally consumes the *second* run's placeholder mid.
//
// In production this is practically unreachable because:
//  1. goclaw debounce coalesces rapid messages from one user into one run
//  2. per-session run limits cap concurrent runs at 1 in DM
//
// This test exists to:
//   - Verify CreateStream is independent (each run gets its own placeholder)
//   - Document the placeholder collision so future code changes preserve
//     or fix the behavior intentionally, not accidentally
//
// If you change c.placeholders to be per-run (via RunContext), update this
// test to assert correct routing.
func TestStreaming_ConcurrentRuns_DoNotInterfere(t *testing.T) {
	m := newStreamBackend(t)
	c := streamChannel(t, m)

	// Two parallel runs in the same chat.
	streamA, errA := c.CreateStream(context.Background(), "188289857", true)
	streamB, errB := c.CreateStream(context.Background(), "188289857", true)
	if errA != nil || errB != nil {
		t.Fatalf("CreateStream errors: A=%v B=%v", errA, errB)
	}

	// Both stream handles must be distinct and have independent message IDs.
	msA := streamA.(*maxStream)
	msB := streamB.(*maxStream)
	if msA == msB {
		t.Fatal("CreateStream returned the same handle twice")
	}
	if msA.messageID == "" || msB.messageID == "" {
		t.Fatal("both streams must have placeholder mids")
	}
	if msA.messageID == msB.messageID {
		t.Errorf("placeholder collision: both streams have mid=%q", msA.messageID)
	}

	// Send Update on each — backend should record edits to each placeholder.
	msA.mu.Lock()
	msA.lastEdit = time.Now().Add(-2 * streamThrottleInterval)
	msA.mu.Unlock()
	streamA.Update(context.Background(), "from run A")

	msB.mu.Lock()
	msB.lastEdit = time.Now().Add(-2 * streamThrottleInterval)
	msB.mu.Unlock()
	streamB.Update(context.Background(), "from run B")

	// Both PUT calls should have happened; we can't easily map which to
	// which without inspecting the URL message_id query param, so we just
	// count.
	calls := m.snapshot()
	posts := 0
	puts := 0
	for _, call := range calls {
		switch call.Method {
		case http.MethodPost:
			posts++
		case http.MethodPut:
			puts++
		}
	}
	if posts != 2 {
		t.Errorf("expected 2 POSTs (one placeholder per run), got %d", posts)
	}
	if puts != 2 {
		t.Errorf("expected 2 PUTs (one update per run), got %d", puts)
	}

	// Document the known placeholder collision: FinalizeStream on B
	// overwrites FinalizeStream on A. This is the bug we're documenting.
	c.FinalizeStream(context.Background(), "188289857", streamA)
	c.FinalizeStream(context.Background(), "188289857", streamB)

	v, ok := c.placeholders.Load("188289857")
	if !ok {
		t.Fatal("expected placeholder after FinalizeStream chain")
	}
	stored, _ := v.(string)
	if stored != msB.messageID {
		t.Errorf("known limitation: expected last-finalize-wins (B's mid), got %q", stored)
	}
}
