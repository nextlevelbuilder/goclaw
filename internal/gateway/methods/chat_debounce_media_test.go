package methods

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// jsonMedia encodes media items as the canonical {path,filename} JSON the wire
// format carries, so tests build chatSendParams.Media the same way clients do.
func jsonMedia(items ...chatMediaItem) json.RawMessage {
	b, err := json.Marshal(items)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(b)
}

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

// --- Media survival across the batch merge (Phase 7 review 7A-H3) ---
//
// A debounce batch is ONE turn (7A-M1), so ALL media in the window must survive
// the merge in arrival order — not just the last send's. The pre-fix merge took
// the last item's params verbatim and joined only text, silently dropping media
// attached to earlier sends. These cover the four required shapes: media→text,
// text→media, media→media (multi-attachment), and attachment-only.

// mediaPaths parses the merged params and returns the attachment paths in order,
// so each case asserts on the concrete media that survived, not just its count.
func mediaPaths(p chatSendParams) []string {
	items := p.parseMedia()
	paths := make([]string, 0, len(items))
	for _, it := range items {
		paths = append(paths, it.Path)
	}
	return paths
}

func mediaItem(path, filename, message string) chatSendRequest {
	return chatSendRequest{params: chatSendParams{
		Message: message,
		Media:   jsonMedia(chatMediaItem{Path: path, Filename: filename}),
	}}
}

func TestMergeChatSendRequests_MediaThenText_KeepsMedia(t *testing.T) {
	items := []chatSendRequest{
		mediaItem("/tmp/a.png", "a.png", "look"),
		{params: chatSendParams{Message: "what is this"}},
	}
	merged := mergeChatSendRequests(items)
	if got := mediaPaths(merged); len(got) != 1 || got[0] != "/tmp/a.png" {
		t.Fatalf("media→text merge lost the earlier attachment: paths=%v", got)
	}
	if merged.Message != "look\nwhat is this" {
		t.Fatalf("merged text = %q", merged.Message)
	}
}

func TestMergeChatSendRequests_TextThenMedia_KeepsMedia(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Message: "here"}},
		mediaItem("/tmp/b.png", "b.png", ""),
	}
	merged := mergeChatSendRequests(items)
	if got := mediaPaths(merged); len(got) != 1 || got[0] != "/tmp/b.png" {
		t.Fatalf("text→media merge lost the attachment: paths=%v", got)
	}
	if merged.Message != "here" {
		t.Fatalf("merged text = %q, want the single text part", merged.Message)
	}
}

func TestMergeChatSendRequests_MediaThenMedia_KeepsAllInOrder(t *testing.T) {
	items := []chatSendRequest{
		mediaItem("/tmp/1.png", "1.png", "first"),
		mediaItem("/tmp/2.png", "2.png", "second"),
	}
	merged := mergeChatSendRequests(items)
	got := mediaPaths(merged)
	want := []string{"/tmp/1.png", "/tmp/2.png"}
	if len(got) != len(want) {
		t.Fatalf("media→media merge attachments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("media→media order = %v, want %v", got, want)
		}
	}
	if merged.Message != "first\nsecond" {
		t.Fatalf("merged text = %q", merged.Message)
	}
}

func TestMergeChatSendRequests_AttachmentOnly_NoText(t *testing.T) {
	items := []chatSendRequest{
		mediaItem("/tmp/only.png", "only.png", ""),
	}
	merged := mergeChatSendRequests(items)
	if got := mediaPaths(merged); len(got) != 1 || got[0] != "/tmp/only.png" {
		t.Fatalf("attachment-only merge lost media: paths=%v", got)
	}
	if merged.Message != "" {
		t.Fatalf("attachment-only merged text = %q, want empty", merged.Message)
	}
}

// A batch with no media at all must leave Media nil (parseMedia stays a no-op) —
// not an empty JSON array — so a downstream media check does not see a phantom
// attachment.
func TestMergeChatSendRequests_NoMedia_LeavesMediaNil(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Message: "one"}},
		{params: chatSendParams{Message: "two"}},
	}
	merged := mergeChatSendRequests(items)
	if merged.Media != nil {
		t.Fatalf("no-media batch must leave Media nil, got %s", string(merged.Media))
	}
	if len(merged.parseMedia()) != 0 {
		t.Fatalf("no-media batch parseMedia = %v, want empty", merged.parseMedia())
	}
}

// End-to-end through the debouncer: a media send followed by a plain-text send in
// the same window flushes as one batch whose merge keeps the media (the busy
// lifecycle path, not just the pure merge function).
func TestChatDebouncer_MediaThenTextFollowup_MergePreservesMedia(t *testing.T) {
	out := make(chan []chatSendRequest, 2)
	d := newChatDebouncer(func(items []chatSendRequest) { out <- items })
	defer d.Stop()

	d.Push("u1:s1", 50*time.Millisecond, mediaItem("/tmp/pic.png", "pic.png", "caption"))
	time.Sleep(10 * time.Millisecond)
	d.Push("u1:s1", 0, chatSendRequest{params: chatSendParams{Message: "and this"}})

	items := waitChatDebounce(t, out)
	merged := mergeChatSendRequests(items)
	if got := mediaPaths(merged); len(got) != 1 || got[0] != "/tmp/pic.png" {
		t.Fatalf("debounced media→text follow-up dropped media: paths=%v", got)
	}
	if merged.Message != "caption\nand this" {
		t.Fatalf("merged text = %q", merged.Message)
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
