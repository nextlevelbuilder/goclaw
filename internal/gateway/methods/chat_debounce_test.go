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

func TestChatDebounceDelayDefaultAndDisabled(t *testing.T) {
	if got := chatDebounceDelay(&config.Config{}); got != time.Second {
		t.Fatalf("default debounce = %s, want 1s", got)
	}
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = -1
	if got := chatDebounceDelay(cfg); got >= 0 {
		t.Fatalf("disabled debounce = %s, want negative duration", got)
	}
	cfg.Gateway.InboundDebounceMs = 250
	if got := chatDebounceDelay(cfg); got != 250*time.Millisecond {
		t.Fatalf("custom debounce = %s, want 250ms", got)
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
