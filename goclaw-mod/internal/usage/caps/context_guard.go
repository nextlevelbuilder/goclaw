package caps

import (
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

var contextGuardCounter tokencount.BudgetCounter = tokencount.NewBudgetCounter()

// AgentBudget is the complete request budget configured for one agent.
// Model and provider are intentionally absent: they are not budget authorities.
type AgentBudget struct {
	ContextWindow int
	MaxTokens     int
}

func (b AgentBudget) valid() bool { return b.ContextWindow > 0 && b.MaxTokens > 0 }

// AgentBudgetWiringError means an agent-scoped call reached a model-call gate
// without the configured window and max_tokens that must have been propagated.
type AgentBudgetWiringError struct {
	Purpose string
	Missing string
}

func (e *AgentBudgetWiringError) Error() string {
	return fmt.Sprintf("agent budget wiring error: missing %s (purpose=%s)", e.Missing, e.Purpose)
}

// ContextWindowExceededError is returned before provider transport when a
// complete agent request cannot fit its configured budget.
type ContextWindowExceededError struct {
	Provider      string
	Model         string
	Purpose       string
	InputTokens   int
	OutputReserve int
	ContextWindow int
}

func (e *ContextWindowExceededError) Error() string {
	return fmt.Sprintf(
		"context window exceeded: input=%d + output_reserve=%d > window=%d (provider=%s model=%s purpose=%s)",
		e.InputTokens, e.OutputReserve, e.ContextWindow, e.Provider, e.Model, e.Purpose,
	)
}

func (e *ContextWindowExceededError) ContextBudgetExceeded() bool { return true }

// GuardContextWindow enforces completeInput + agentMaxTokens <= agentWindow.
// Provider/model are retained only for diagnostics.
func GuardContextWindow(req providers.ChatRequest, providerName, model, purpose string, budget AgentBudget) error {
	if !budget.valid() {
		missing := "agent_context_window,agent_max_tokens"
		switch {
		case budget.ContextWindow > 0:
			missing = "agent_max_tokens"
		case budget.MaxTokens > 0:
			missing = "agent_context_window"
		}
		return &AgentBudgetWiringError{Purpose: purpose, Missing: missing}
	}
	input, err := contextGuardCounter.CountRequest(req)
	if err != nil {
		return fmt.Errorf("count complete request: %w", err)
	}
	if input+budget.MaxTokens <= budget.ContextWindow {
		return nil
	}
	slog.Info("caps.context_guard",
		"provider", providerName,
		"model", model,
		"purpose", purpose,
		"input_tokens", input,
		"output_reserve_tokens", budget.MaxTokens,
		"context_window", budget.ContextWindow,
		"action", "abort",
	)
	return &ContextWindowExceededError{
		Provider:      providerName,
		Model:         model,
		Purpose:       purpose,
		InputTokens:   input,
		OutputReserve: budget.MaxTokens,
		ContextWindow: budget.ContextWindow,
	}
}
