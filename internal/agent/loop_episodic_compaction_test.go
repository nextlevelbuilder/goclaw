package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
)

// recordingBus captures every published DomainEvent for assertion.
// Distinct from the eventbus package's own test bus — this one does no
// dispatch, it only records what the emit callback published.
type recordingBus struct {
	mu        sync.Mutex
	published []eventbus.DomainEvent
}

func (r *recordingBus) Publish(event eventbus.DomainEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.published = append(r.published, event)
}
func (r *recordingBus) Subscribe(_ eventbus.EventType, _ eventbus.DomainEventHandler) func() {
	return func() {}
}
func (r *recordingBus) Start(_ context.Context)            {}
func (r *recordingBus) Drain(_ time.Duration) error        { return nil }
func (r *recordingBus) events() []eventbus.DomainEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventbus.DomainEvent, len(r.published))
	copy(out, r.published)
	return out
}

// ---------------------------------------------------------------------------
// V1-A: emit reads the CUMULATIVE session compaction count, not the per-run 0.
// ---------------------------------------------------------------------------

func TestEmitSessionCompleted_UsesCumulativeCount(t *testing.T) {
	bus := &recordingBus{}
	sessions := &nopSessionStore{
		compactionCount: 5, // cumulative session count
		summary:         "prior-cycle summary",
	}
	loop := &Loop{
		id:        "test-agent",
		agentUUID: uuid.New(),
		tenantID:  uuid.New(),
		domainBus: bus,
		sessions:  sessions,
	}

	loop.emitSessionCompleted(context.Background(), "sess-1", "user-1", 42, 9000)

	evs := bus.events()
	if len(evs) != 1 {
		t.Fatalf("published %d events, want 1", len(evs))
	}
	ev := evs[0]
	// Bug A: payload.CompactionCount (the field the worker uses for idempotency)
	// must be the cumulative count (5), not the per-run 0.
	pl, ok := ev.Payload.(*eventbus.SessionCompletedPayload)
	if !ok {
		t.Fatalf("payload type = %T, want *SessionCompletedPayload", ev.Payload)
	}
	if pl.CompactionCount != 5 {
		t.Errorf("payload.CompactionCount = %d, want 5 (cumulative)", pl.CompactionCount)
	}
	// Bug C: SourceID embeds the cumulative count.
	if ev.SourceID != "sess-1:5" {
		t.Errorf("SourceID = %q, want %q", ev.SourceID, "sess-1:5")
	}
	// Summary from the previous cycle is attached when count > 0.
	if pl.Summary != "prior-cycle summary" {
		t.Errorf("payload.Summary = %q, want prior-cycle summary", pl.Summary)
	}
	if pl.MessageCount != 42 || pl.TokensUsed != 9000 {
		t.Errorf("msgCount/tokens = %d/%d, want 42/9000", pl.MessageCount, pl.TokensUsed)
	}
}

func TestEmitSessionCompleted_ZeroCountOmitsSummary(t *testing.T) {
	bus := &recordingBus{}
	// count == 0 → no prior cycle → summary must NOT be fetched/attached
	// (worker would otherwise get a stale/empty summary and skip the LLM path).
	sessions := &nopSessionStore{
		compactionCount: 0,
		summary:         "should-not-be-attached",
	}
	loop := &Loop{
		id:        "test-agent",
		agentUUID: uuid.New(),
		tenantID:  uuid.New(),
		domainBus: bus,
		sessions:  sessions,
	}

	loop.emitSessionCompleted(context.Background(), "sess-1", "user-1", 1, 100)

	evs := bus.events()
	if len(evs) != 1 {
		t.Fatalf("published %d events, want 1", len(evs))
	}
	pl := evs[0].Payload.(*eventbus.SessionCompletedPayload)
	if pl.CompactionCount != 0 {
		t.Errorf("payload.CompactionCount = %d, want 0", pl.CompactionCount)
	}
	if pl.Summary != "" {
		t.Errorf("payload.Summary = %q, want empty (count==0)", pl.Summary)
	}
	if evs[0].SourceID != "sess-1:0" {
		t.Errorf("SourceID = %q, want sess-1:0", evs[0].SourceID)
	}
}

func TestEmitSessionCompleted_NoBusIsNoop(t *testing.T) {
	loop := &Loop{
		id:        "test-agent",
		agentUUID: uuid.New(),
		tenantID:  uuid.New(),
		domainBus: nil, // no bus
		sessions:  &nopSessionStore{compactionCount: 3},
	}
	// Must not panic when domainBus is nil.
	loop.emitSessionCompleted(context.Background(), "sess-1", "user-1", 1, 100)
}

// ---------------------------------------------------------------------------
// V1-C: eventbus dedup — distinct counts both pass, same count deduped.
// Uses the REAL bus + real dedupSet (not the recording double) so we exercise
// the actual dedup key = Type + ":" + SourceID.
// ---------------------------------------------------------------------------

func TestEventbusDedup_DistinctCompactionCountsPass(t *testing.T) {
	bus := eventbus.NewDomainEventBus(eventbus.Config{
		QueueSize:     100,
		WorkerCount:   2,
		RetryAttempts: 1,
		RetryDelay:    time.Millisecond,
		DedupTTL:      time.Minute,
	})
	bus.Start(context.Background())
	defer func() { _ = bus.Drain(time.Second) }()

	var mu sync.Mutex
	var seen []string
	bus.Subscribe(eventbus.EventSessionCompleted, func(_ context.Context, e eventbus.DomainEvent) error {
		mu.Lock()
		seen = append(seen, e.SourceID)
		mu.Unlock()
		return nil
	})

	// Same session, three DIFFERENT compaction cycles → all three must pass dedup.
	bus.Publish(eventbus.DomainEvent{Type: eventbus.EventSessionCompleted, SourceID: "sess-1:1"})
	bus.Publish(eventbus.DomainEvent{Type: eventbus.EventSessionCompleted, SourceID: "sess-1:2"})
	bus.Publish(eventbus.DomainEvent{Type: eventbus.EventSessionCompleted, SourceID: "sess-1:3"})
	// Rapid duplicate of cycle 2 within TTL → must be deduped.
	bus.Publish(eventbus.DomainEvent{Type: eventbus.EventSessionCompleted, SourceID: "sess-1:2"})

	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	got := len(seen)
	mu.Unlock()
	if got != 3 {
		t.Errorf("handler saw %d events, want 3 (cycles 1,2,3; duplicate of 2 deduped)", got)
	}
}

func TestEventbusDedup_SameCompactionCountDeduped(t *testing.T) {
	bus := eventbus.NewDomainEventBus(eventbus.Config{
		QueueSize:     100,
		WorkerCount:   2,
		RetryAttempts: 1,
		RetryDelay:    time.Millisecond,
		DedupTTL:      time.Minute,
	})
	bus.Start(context.Background())
	defer func() { _ = bus.Drain(time.Second) }()

	var count int
	var mu sync.Mutex
	bus.Subscribe(eventbus.EventSessionCompleted, func(_ context.Context, _ eventbus.DomainEvent) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	})

	// Same SourceID (same session, same cycle) published 3× → only 1 passes.
	for range 3 {
		bus.Publish(eventbus.DomainEvent{Type: eventbus.EventSessionCompleted, SourceID: "sess-1:7"})
	}

	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	got := count
	mu.Unlock()
	if got != 1 {
		t.Errorf("handler called %d times, want 1 (same cycle deduped)", got)
	}
}
