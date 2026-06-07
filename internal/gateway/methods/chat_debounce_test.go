package methods

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestMergeChatSendRequestsJoinsContentAndUsesLatestParams(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Message: "first", AgentID: "agent-a", SessionKey: "session-a", Stream: false}},
		{params: chatSendParams{Message: "", AgentID: "agent-a", SessionKey: "session-a", Stream: true}},
		{params: chatSendParams{Message: "second", AgentID: "agent-a", SessionKey: "session-a", Stream: true}},
	}

	got := mergeChatSendRequests(items)
	if got.Message != "first\nsecond" {
		t.Fatalf("merged message = %q, want %q", got.Message, "first\nsecond")
	}
	if !got.Stream {
		t.Fatal("latest params should win for stream flag")
	}
}

func TestChatDebouncerFlushesOnceAfterQuietWindow(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", 20*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "one"}})
	d.Push("u1:s1", 20*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "two"}})

	items := waitChatDebounce(t, out)
	if len(items) != 2 {
		t.Fatalf("flushed items = %d, want 2", len(items))
	}
	if got := mergeChatSendRequests(items).Message; got != "one\ntwo" {
		t.Fatalf("merged message = %q", got)
	}
}

func TestChatDebouncerTakeDrainsPendingBeforeBypass(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", time.Minute, chatSendRequest{params: chatSendParams{Message: "pending"}})

	items := d.Take("u1:s1")
	if len(items) != 1 || items[0].params.Message != "pending" {
		t.Fatalf("flushed items = %#v", items)
	}
	assertNoChatDebounceFlush(t, out)
}

func TestChatDebouncerDiscardDropsPendingBeforeCancel(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", 20*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "pending"}})
	d.Discard("u1:s1")

	assertNoChatDebounceFlush(t, out)
}

func TestChatDebounceDelayGlobalAndAgentOverride(t *testing.T) {
	// hasMedia=false: legacy behavior preserved (no floor applied).
	if got := chatDebounceDelay(&config.Config{}, nil, false); got != 0 {
		t.Fatalf("default debounce = %s, want disabled", got)
	}
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 250
	if got := chatDebounceDelay(cfg, nil, false); got != 250*time.Millisecond {
		t.Fatalf("global debounce = %s, want 250ms", got)
	}
	if got := chatDebounceDelay(cfg, []byte(`{"inbound_debounce_ms":0}`), false); got != 0 {
		t.Fatalf("agent disabled debounce = %s, want disabled", got)
	}
	if got := chatDebounceDelay(cfg, []byte(`{"inbound_debounce_ms":500}`), false); got != 500*time.Millisecond {
		t.Fatalf("agent custom debounce = %s, want 500ms", got)
	}
	if got := chatDebounceDelay(cfg, []byte(`{"other":true}`), false); got != 250*time.Millisecond {
		t.Fatalf("agent inherit debounce = %s, want 250ms", got)
	}
}

func waitChatDebounce(t *testing.T, ch <-chan []chatSendRequest) []chatSendRequest {
	t.Helper()
	select {
	case items := <-ch:
		return items
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for chat debounce flush")
		return nil
	}
}

func assertNoChatDebounceFlush(t *testing.T, ch <-chan []chatSendRequest) {
	t.Helper()
	select {
	case items := <-ch:
		t.Fatalf("unexpected flush: %#v", items)
	case <-time.After(50 * time.Millisecond):
	}
}
