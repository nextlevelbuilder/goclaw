package dingtalk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func newTestChannel(t *testing.T) *Channel {
	t.Helper()
	ch, err := New(Config{ClientID: "k", ClientSecret: "s"}, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ch
}

func TestNew_ValidatesConfig(t *testing.T) {
	if _, err := New(Config{}, bus.New(), nil, nil, nil); err == nil {
		t.Fatal("want error on missing credentials")
	}
	if _, err := New(Config{ClientID: "k", ClientSecret: "s", GroupReplyMode: "bogus"},
		bus.New(), nil, nil, nil); err == nil {
		t.Fatal("want error on bad group_reply_mode")
	}
}

// New must seed BaseChannel.requireMention from the config, or the shared
// mention gate reads false while the config says true.
func TestNew_SeedsRequireMention(t *testing.T) {
	ch := newTestChannel(t)
	if !ch.RequireMention() {
		t.Error("BaseChannel.RequireMention() = false, want true (config default)")
	}

	no := false
	off, err := New(Config{ClientID: "k", ClientSecret: "s", RequireMention: &no},
		bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if off.RequireMention() {
		t.Error("BaseChannel.RequireMention() = true, want false (explicit config)")
	}
}

func TestStartStop_Lifecycle(t *testing.T) {
	ch := newTestChannel(t)
	ctx := context.Background()

	if ch.IsRunning() {
		t.Fatal("running before Start")
	}
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !ch.IsRunning() {
		t.Fatal("not running after Start")
	}
	if err := ch.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if ch.IsRunning() {
		t.Fatal("still running after Stop")
	}
}

// The manager may Stop a channel that already stopped (shutdown races, failed
// Start). Closing stopCh twice would panic, so Stop is guarded by sync.Once.
func TestStop_Idempotent(t *testing.T) {
	ch := newTestChannel(t)
	ctx := context.Background()
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ch.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := ch.Stop(ctx); err != nil {
		t.Fatalf("second Stop panicked or errored: %v", err)
	}
}

// Stop without a preceding Start happens when Start fails partway. It must not
// panic on the unstarted history flusher.
func TestStop_WithoutStart(t *testing.T) {
	ch := newTestChannel(t)
	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
}

// Phase 1 has no transport: Send is a no-op. This test pins that so Phase 4
// replacing it is a deliberate change, not an accident.
func TestSend_NoopUntilPhase4(t *testing.T) {
	ch := newTestChannel(t)
	err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "cid", Content: "hi"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestFactory_AllowListWiredToBaseChannel(t *testing.T) {
	ch, err := Factory("dt", json.RawMessage(validCreds),
		json.RawMessage(`{"allow_from":["staff1"]}`), bus.New(), nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if !ch.IsAllowed("staff1") {
		t.Error("staff1 should be allowed")
	}
	if ch.IsAllowed("intruder") {
		t.Error("intruder should not be allowed")
	}
}
