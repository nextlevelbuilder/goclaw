package line

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/line/line-bot-sdk-go/v7/linebot"
)

// recordingHook is a MessageHook implementation used by the contract tests.
// It increments per-event counters and optionally returns a configured error.
type recordingHook struct {
	name      string
	audioHits int32
	textHits  int32
	pbHits    int32
	err       error
	delay     time.Duration
}

func (r *recordingHook) OnAudio(_ context.Context, _ AudioEvent) error {
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	atomic.AddInt32(&r.audioHits, 1)
	return r.err
}

func (r *recordingHook) OnText(_ context.Context, _ TextEvent) error {
	atomic.AddInt32(&r.textHits, 1)
	return r.err
}

func (r *recordingHook) OnPostback(_ context.Context, _ PostbackEvent) error {
	atomic.AddInt32(&r.pbHits, 1)
	return r.err
}

// waitFor polls a condition with a 1s timeout. Returns true if cond becomes
// true, false on timeout. Used because fan-out goroutines are async.
func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// TestChannel_RegisterHookFanOut verifies that calling RegisterHook with
// multiple hooks results in every hook receiving every event of the
// matching type.
func TestChannel_RegisterHookFanOut(t *testing.T) {
	c := &Channel{}
	h1 := &recordingHook{name: "h1"}
	h2 := &recordingHook{name: "h2"}
	h3 := &recordingHook{name: "h3"}
	c.RegisterHook(h1)
	c.RegisterHook(h2)
	c.RegisterHook(h3)

	c.fanOutAudio(AudioEvent{MessageID: "m1"})
	c.fanOutText(TextEvent{Text: "hello"})
	c.fanOutPostback(PostbackEvent{Data: "action=ping"})

	if !waitFor(func() bool {
		return atomic.LoadInt32(&h1.audioHits) == 1 &&
			atomic.LoadInt32(&h2.audioHits) == 1 &&
			atomic.LoadInt32(&h3.audioHits) == 1 &&
			atomic.LoadInt32(&h1.textHits) == 1 &&
			atomic.LoadInt32(&h2.textHits) == 1 &&
			atomic.LoadInt32(&h3.textHits) == 1 &&
			atomic.LoadInt32(&h1.pbHits) == 1 &&
			atomic.LoadInt32(&h2.pbHits) == 1 &&
			atomic.LoadInt32(&h3.pbHits) == 1
	}) {
		t.Fatalf("expected all hooks to receive all events; got h1{a=%d,t=%d,p=%d} h2{a=%d,t=%d,p=%d} h3{a=%d,t=%d,p=%d}",
			h1.audioHits, h1.textHits, h1.pbHits,
			h2.audioHits, h2.textHits, h2.pbHits,
			h3.audioHits, h3.textHits, h3.pbHits)
	}
}

// TestChannel_HookErrorDoesNotBlockOthers verifies that an error returned by
// one hook does not prevent later hooks from receiving the event.
func TestChannel_HookErrorDoesNotBlockOthers(t *testing.T) {
	c := &Channel{}
	failing := &recordingHook{name: "failing", err: errors.New("boom")}
	healthy := &recordingHook{name: "healthy"}
	c.RegisterHook(failing)
	c.RegisterHook(healthy)

	c.fanOutAudio(AudioEvent{MessageID: "m1"})

	if !waitFor(func() bool {
		return atomic.LoadInt32(&failing.audioHits) == 1 &&
			atomic.LoadInt32(&healthy.audioHits) == 1
	}) {
		t.Fatalf("expected both hooks to receive event; failing=%d healthy=%d",
			failing.audioHits, healthy.audioHits)
	}
}

