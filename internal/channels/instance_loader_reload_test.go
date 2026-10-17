package channels

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// failedStartTestChannel mirrors the Telegram channel's shape for the Reload
// hang regression: it owns a persistent PendingHistory, Start() fails before
// it would have reached StartFlusher(), and Stop() unconditionally calls
// StopFlusher() — exactly what telegram.Channel.Stop does.
type failedStartTestChannel struct {
	base       *BaseChannel
	history    *PendingHistory
	startCalls atomic.Int32
	stopCalls  atomic.Int32
}

func newFailedStartTestChannel(name string) *failedStartTestChannel {
	b := NewBaseChannel(name, bus.New(), nil)
	b.SetType(TypeTelegram)
	return &failedStartTestChannel{
		base:    b,
		history: NewPersistentHistory(name, noopPendingStore{}, uuid.Nil),
	}
}

func (c *failedStartTestChannel) Name() string                                    { return c.base.Name() }
func (c *failedStartTestChannel) Type() string                                    { return c.base.Type() }
func (c *failedStartTestChannel) IsRunning() bool                                 { return c.base.IsRunning() }
func (c *failedStartTestChannel) IsAllowed(senderID string) bool                  { return c.base.IsAllowed(senderID) }
func (c *failedStartTestChannel) Send(context.Context, bus.OutboundMessage) error { return nil }
func (c *failedStartTestChannel) SetType(t string)                                { c.base.SetType(t) }
func (c *failedStartTestChannel) HealthSnapshot() ChannelHealth                   { return c.base.HealthSnapshot() }
func (c *failedStartTestChannel) MarkFailed(summary, detail string, kind ChannelFailureKind, retryable bool) {
	c.base.MarkFailed(summary, detail, kind, retryable)
}

func (c *failedStartTestChannel) Start(context.Context) error {
	c.startCalls.Add(1)
	// Fails before StartFlusher would run — like Telegram's getMe/long-poll
	// setup failing on a dead network.
	return errors.New("validate telegram bot: dial tcp: i/o timeout")
}

func (c *failedStartTestChannel) Stop(context.Context) error {
	c.stopCalls.Add(1)
	c.base.MarkStopped("Stopped")
	c.history.StopFlusher()
	return nil
}

// listAllEnabledStore serves a fixed instance list to Reload.
type listAllEnabledStore struct {
	store.ChannelInstanceStore
	instances []store.ChannelInstanceData
}

func (s *listAllEnabledStore) ListAllEnabled(context.Context) ([]store.ChannelInstanceData, error) {
	return s.instances, nil
}

// TestReload_ChannelWithFailedStartDoesNotBlock: the first Reload loads a
// channel whose Start() fails; the second Reload must still Stop() it and
// finish (log "channel instances reloaded") instead of hanging on the loader
// mutex forever. In production this left every later channel save queued
// behind a Reload that never returned.
func TestReload_ChannelWithFailedStartDoesNotBlock(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	st := &listAllEnabledStore{instances: []store.ChannelInstanceData{{
		BaseModel:   store.BaseModel{ID: uuid.New()},
		Name:        "telegram-dead",
		ChannelType: TypeTelegram,
		Enabled:     true,
	}}}
	loader := NewInstanceLoader(st, nil, mgr, msgBus, nil)

	var channels []*failedStartTestChannel
	loader.RegisterFactory(TypeTelegram, func(name string, _ json.RawMessage, _ json.RawMessage, _ *bus.MessageBus, _ store.PairingStore) (Channel, error) {
		ch := newFailedStartTestChannel(name)
		channels = append(channels, ch)
		return ch, nil
	})

	ctx := context.Background()

	// First reload: channel is registered and its Start fails.
	loader.Reload(ctx)
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel after first reload, got %d", len(channels))
	}
	if got := mgr.GetStatus()["telegram-dead"].(ChannelHealth); got.State != ChannelHealthStateFailed {
		t.Fatalf("expected failed health after start error, got %q", got.State)
	}

	// Second reload (any channel save in the dashboard triggers one): must
	// Stop() the failed channel and come back.
	done := make(chan struct{})
	go func() {
		loader.Reload(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Reload did not return within 5s — Stop() of a channel that never started is blocking the loader mutex")
	}

	if channels[0].stopCalls.Load() != 1 {
		t.Fatalf("expected first channel to be stopped once, got %d", channels[0].stopCalls.Load())
	}
	if len(channels) != 2 {
		t.Fatalf("expected the channel to be re-created on second reload, got %d channels", len(channels))
	}

	// The mutex must be free again for later reloads.
	if names := loader.LoadedNames(); len(names) != 1 {
		t.Fatalf("expected 1 loaded name after reload, got %v", names)
	}
}
