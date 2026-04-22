package channels

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// timeoutTestChannel is a minimal Channel implementation used by the Reload
// timeout regression tests. It intentionally does NOT embed *BaseChannel so
// optional interfaces (SetAgentID, SetTenantID, ...) aren't promoted — that
// keeps loadInstance on the "no agent store needed" path.
type timeoutTestChannel struct {
	base        *BaseChannel
	release     chan struct{}
	hang        bool
	captureCtx  func(context.Context)
	startReturn chan error
	stopCalls   atomic.Int32
}

func newTimeoutTestChannel(name, channelType string, hang bool) *timeoutTestChannel {
	b := NewBaseChannel(name, bus.New(), nil)
	b.SetType(channelType)
	return &timeoutTestChannel{
		base:        b,
		release:     make(chan struct{}),
		hang:        hang,
		startReturn: make(chan error, 1),
	}
}

func (c *timeoutTestChannel) Name() string                                    { return c.base.Name() }
func (c *timeoutTestChannel) Type() string                                    { return c.base.Type() }
func (c *timeoutTestChannel) IsRunning() bool                                 { return c.base.IsRunning() }
func (c *timeoutTestChannel) IsAllowed(senderID string) bool                  { return c.base.IsAllowed(senderID) }
func (c *timeoutTestChannel) Send(context.Context, bus.OutboundMessage) error { return nil }
func (c *timeoutTestChannel) SetType(t string)                                { c.base.SetType(t) }
func (c *timeoutTestChannel) HealthSnapshot() ChannelHealth                   { return c.base.HealthSnapshot() }

func (c *timeoutTestChannel) Start(ctx context.Context) error {
	if c.captureCtx != nil {
		c.captureCtx(ctx)
	}
	if !c.hang {
		c.base.MarkHealthy("Connected")
		return nil
	}
	// Hang until released or ctx done (covers both honor-ctx and ignore-ctx cases
	// — whichever fires first).
	select {
	case <-ctx.Done():
		c.startReturn <- ctx.Err()
		return ctx.Err()
	case <-c.release:
		c.startReturn <- nil
		return nil
	}
}

func (c *timeoutTestChannel) Stop(context.Context) error {
	c.stopCalls.Add(1)
	c.base.MarkStopped("Stopped")
	return nil
}

// MarkFailed delegates to the embedded base so recordChannelStartFailureLocked
// can transition the channel from "stopped" → "failed" after the timeout cleanup
// (real channels embed *BaseChannel and inherit this for free).
func (c *timeoutTestChannel) MarkFailed(summary, detail string, kind ChannelFailureKind, retryable bool) {
	c.base.MarkFailed(summary, detail, kind, retryable)
}

// withShortReloadTimeout shrinks the package-level timeout for the duration
// of a test so we don't wait the real 90s.
func withShortReloadTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := reloadStartTimeout
	reloadStartTimeout = d
	t.Cleanup(func() { reloadStartTimeout = orig })
}

// TestLoadInstance_HungStartDoesNotBlock verifies that a Start() that never
// returns is abandoned after reloadStartTimeout so Reload() can proceed to
// other channels instead of deadlocking on the loader mutex.
func TestLoadInstance_HungStartDoesNotBlock(t *testing.T) {
	withShortReloadTimeout(t, 200*time.Millisecond)

	msgBus := bus.New()
	mgr := NewManager(msgBus)
	loader := NewInstanceLoader(nil, nil, mgr, msgBus, nil)

	hung := newTimeoutTestChannel("telegram-hung", TypeTelegram, true)
	loader.RegisterFactory(TypeTelegram, func(string, json.RawMessage, json.RawMessage, *bus.MessageBus, store.PairingStore) (Channel, error) {
		return hung, nil
	})

	done := make(chan error, 1)
	start := time.Now()
	loader.mu.Lock()
	go func() {
		defer loader.mu.Unlock()
		done <- loader.loadInstance(context.Background(), store.ChannelInstanceData{
			Name:        "telegram-hung",
			ChannelType: TypeTelegram,
		}, true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loadInstance returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("loadInstance did not return within 3s — timeout path is broken")
	}

	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("returned too fast (%s); timeout should have waited", elapsed)
	}

	status := mgr.GetStatus()["telegram-hung"].(ChannelHealth)
	if status.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed state, got %q", status.State)
	}
	if hung.stopCalls.Load() == 0 {
		t.Fatal("expected Stop() to be called on timed-out channel for cleanup")
	}

	// Drain the start goroutine so it doesn't leak past the test.
	close(hung.release)
	select {
	case <-hung.startReturn:
	case <-time.After(time.Second):
		t.Fatal("start goroutine did not drain after release")
	}
}

// TestLoadInstance_FastStartPreservesCallerContext verifies the critical fix:
// channels derive long-running goroutines from the ctx they receive in Start().
// We must pass the caller's ctx through unchanged, not a timeout-wrapped ctx,
// otherwise a successful Start() has its long-lived work cancelled when the
// timeout fires or cancel runs — silently breaking polling after Reload.
func TestLoadInstance_FastStartPreservesCallerContext(t *testing.T) {
	withShortReloadTimeout(t, 200*time.Millisecond)

	msgBus := bus.New()
	mgr := NewManager(msgBus)
	loader := NewInstanceLoader(nil, nil, mgr, msgBus, nil)

	var receivedCtx context.Context
	ch := newTimeoutTestChannel("telegram-fast", TypeTelegram, false)
	ch.captureCtx = func(ctx context.Context) { receivedCtx = ctx }
	loader.RegisterFactory(TypeTelegram, func(string, json.RawMessage, json.RawMessage, *bus.MessageBus, store.PairingStore) (Channel, error) {
		return ch, nil
	})

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	loader.mu.Lock()
	err := loader.loadInstance(parent, store.ChannelInstanceData{
		Name:        "telegram-fast",
		ChannelType: TypeTelegram,
	}, true)
	loader.mu.Unlock()
	if err != nil {
		t.Fatalf("loadInstance error: %v", err)
	}

	if receivedCtx == nil {
		t.Fatal("Start did not receive a ctx")
	}
	// Wait past the timeout window. If Start had been handed a timeout ctx,
	// receivedCtx.Done() would be closed by now. It MUST still be open,
	// otherwise long-running polling goroutines (Telegram) would die.
	time.Sleep(300 * time.Millisecond)
	select {
	case <-receivedCtx.Done():
		t.Fatal("ctx passed to Start was cancelled after timeout window — would kill derived polling goroutines")
	default:
	}

	// Cancelling the parent ctx must still propagate (caller retains control).
	cancelParent()
	select {
	case <-receivedCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancel did not propagate to Start ctx")
	}
}
