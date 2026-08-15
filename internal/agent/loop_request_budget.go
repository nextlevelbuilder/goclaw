package agent

import (
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// RequestBudgetExceededError is returned before provider transport when the
// COMPLETE final request (messages + tools + output reserve) exceeds the
// receiving agent's configured context window. It is intentionally internal
// to the agent loop: callers abort/reduce instead of falling through to model
// fallback or sending an oversized payload.
type RequestBudgetExceededError struct {
	AgentID       string
	Provider      string
	Model         string
	Attempt       string
	InputTokens   int
	OutputReserve int
	ContextWindow int
}

func (e *RequestBudgetExceededError) Error() string {
	return fmt.Sprintf(
		"context budget exceeded: input=%d + output_reserve=%d > agent_window=%d (agent=%s provider=%s model=%s attempt=%s)",
		e.InputTokens, e.OutputReserve, e.ContextWindow, e.AgentID, e.Provider, e.Model, e.Attempt,
	)
}

// ContextBudgetExceeded marks this error as a request-level context-budget
// overflow. Package-neutral consumers (pipeline.ThinkStage) detect it via an
// interface check without importing the agent package, so they can re-enter
// reduction (prune -> compact -> shrink) and rebuild before aborting.
func (e *RequestBudgetExceededError) ContextBudgetExceeded() bool { return true }

// guardCompleteModelRequest is the mandatory final boundary check for EVERY
// model request emitted by an agent loop. Call it only after all messages,
// tools, retry instructions, model overrides and options have been finalized,
// and immediately before provider.Chat/ChatStream.
//
// Hard invariant:
//
//	completeInputTokens + outputReserveTokens <= agent.ContextWindow
//
// The configured agent context window and configured max_tokens are the only
// budget authorities. Model and provider are diagnostic fields only.
func (l *Loop) guardCompleteModelRequest(request providers.ChatRequest, providerName, model, attempt string) error {
	// Bare Loop literals used by unrelated unit tests opt out; NewLoop always
	// wires the fixed local budget counter.
	if l.budgetCounter == nil {
		return nil
	}
	contextWindow := l.resolveEffectiveContextWindow()
	if contextWindow <= 0 {
		return fmt.Errorf("context_window_unresolved: agent=%s provider=%s model=%s attempt=%s", l.id, providerName, model, attempt)
	}

	inputTokens, err := l.budgetCounter.CountRequest(request)
	if err != nil {
		return fmt.Errorf("count complete request: %w", err)
	}
	outputReserve := l.effectiveMaxTokens()

	if inputTokens+outputReserve <= contextWindow {
		slog.Debug("context_budget.guard",
			"agent", l.id,
			"provider", providerName,
			"model", model,
			"attempt", attempt,
			"input_tokens", inputTokens,
			"output_reserve_tokens", outputReserve,
			"context_window", contextWindow,
			"action", "allow")
		return nil
	}

	slog.Info("context_budget.guard",
		"agent", l.id,
		"provider", providerName,
		"model", model,
		"attempt", attempt,
		"input_tokens", inputTokens,
		"output_reserve_tokens", outputReserve,
		"context_window", contextWindow,
		"action", "abort",
		"reason", "context_budget_exceeded")

	return &RequestBudgetExceededError{
		AgentID:       l.id,
		Provider:      providerName,
		Model:         model,
		Attempt:       attempt,
		InputTokens:   inputTokens,
		OutputReserve: outputReserve,
		ContextWindow: contextWindow,
	}
}
