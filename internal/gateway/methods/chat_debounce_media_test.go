package methods

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// TestChatDebounceDelay_HasMediaWithZeroConfigAppliesFloor — Phase 1.5 Rule #2.
// When global debounce is disabled, no agent override, and the message carries
// media, the 1000ms media floor MUST be applied.
func TestChatDebounceDelay_HasMediaWithZeroConfigAppliesFloor(t *testing.T) {
	got := chatDebounceDelay(&config.Config{}, nil, true)
	want := time.Duration(chatMediaDebounceFloorMs) * time.Millisecond
	if got != want {
		t.Fatalf("chatDebounceDelay(cfg=0, hasMedia=true) = %s, want %s", got, want)
	}
}

// TestChatDebounceDelay_AgentOverrideBelowFloorHonored — Rule #2 precedence.
// Floor fires only when post-override delay == 0. A 500ms override MUST be honored.
func TestChatDebounceDelay_AgentOverrideBelowFloorHonored(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 0
	got := chatDebounceDelay(cfg, []byte(`{"inbound_debounce_ms":500}`), true)
	if got != 500*time.Millisecond {
		t.Fatalf("override 500ms with media = %s, want 500ms (floor must not raise)", got)
	}
}

// TestChatDebounceDelay_NoMediaZeroConfig: floor does NOT apply when no media.
func TestChatDebounceDelay_NoMediaZeroConfig(t *testing.T) {
	got := chatDebounceDelay(&config.Config{}, nil, false)
	if got != 0 {
		t.Fatalf("chatDebounceDelay(cfg=0, hasMedia=false) = %s, want 0", got)
	}
}

// TestChatDebounceDelay_MediaConfigAboveFloorUnchanged: cfg already above floor → unchanged.
func TestChatDebounceDelay_MediaConfigAboveFloorUnchanged(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 2000
	got := chatDebounceDelay(cfg, nil, true)
	if got != 2000*time.Millisecond {
		t.Fatalf("chatDebounceDelay(cfg=2000, hasMedia=true) = %s, want 2s", got)
	}
}

// TestChatDebouncer_NoMediaFollowupMergesIntoBufferedMedia — Rule #4.
// When a follow-up Push arrives with delay==0 while a buffer exists for the key,
// it MUST merge into the buffer rather than dispatch immediately.
func TestChatDebouncer_NoMediaFollowupMergesIntoBufferedMedia(t *testing.T) {
	out := make(chan []chatSendRequest, 2)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	// First push: media-bearing, 50ms window.
	d.Push("u1:s1", 50*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "caption"}})
	// Second push: arrives while buffered, delay==0 (no media follow-up).
	time.Sleep(10 * time.Millisecond)
	d.Push("u1:s1", 0, chatSendRequest{params: chatSendParams{Message: "ps"}})

	items := waitChatDebounce(t, out)
	if len(items) != 2 {
		t.Fatalf("flushed items = %d, want 2 (follow-up must merge, not bypass)", len(items))
	}
	merged := mergeChatSendRequests(items).Message
	if merged != "caption\nps" {
		t.Fatalf("merged = %q, want %q", merged, "caption\nps")
	}

	assertNoChatDebounceFlush(t, out)
}

// TestChatDebouncer_DelayZeroNoBufferStillDispatches: delay==0 with empty buffer
// dispatches immediately (preserves existing behavior for plain text sends).
func TestChatDebouncer_DelayZeroNoBufferStillDispatches(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", 0, chatSendRequest{params: chatSendParams{Message: "one"}})
	items := waitChatDebounce(t, out)
	if len(items) != 1 || items[0].params.Message != "one" {
		t.Fatalf("dispatch = %#v, want single 'one'", items)
	}
}
