package pipeline

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// stubEventBus records published events for assertion. Implements the full
// DomainEventBus interface (only Publish is exercised by the pipeline).
type stubEventBus struct {
	mu     sync.Mutex
	events []eventbus.DomainEvent
}

func (s *stubEventBus) Publish(event eventbus.DomainEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}
func (s *stubEventBus) Subscribe(eventType eventbus.EventType, handler eventbus.DomainEventHandler) func() {
	return func() {}
}
func (s *stubEventBus) Start(ctx context.Context)               {}
func (s *stubEventBus) Drain(timeout time.Duration) error      { return nil }

func (s *stubEventBus) publishedBudgetEvents() []eventbus.DomainEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []eventbus.DomainEvent
	for _, e := range s.events {
		if e.Type == eventbus.EventContextBudgetExceeded {
			out = append(out, e)
		}
	}
	return out
}

// TestThinkStage_BudgetExceeded_EmitTelemetry verifies Gap D: when the final
// request cannot be brought under the context budget pre-transport, the run
// emits a context.budget_exceeded event carrying the estimate fields, then
// aborts with the budget error.
func TestThinkStage_BudgetExceeded_EmitTelemetry(t *testing.T) {
	t.Parallel()
	bus := &stubEventBus{}
	// Tiny window: one user message already exceeds the hard input cap, so the
	// reduction ladder (prune -> compact -> shrink) has nothing to change and the
	// run must abort. CompactMessages returns the same history (no reduction),
	// PruneMessages trims nothing — exercising the exhaustion path.
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
			Compaction:    nil,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 600},
		EventBus:     bus,
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			t.Fatal("CallLLM must not run when the request fails the pre-transport budget")
			return nil, nil
		},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{}
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			return msgs, nil // no actual reduction
		},
	}

	stage := NewThinkStage(deps)
	state := defaultState()
	state.Input.SessionKey = "sess-budget"
	state.RunID = "run-budget" // NewRunState copies RunInput.RunID at construction
	state.Input.Message = "hello"
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "a" + strings.Repeat(" long message", 200)}, // ~1200 tokens
	})

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected budget-exceeded abort, got nil error")
	}
	if !strings.Contains(err.Error(), "final request context budget") {
		t.Fatalf("unexpected error: %v", err)
	}

	evs := bus.publishedBudgetEvents()
	if len(evs) != 1 {
		t.Fatalf("published %d budget events, want exactly 1", len(evs))
	}
	p, ok := evs[0].Payload.(*eventbus.ContextBudgetExceededPayload)
	if !ok {
		t.Fatalf("payload type = %T, want *ContextBudgetExceededPayload", evs[0].Payload)
	}
	if p.SessionKey != "sess-budget" || p.RunID != "run-budget" {
		t.Errorf("identifiers = (%q, %q), want (sess-budget, run-budget)", p.SessionKey, p.RunID)
	}
	if p.InputTokens <= 0 || p.HardInputCap <= 0 || p.ContextWindow <= 0 {
		t.Errorf("estimate fields not populated: %+v", p)
	}
	if !p.ReductionExhausted {
		t.Error("ReductionExhausted = false, want true (ladder exhausted before abort)")
	}
}

// TestThinkStage_BudgetExceeded_NoEventBus_NoPanic verifies nil-safe behavior:
// when the pipeline has no event bus wired, budget aborts still return the
// error without panicking (telemetry is best-effort observability).
func TestThinkStage_BudgetExceeded_NoEventBus_NoPanic(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 600},
		// EventBus intentionally nil.
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			t.Fatal("CallLLM must not run")
			return nil, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "a" + strings.Repeat(" long message", 200)},
	})

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected budget-exceeded abort even without event bus")
	}
	if !strings.Contains(err.Error(), "final request context budget") {
		t.Fatalf("unexpected error: %v", err)
	}
}
