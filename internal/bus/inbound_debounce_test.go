package bus

import (
	"sort"
	"strings"
	"testing"
	"time"
)

func TestInboundDebouncerMergesRapidText(t *testing.T) {
	out := make(chan InboundMessage, 1)
	d := NewInboundDebouncer(20*time.Millisecond, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "one"})
	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "two", Metadata: map[string]string{"message_id": "m2"}})

	got := waitInbound(t, out)
	if got.Content != "one\ntwo" {
		t.Fatalf("merged content = %q, want %q", got.Content, "one\ntwo")
	}
	if got.Metadata["message_id"] != "m2" {
		t.Fatalf("metadata should come from latest message, got %#v", got.Metadata)
	}
}

func TestInboundDebouncerDisabledPassesThrough(t *testing.T) {
	out := make(chan InboundMessage, 2)
	d := NewInboundDebouncer(0, func(msg InboundMessage) {
		out <- msg
	})

	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "one"})
	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "two"})

	if got := waitInbound(t, out); got.Content != "one" {
		t.Fatalf("first content = %q", got.Content)
	}
	if got := waitInbound(t, out); got.Content != "two" {
		t.Fatalf("second content = %q", got.Content)
	}
}

func TestInboundDebouncerDynamicDelay(t *testing.T) {
	out := make(chan InboundMessage, 2)
	d := NewInboundDebouncerFunc(func(msg InboundMessage) time.Duration {
		if msg.AgentID == "instant" {
			return 0
		}
		return 20 * time.Millisecond
	}, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", AgentID: "instant", Content: "one"})
	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", AgentID: "debounced", Content: "two"})
	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", AgentID: "debounced", Content: "three"})

	if got := waitInbound(t, out); got.Content != "one" {
		t.Fatalf("instant content = %q", got.Content)
	}
	if got := waitInbound(t, out); got.Content != "two\nthree" {
		t.Fatalf("debounced content = %q", got.Content)
	}
}

func TestInboundDebouncerSeparatesAgents(t *testing.T) {
	out := make(chan InboundMessage, 2)
	d := NewInboundDebouncer(20*time.Millisecond, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", AgentID: "agent-a", Content: "a"})
	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", AgentID: "agent-b", Content: "b"})

	first := waitInbound(t, out)
	second := waitInbound(t, out)
	got := map[string]string{first.AgentID: first.Content, second.AgentID: second.Content}
	if got["agent-a"] != "a" || got["agent-b"] != "b" {
		t.Fatalf("agent buffers = %#v, want separate flushes", got)
	}
}

// TestInboundDebouncerMergesMediaWithinWindow asserts two media messages within
// the debounce window merge into a single flush with all media in arrival order.
// Replaces the prior media-bypass test (issue #63 — red-team Phase 1 Rule #1 / #2).
func TestInboundDebouncerMergesMediaWithinWindow(t *testing.T) {
	out := make(chan InboundMessage, 1)
	d := NewInboundDebouncer(50*time.Millisecond, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1",
		Media: []MediaFile{{Path: "/tmp/a.png", MimeType: "image/png"}},
	})
	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1",
		Media: []MediaFile{{Path: "/tmp/b.png", MimeType: "image/png"}},
	})

	got := waitInbound(t, out)
	if len(got.Media) != 2 {
		t.Fatalf("merged media count = %d, want 2 (paths: %#v)", len(got.Media), got.Media)
	}
	if got.Media[0].Path != "/tmp/a.png" || got.Media[1].Path != "/tmp/b.png" {
		t.Fatalf("media order wrong: %#v", got.Media)
	}

	// Drain check — no second flush.
	select {
	case extra := <-out:
		t.Fatalf("unexpected second flush: %#v", extra)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestInboundDebouncerFlushesMediaAfterSilence(t *testing.T) {
	out := make(chan InboundMessage, 1)
	d := NewInboundDebouncer(30*time.Millisecond, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1",
		Media: []MediaFile{{Path: "/tmp/a.png"}},
	})

	got := waitInbound(t, out)
	if len(got.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(got.Media))
	}
}

