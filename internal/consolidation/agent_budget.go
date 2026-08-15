package consolidation

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// withAgentRequestBudget wires the calling agent's configured request
// budget (context_window, max_tokens) into ctx so background LLM calls
// (episodic summarization, dreaming synthesis) pass the agent-only preflight
// guard instead of failing closed with an AgentBudgetWiringError.
//
// Background workers run off a session.completed / episodic.created event and
// do not carry a live agent Loop, so the budget cannot come from
// agent.WithAgentBudget. We load it straight from the agent row. If the store
// is unavailable or the row cannot be read, we fall back to the operator
// defaults rather than let a transient lookup miss silently kill memory
// consolidation — the guard's job is to bound requests to the agent window,
// and the defaults are the same window the agent would use unconfigured.
func withAgentRequestBudget(ctx context.Context, agents store.AgentCRUDStore, agentID uuid.UUID, purpose string) context.Context {
	contextWindow := config.DefaultContextWindow
	maxTokens := config.DefaultMaxTokens

	if agents != nil && agentID != uuid.Nil {
		if ag, err := agents.GetByIDUnscoped(ctx, agentID); err != nil {
			slog.Warn("consolidation: agent budget lookup failed, using defaults",
				"purpose", purpose, "agent", agentID, "err", err,
				"context_window", contextWindow, "max_tokens", maxTokens)
		} else {
			if ag.ContextWindow > 0 {
				contextWindow = ag.ContextWindow
			}
			if ag.MaxTokens > 0 {
				maxTokens = ag.MaxTokens
			}
		}
	} else {
		slog.Warn("consolidation: no agent store or agent id, using default budget",
			"purpose", purpose, "agent", agentID,
			"context_window", contextWindow, "max_tokens", maxTokens)
	}

	ctx = store.WithAgentContextWindow(ctx, contextWindow)
	ctx = store.WithAgentMaxTokens(ctx, maxTokens)
	return ctx
}