// TestChannel_NoHooksRegistered_EventsDropped verifies that the channel
// works fine with zero hooks registered (the upstream / non-e-smith use
// case). Fan-out must not panic on a nil/empty slice.
func TestChannel_NoHooksRegistered_EventsDropped(t *testing.T) {
	c := &Channel{}

	// Should not panic.
	c.fanOutAudio(AudioEvent{MessageID: "m1"})
	c.fanOutText(TextEvent{Text: "hi"})
	c.fanOutPostback(PostbackEvent{Data: "x"})

	// Give any rogue goroutines a moment to misbehave.
	time.Sleep(20 * time.Millisecond)

	if len(c.hooks) != 0 {
		t.Fatalf("expected zero hooks, got %d", len(c.hooks))
	}
}

// Compile-time guard: recordingHook must satisfy MessageHook.
var _ MessageHook = (*recordingHook)(nil)

// silenceUnusedSync keeps sync.WaitGroup imported for future test additions
// without triggering an "imported and not used" error if all current tests
// use channels for sync.
var _ sync.WaitGroup

// --- PR #715 test plan: Group / Image / Multi-agent ---

// TestClassifySource_GroupReturnsGroupID exercises the source-type classifier
// that handleEvent uses to decide ChatID/UserID/peerKind. For a group source
// the chat ID surfaced to hooks MUST be the group ID (so replies go to the
// group), the user ID stays the message sender's ID, peerKind is "group".
//
// Covers: PR #715 test plan "Group messages"
// Covers spec: specs/line-adapter-test-plan.md > Requirement: Group message
// webhook handling MUST 有 unit + staging 驗證 > Scenario: Group webhook
// routing (unit)
func TestClassifySource_GroupReturnsGroupID(t *testing.T) {
	src := newGroupSource("U-test-user-123", "G-test-group-456")
	userID, chatID, peerKind := classifySource(src)
	if userID != "U-test-user-123" {
		t.Errorf("userID=%q, want U-test-user-123", userID)
	}
	if chatID != "G-test-group-456" {
		t.Errorf("chatID=%q, want G-test-group-456 (group ID, not user ID)", chatID)
	}
	if peerKind != "group" {
		t.Errorf("peerKind=%q, want group", peerKind)
	}
}

// TestClassifySource_DirectReturnsUserID covers 1:1 chat: chatID == userID,
// peerKind=="direct". Provides the negative against the group case so a
// regression that swaps the cases is caught immediately.
func TestClassifySource_DirectReturnsUserID(t *testing.T) {
	src := newDirectSource("U-test-user-123")
	userID, chatID, peerKind := classifySource(src)
	if userID != "U-test-user-123" {
		t.Errorf("userID=%q, want U-test-user-123", userID)
	}
	if chatID != "U-test-user-123" {
		t.Errorf("chatID=%q, want U-test-user-123 (== userID for direct)", chatID)
	}
	if peerKind != "direct" {
		t.Errorf("peerKind=%q, want direct", peerKind)
	}
}

// TestClassifySource_RoomBehavesLikeGroup covers the LINE Room source type
// (multi-person chat without a stable group). Downstream routing treats it as
// "group" because reply must go to the room, not the sender's DM.
func TestClassifySource_RoomBehavesLikeGroup(t *testing.T) {
	src := newRoomSource("U-test-user-123", "R-test-room-789")
	userID, chatID, peerKind := classifySource(src)
	if chatID != "R-test-room-789" {
		t.Errorf("chatID=%q, want R-test-room-789 (room ID)", chatID)
	}
	if peerKind != "group" {
		t.Errorf("peerKind=%q, want group (room downstream-routes-as group)", peerKind)
	}
	_ = userID
}

// TestClassifySource_NilOrUnknown_ReturnsEmptyPeerKind covers the safety
// fall-through: nil or unrecognised source types return empty peerKind so
// handleEvent can drop the event instead of panic.
func TestClassifySource_NilOrUnknown_ReturnsEmptyPeerKind(t *testing.T) {
	if _, _, pk := classifySource(nil); pk != "" {
		t.Errorf("nil source: peerKind=%q, want empty", pk)
	}
}

