package eventbus

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// captureSlog swaps the default slog logger for one that writes to a buffer
// and returns a restore function plus a pointer to the buffer.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	return &buf, func() { slog.SetDefault(prev) }
}

func TestValidateAgentID_EmptyAgentID_NoWarn(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	validateAgentID(DomainEvent{
		Type:     EventRunCompleted,
		SourceID: "r1",
		AgentID:  "",
	})

	if strings.Contains(buf.String(), "non_uuid_agent_id") {
		t.Errorf("empty AgentID should not trigger warning, got log:\n%s", buf.String())
	}
}

func TestValidateAgentID_ValidUUID_NoWarn(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	validateAgentID(DomainEvent{
		Type:     EventEpisodicCreated,
		SourceID: "s1",
		AgentID:  uuid.New().String(),
	})

	if strings.Contains(buf.String(), "non_uuid_agent_id") {
		t.Errorf("valid UUID should not trigger warning, got log:\n%s", buf.String())
	}
}

func TestValidateAgentID_NonUUIDWarns(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	validateAgentID(DomainEvent{
		Type:     EventEpisodicCreated,
		SourceID: "s1",
		AgentID:  "goctech-leader", // agent_key, not UUID
	})

	out := buf.String()
	if !strings.Contains(out, "non_uuid_agent_id") {
		t.Errorf("non-UUID AgentID should emit warning with distinct field, got log:\n%s", out)
	}
	if !strings.Contains(out, "goctech-leader") {
		t.Errorf("warning should include the offending value, got log:\n%s", out)
	}
	if !strings.Contains(out, "eventbus.non_uuid_agent_id") {
		t.Errorf("warning message should be eventbus.non_uuid_agent_id, got log:\n%s", out)
	}
}

func TestValidateAgentID_DistinctFieldName_NoCollision(t *testing.T) {
	// The log field name must NOT be `agent_id` (which downstream
	// observability tooling parses as UUID) — it must be `non_uuid_agent_id`
	// so the warning never collides with valid UUID-typed agent_id fields.
	buf, restore := captureSlog(t)
	defer restore()

	validateAgentID(DomainEvent{
		Type:     EventRunCompleted,
		SourceID: "r1",
		AgentID:  "bad-key",
	})

	out := buf.String()
	if !strings.Contains(out, "non_uuid_agent_id=") {
		t.Errorf("expected field name `non_uuid_agent_id=`, got log:\n%s", out)
	}
}

// TestBusPublish_InvokesValidator verifies the validator is wired into Publish,
// so every publish site is covered without per-caller instrumentation.
func TestBusPublish_InvokesValidator(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	bus := newTestBus()
	ctx := t.Context()
	bus.Start(ctx)
	defer func() { _ = bus.Drain(500 * time.Millisecond) }()

	var received atomic.Int32
	bus.Subscribe(EventRunCompleted, func(_ context.Context, _ DomainEvent) error {
		received.Add(1)
		return nil
	})

	// Publish with non-UUID AgentID — should still dispatch (observability only, not blocking)
	bus.Publish(DomainEvent{
		Type:     EventRunCompleted,
		SourceID: "validator-test",
		AgentID:  "invalid-agent-key",
	})

	// Wait up to 200ms for the worker to dispatch.
	deadline := time.Now().Add(200 * time.Millisecond)
	for received.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}

	if received.Load() != 1 {
		t.Errorf("expected event to still be dispatched (non-blocking), got received=%d", received.Load())
	}
	if !strings.Contains(buf.String(), "non_uuid_agent_id") {
		t.Errorf("expected bus.Publish to invoke validateAgentID, got log:\n%s", buf.String())
	}
}
