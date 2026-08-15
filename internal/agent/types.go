package agent

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Agent is the core abstraction for an AI agent execution loop.
// Implemented by *Loop; extracted as an interface for testability and composability.
type Agent interface {
	ID() string
	UUID() uuid.UUID
	OtherConfig() json.RawMessage
	Run(ctx context.Context, req RunRequest) (*RunResult, error)
	IsRunning() bool
	Model() string
	ProviderName() string
	Provider() providers.Provider
}

// BudgetedAgent exposes the configured agent-only request budget without adding
// model/provider authority or expanding the broad Agent test interface.
type BudgetedAgent interface {
	ContextWindow() int
	MaxTokens() int
}

// WithAgentBudget propagates an agent's configured budget to nested model calls.
func WithAgentBudget(ctx context.Context, a Agent) context.Context {
	budgeted, ok := a.(BudgetedAgent)
	if !ok {
		return ctx
	}
	ctx = store.WithAgentContextWindow(ctx, budgeted.ContextWindow())
	return store.WithAgentMaxTokens(ctx, budgeted.MaxTokens())
}