// TestImageContentTypeExt_MapsCorrectly covers the file-extension mapping in
// downloadContent: image/png/gif/jpeg lands on the right .ext so downstream
// vision attachment knows how to handle the file. Direct unit test of the
// ext-derivation block; the actual HTTP fetch is exercised by staging
// (see test-evidence/pr715-line-staging.md > ## Image / Media Messages).
//
// Covers: PR #715 test plan "Image/media messages"
// Covers spec: specs/line-adapter-test-plan.md > Requirement: Image / media
// message handling MUST 能下載 binary > Scenario: Image download (unit)
func TestImageContentTypeExt_MapsCorrectly(t *testing.T) {
	cases := []struct {
		contentType string
		wantExt     string
	}{
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/jpeg", ".jpg"}, // default for non-png/gif
		{"", ".jpg"},           // missing content-type → default
		{"application/octet-stream", ".jpg"},
	}
	for _, tc := range cases {
		got := imageExtForContentType(tc.contentType)
		if got != tc.wantExt {
			t.Errorf("contentType=%q: got ext=%q, want %q", tc.contentType, got, tc.wantExt)
		}
	}
}

// TestMultiChannelHookIsolation proves that two Channel instances each maintain
// their own hooks []MessageHook slice — fan-out from channel A reaches only
// hookA, not hookB. This is the unit-test foundation for multi-agent routing:
// each LINE channel instance dispatches to its own configured agent path with
// no cross-contamination.
//
// Covers: PR #715 test plan "Multi-agent routing"
// Covers spec: specs/line-adapter-test-plan.md > Requirement: Multi-agent
// routing MUST 可配置且有 unit 覆蓋 > Scenario: Multi-channel dispatch unit
// test 綠
func TestMultiChannelHookIsolation(t *testing.T) {
	chA := &Channel{}
	chB := &Channel{}
	hookA := &recordingHook{name: "agent-a"}
	hookB := &recordingHook{name: "agent-b"}
	chA.RegisterHook(hookA)
	chB.RegisterHook(hookB)

	// Fan out a TextEvent on each channel. Hook on the OTHER channel must
	// not receive it.
	chA.fanOutText(TextEvent{Text: "to A", UserID: "uA", ChatID: "cA"})
	chB.fanOutText(TextEvent{Text: "to B", UserID: "uB", ChatID: "cB"})

	if !waitFor(func() bool {
		return atomic.LoadInt32(&hookA.textHits) == 1 &&
			atomic.LoadInt32(&hookB.textHits) == 1
	}) {
		t.Fatalf("expected each hook to receive 1 event; hookA=%d hookB=%d",
			hookA.textHits, hookB.textHits)
	}
	// Cross-pollination check: hookA must not have received chB's event.
	// We can't observe the captured payload (recordingHook only counts), but
	// the count==1 invariant after exactly 1 fan-out per channel proves
	// isolation: if hooks bled across channels we'd see count==2 on each.
	if got := atomic.LoadInt32(&hookA.textHits); got != 1 {
		t.Errorf("hookA crossed channels: textHits=%d, want 1", got)
	}
	if got := atomic.LoadInt32(&hookB.textHits); got != 1 {
		t.Errorf("hookB crossed channels: textHits=%d, want 1", got)
	}
}

// --- helpers for source construction in tests ---

func newDirectSource(userID string) *linebot.EventSource {
	return &linebot.EventSource{Type: linebot.EventSourceTypeUser, UserID: userID}
}

func newGroupSource(userID, groupID string) *linebot.EventSource {
	return &linebot.EventSource{Type: linebot.EventSourceTypeGroup, UserID: userID, GroupID: groupID}
}

func newRoomSource(userID, roomID string) *linebot.EventSource {
	return &linebot.EventSource{Type: linebot.EventSourceTypeRoom, UserID: userID, RoomID: roomID}
}
