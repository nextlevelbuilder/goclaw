package pipeline

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// PruneStage budgets with the request guard's BudgetCounter so both stages agree;
// TokenCounter falls back to a chars/2 heuristic for unregistered models.
func budgetMessageTokens(deps *PipelineDeps, model string, msgs []providers.Message) int {
	if deps.BudgetCounter != nil {
		n, err := deps.BudgetCounter.CountMessages(msgs)
		if err == nil {
			return n
		}
		slog.Warn("budget_counter.count_failed", "target", "messages", "error", err)
	}
	if deps.TokenCounter == nil {
		return 0
	}
	return deps.TokenCounter.CountMessages(model, msgs)
}

func budgetOverheadTokens(deps *PipelineDeps, model string, system providers.Message, tools []providers.ToolDefinition) (int, bool) {
	if deps.BudgetCounter != nil {
		msgTokens, err := deps.BudgetCounter.CountMessages([]providers.Message{system})
		if err == nil {
			var toolTokens int
			if toolTokens, err = deps.BudgetCounter.CountToolSchemas(tools); err == nil {
				return msgTokens + toolTokens, true
			}
		}
		slog.Warn("budget_counter.count_failed", "target", "overhead", "error", err)
	}
	if deps.TokenCounter == nil {
		return 0, false
	}
	return deps.TokenCounter.CountMessages(model, []providers.Message{system}) +
		deps.TokenCounter.CountToolSchemas(model, tools), true
}
