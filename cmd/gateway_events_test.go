package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// captureSlog redirects the default slog logger to an in-memory JSON buffer for
// the duration of the test and restores the previous default on cleanup.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// fillInboundBuffer pushes messages until TryPublishInbound reports the inbound
// buffer is full, guaranteeing the next leader-mode inject inside the drain
// callback is rejected. Bounded so a mis-sized buffer cannot spin forever.
func fillInboundBuffer(t *testing.T, mb *bus.MessageBus) {
	t.Helper()
	for i := 0; i < 100000; i++ {
		if !mb.TryPublishInbound(bus.InboundMessage{Content: "filler"}) {
			return
		}
	}
	t.Fatal("inbound buffer never reported full")
}

// Leader-mode drain when TryPublishInbound is rejected (buffer full): must not
// panic and must surface a warning carrying the full routing tuple + batch size.
func TestDrainTeamProgressNotify_LeaderInjectDroppedLogsWarning(t *testing.T) {
	buf := captureSlog(t)

	mb := bus.New()
	defer mb.Close()
	fillInboundBuffer(t, mb)

	d := &gatewayDeps{msgBus: mb}
	tenant := uuid.New()
	meta := tools.NotifyRoutingMeta{
		TenantID:  tenant,
		TeamID:    "team-abc",
		Mode:      "leader",
		Channel:   "telegram",
		ChatID:    "chat-123",
		UserID:    "user-9",
		LeadAgent: "lead-key",
		PeerKind:  "group",
		LocalKey:  "chat-123:topic-7",
	}

	// Must not panic even though the inbound bus rejects the injection.
	d.drainTeamProgressNotify([]string{"line-1", "line-2"}, meta)

	// Exactly one warning must be emitted with the full routing tuple.
	var found map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		if rec["msg"] == "team progress leader inject dropped" {
			found = rec
			break
		}
	}
	if found == nil {
		t.Fatalf("expected leader-inject-dropped warning, got logs:\n%s", buf.String())
	}
	if found["level"] != "WARN" {
		t.Fatalf("warning level = %v, want WARN", found["level"])
	}
	checks := map[string]any{
		"tenant_id":  tenant.String(),
		"team_id":    "team-abc",
		"lead_agent": "lead-key",
		"channel":    "telegram",
		"peer_kind":  "group",
		"chat_id":    "chat-123",
		"local_key":  "chat-123:topic-7",
		"batch_size": float64(2), // JSON numbers decode as float64
	}
	for k, want := range checks {
		if got := found[k]; got != want {
			t.Fatalf("warning field %q = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

// Direct-mode drain publishes the batched content outbound with the routing
// tuple carried through — and never touches the inbound leader path.
func TestDrainTeamProgressNotify_DirectModePublishesOutbound(t *testing.T) {
	mb := bus.New()
	defer mb.Close()

	d := &gatewayDeps{msgBus: mb}
	meta := tools.NotifyRoutingMeta{
		TeamID:   "team-xyz",
		Mode:     "direct",
		Channel:  "discord",
		ChatID:   "chat-777",
		LocalKey: "chat-777",
	}

	d.drainTeamProgressNotify([]string{"alpha", "beta"}, meta)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected an outbound message from direct-mode drain")
	}
	if out.Channel != "discord" || out.ChatID != "chat-777" {
		t.Fatalf("outbound routing wrong: %+v", out)
	}
	if out.Content != tools.FormatBatchedNotify([]string{"alpha", "beta"}) {
		t.Fatalf("outbound content = %q", out.Content)
	}
}