func TestInboundDebouncerMergesTextThenMedia(t *testing.T) {
	out := make(chan InboundMessage, 1)
	d := NewInboundDebouncer(50*time.Millisecond, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "look:"})
	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1",
		Content: "this", Media: []MediaFile{{Path: "/tmp/a.png"}},
	})

	got := waitInbound(t, out)
	if got.Content != "look:\nthis" {
		t.Fatalf("content = %q, want %q", got.Content, "look:\nthis")
	}
	if len(got.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(got.Media))
	}

	select {
	case extra := <-out:
		t.Fatalf("unexpected second flush: %#v", extra)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestInboundDebouncerMergesMixedMediaAndText(t *testing.T) {
	out := make(chan InboundMessage, 1)
	d := NewInboundDebouncer(50*time.Millisecond, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "one"})
	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1",
		Media: []MediaFile{{Path: "/tmp/a.png"}},
	})
	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "two"})

	got := waitInbound(t, out)
	if got.Content != "one\ntwo" {
		t.Fatalf("content = %q, want %q", got.Content, "one\ntwo")
	}
	if len(got.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(got.Media))
	}
}

// TestInboundDebouncerMergesNoMediaFollowupWithBufferedMedia — red-team Rule #1.
// A no-media message arriving with delay==0 while a buffered media message exists
// MUST merge into the buffer, not bypass it.
func TestInboundDebouncerMergesNoMediaFollowupWithBufferedMedia(t *testing.T) {
	out := make(chan InboundMessage, 2)
	// First push has media → returns floor 50ms. Second push has no media → returns 0.
	d := NewInboundDebouncerFunc(func(m InboundMessage) time.Duration {
		if len(m.Media) > 0 {
			return 50 * time.Millisecond
		}
		return 0
	}, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "caption",
		Media: []MediaFile{{Path: "/tmp/a.png"}},
	})
	// Arrive while buffered (before 50ms window elapses).
	time.Sleep(10 * time.Millisecond)
	d.Push(InboundMessage{Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "ps"})

	got := waitInbound(t, out)
	if got.Content != "caption\nps" {
		t.Fatalf("content = %q, want %q (follow-up must merge, not bypass)", got.Content, "caption\nps")
	}
	if len(got.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(got.Media))
	}

	select {
	case extra := <-out:
		t.Fatalf("unexpected second flush — no-media follow-up bypassed instead of merging: %#v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestInboundDebouncerMergePopulatesMergedMessageIDs — red-team Rule #4 / Finding #10.
// Merged flush MUST populate metadata["merged_message_ids"] with all source IDs.
func TestInboundDebouncerMergePopulatesMergedMessageIDs(t *testing.T) {
	out := make(chan InboundMessage, 1)
	d := NewInboundDebouncer(40*time.Millisecond, func(msg InboundMessage) {
		out <- msg
	})
	defer d.Stop()

	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "a",
		Metadata: map[string]string{"message_id": "m1"},
	})
	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "b",
		Metadata: map[string]string{"message_id": "m2"},
	})
	d.Push(InboundMessage{
		Channel: "telegram", ChatID: "chat-1", SenderID: "user-1", Content: "c",
		Metadata: map[string]string{"message_id": "m3"},
	})

	got := waitInbound(t, out)
	merged := got.Metadata["merged_message_ids"]
	if merged == "" {
		t.Fatalf("merged_message_ids not populated; metadata = %#v", got.Metadata)
	}
	ids := strings.Split(merged, ",")
	sort.Strings(ids)
	want := []string{"m1", "m2", "m3"}
	if !equalStringSlice(ids, want) {
		t.Fatalf("merged_message_ids = %v, want (sorted) %v", ids, want)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func waitInbound(t *testing.T, ch <-chan InboundMessage) InboundMessage {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for debounced message")
		return InboundMessage{}
	}
}
