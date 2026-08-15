package consolidation

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// budgetAgentStore overrides only GetByIDUnscoped; the embedded nil interface
// panics if any other method is called, which keeps the mock honest.
type budgetAgentStore struct {
	store.AgentCRUDStore
	agent *store.AgentData
	err   error
}

func (m *budgetAgentStore) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agent, nil
}

// The whole point of the fix: background workers must carry the agent's
// configured request budget into ctx, or the agent-only preflight guard fails
// closed with AgentBudgetWiringError and silently kills memory consolidation.
func TestWithAgentRequestBudget_WiresConfiguredAgentBudget(t *testing.T) {
	id := uuid.New()
	agents := &budgetAgentStore{agent: &store.AgentData{ContextWindow: 128000, MaxTokens: 4096}}

	ctx := withAgentRequestBudget(context.Background(), agents, id, "episodic-summary")

	if got := store.AgentContextWindowFromContext(ctx); got != 128000 {
		t.Fatalf("context window: want 128000, got %d", got)
	}
	if got := store.AgentMaxTokensFromContext(ctx); got != 4096 {
		t.Fatalf("max tokens: want 4096, got %d", got)
	}
}

func TestWithAgentRequestBudget_NilStoreFallsBackToDefaults(t *testing.T) {
	ctx := withAgentRequestBudget(context.Background(), nil, uuid.New(), "dreaming-synthesis")

	if got := store.AgentContextWindowFromContext(ctx); got != config.DefaultContextWindow {
		t.Fatalf("context window: want default %d, got %d", config.DefaultContextWindow, got)
	}
	if got := store.AgentMaxTokensFromContext(ctx); got != config.DefaultMaxTokens {
		t.Fatalf("max tokens: want default %d, got %d", config.DefaultMaxTokens, got)
	}
}

func TestWithAgentRequestBudget_LookupErrorFallsBackToDefaults(t *testing.T) {
	agents := &budgetAgentStore{err: errors.New("db down")}

	ctx := withAgentRequestBudget(context.Background(), agents, uuid.New(), "episodic-summary")

	if got := store.AgentContextWindowFromContext(ctx); got != config.DefaultContextWindow {
		t.Fatalf("context window: want default %d, got %d", config.DefaultContextWindow, got)
	}
	if got := store.AgentMaxTokensFromContext(ctx); got != config.DefaultMaxTokens {
		t.Fatalf("max tokens: want default %d, got %d", config.DefaultMaxTokens, got)
	}
}

// A zero/partial agent row must not zero out the budget — the setters ignore
// non-positive values, and the helper must supply defaults for the missing half.
func TestWithAgentRequestBudget_ZeroFieldsUseDefaults(t *testing.T) {
	agents := &budgetAgentStore{agent: &store.AgentData{ContextWindow: 0, MaxTokens: 0}}

	ctx := withAgentRequestBudget(context.Background(), agents, uuid.New(), "episodic-summary")

	if got := store.AgentContextWindowFromContext(ctx); got != config.DefaultContextWindow {
		t.Fatalf("context window: want default %d, got %d", config.DefaultContextWindow, got)
	}
	if got := store.AgentMaxTokensFromContext(ctx); got != config.DefaultMaxTokens {
		t.Fatalf("max tokens: want default %d, got %d", config.DefaultMaxTokens, got)
	}
}
