package max

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// =====================================================================
// Integration tests — exercise the full Channel lifecycle end-to-end.
// Unlike per-file unit tests these don't isolate components; they assert
// that real wiring works (poll → translator → bus → Send → mock backend).
// =====================================================================

// integrationBackend simulates Max API just enough to drive a real
// Channel through Start → poll → handle → Send → Stop.
type integrationBackend struct {
	server *httptest.Server

	mu             sync.Mutex
	updatesQueue   []string // raw JSON Update objects to deliver
	updatesServed  int      // how many GetUpdates calls served queued data
	sendsCaptured  []map[string]any
	editsCaptured  []map[string]any
	getMeCalls     int32
	getUpdateCalls int32
}

func newIntegrationBackend(t *testing.T) *integrationBackend {
	t.Helper()
	b := &integrationBackend{}
	b.server = httptest.NewServer(http.HandlerFunc(b.handle))
	t.Cleanup(b.server.Close)
	return b
}

func (b *integrationBackend) handle(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/me":
		atomic.AddInt32(&b.getMeCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"user_id":256747471,"first_name":"3-С","username":"id_test_bot","is_bot":true}`)

	case "/updates":
		atomic.AddInt32(&b.getUpdateCalls, 1)
		b.serveUpdates(w, r)

	case "/messages":
		b.serveMessages(w, r)

	default:
		// Unknown path — return empty success so we don't crash on edge
		// API calls (e.g. POST /actions during reaction refresh).
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}
}

func (b *integrationBackend) serveUpdates(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	var update string
	if len(b.updatesQueue) > 0 {
		update = b.updatesQueue[0]
		b.updatesQueue = b.updatesQueue[1:]
		b.updatesServed++
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if update == "" {
		// No queued updates — return empty list. Real long-polling would
		// block until timeout, but for the test fast-empty is fine.
		_, _ = io.WriteString(w, `{"updates":[],"marker":1}`)
		return
	}
	_, _ = io.WriteString(w, `{"updates":[`+update+`],"marker":2}`)
}

func (b *integrationBackend) serveMessages(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)

	b.mu.Lock()
	switch r.Method {
	case http.MethodPost:
		b.sendsCaptured = append(b.sendsCaptured, parsed)
	case http.MethodPut:
		b.editsCaptured = append(b.editsCaptured, parsed)
	}
	b.mu.Unlock()

	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		chatID = "0"
	}
	mid := "mid.integ_" + r.URL.Query().Get("message_id")
	if r.Method == http.MethodPost {
		mid = "mid.integ_new"
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{
		"message":{"timestamp":1,"message":{"mid":"`+mid+`","seq":1}},
		"chat_id":`+chatID+`,
		"recipient_id":`+chatID+`,
		"message_id":"`+mid+`"
	}`)
}

// queueUpdate appends a JSON-encoded Update object to be returned by the
// next /updates call.
func (b *integrationBackend) queueUpdate(json string) {
	b.mu.Lock()
	b.updatesQueue = append(b.updatesQueue, json)
	b.mu.Unlock()
}

func (b *integrationBackend) sendsLen() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.sendsCaptured)
}

// integrationChannel wires up a real Channel pointed at the integration
// backend, with a real bus. Caller still has to Start() and Stop().
func integrationChannel(t *testing.T, b *integrationBackend) (*Channel, *bus.MessageBus) {
	t.Helper()
	creds := instanceCreds{BotToken: "tok", BotID: 256747471, Username: "id_test_bot"}
	cfg := instanceConfig{Mode: "polling", PollingTimeout: 1, DMPolicy: "open"}
	msgBus := bus.New()
	c, err := New("integ-bot", creds, cfg, msgBus, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.client = NewClient("tok", WithBaseURL(b.server.URL), WithMaxRetries(1))
	return c, msgBus
}

// TestIntegration_FullPipeline — Day 5b regression for the full flow.
//
// Verifies:
//  1. Channel.Start probes /me (auth check).
//  2. Polling fetches /updates and dispatches translated InboundMessage onto bus.
//  3. Bus consumer receives the message with correct fields.
//  4. Channel.Send delivers the reply to /messages POST.
//  5. Channel.Stop drains polling and handlers cleanly within timeout.
//
// This is the safety net we lacked: prior tests covered each step in
// isolation but never the full chain. A regression in any wire-up
// (e.g. translator silently dropping a field, Send routing wrong) would
// have escaped detection.
func TestIntegration_FullPipeline(t *testing.T) {
	b := newIntegrationBackend(t)
	c, msgBus := integrationChannel(t, b)

	// Queue one DM update before Start so the first /updates call delivers it.
	b.queueUpdate(`{
		"update_type": "message_created",
		"timestamp": 1730000000,
		"message": {
			"sender":    {"user_id": 74757262, "first_name": "Тарас"},
			"recipient": {"chat_type": "dialog", "user_id": 256747471, "chat_id": 188289857},
			"timestamp": 1730000000,
			"message":   {"mid": "mid.in_1", "seq": 100, "text": "Привет, бот!"}
		}
	}`)

	// 1. Start — should call /me, then begin polling in a goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := atomic.LoadInt32(&b.getMeCalls); got < 1 {
		t.Errorf("expected /me to have been called at least once, got %d", got)
	}

	// 2-3. Wait for the polled update to arrive on the bus.
	consumeCtx, consumeCancel := context.WithTimeout(ctx, 3*time.Second)
	defer consumeCancel()

	inbound, ok := msgBus.ConsumeInbound(consumeCtx)
	if !ok {
		t.Fatalf("ConsumeInbound returned !ok; getUpdateCalls=%d",
			atomic.LoadInt32(&b.getUpdateCalls))
	}

	if inbound.Channel != c.Name() {
		t.Errorf("inbound.Channel = %q, want %q", inbound.Channel, c.Name())
	}
	if inbound.SenderID != "74757262" {
		t.Errorf("inbound.SenderID = %q, want %q", inbound.SenderID, "74757262")
	}
	if inbound.ChatID != "188289857" {
		t.Errorf("inbound.ChatID = %q, want %q", inbound.ChatID, "188289857")
	}
	if inbound.Content != "Привет, бот!" {
		t.Errorf("inbound.Content = %q, want %q", inbound.Content, "Привет, бот!")
	}

	// 4. Channel.Send delivers a reply.
	if err := c.Send(ctx, bus.OutboundMessage{
		Channel: c.Name(),
		ChatID:  inbound.ChatID,
		Content: "Hi back!",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Backend should have received exactly one POST /messages.
	if got := b.sendsLen(); got != 1 {
		t.Errorf("expected 1 POST /messages, got %d", got)
	}

	// 5. Stop drains gracefully.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()
	if err := c.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
