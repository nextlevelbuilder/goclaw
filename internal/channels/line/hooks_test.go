package line

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
