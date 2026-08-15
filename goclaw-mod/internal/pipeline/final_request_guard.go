package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// FinalRequestEstimate describes the complete pre-call context budget for the
// request that will actually be sent to the model.
type FinalRequestEstimate struct {
	MessageTokens       int
	ToolTokens          int
	InputTokens         int
	OutputReserveTokens int
	HardInputCapTokens  int
	CompactTargetTokens int
	ContextWindow       int
	MaxRequestShare     float64
}

const defaultMaxRequestShare = 0.85

func effectiveMaxRequestShare(cfg *config.CompactionConfig) float64 {
	if cfg != nil && cfg.MaxRequestShare > 0 && cfg.MaxRequestShare <= 1 {
		return cfg.MaxRequestShare
	}
	return defaultMaxRequestShare
}

func (s *ThinkStage) buildChatRequest(state *RunState, toolDefs []providers.ToolDefinition) providers.ChatRequest {
	options := map[string]any{
		providers.OptMaxTokens: s.deps.Config.MaxTokens,
	}
	return providers.ChatRequest{
		Messages: state.Messages.All(),
		Tools:    toolDefs,
		Model:    state.Model,
		Options:  options,
	}
}

func (s *ThinkStage) finalRequestEstimate(state *RunState, req providers.ChatRequest) (FinalRequestEstimate, error) {
	contextWindow := state.Context.EffectiveContextWindow
	if contextWindow == 0 {
		contextWindow = s.deps.Config.ContextWindow
	}
	if contextWindow <= 0 {
		return FinalRequestEstimate{}, nil
	}

	messageTokens, toolTokens, err := countBudgetInput(s.deps, state.Model, req)
	if err != nil {
		return FinalRequestEstimate{}, err
	}
	outputReserve := s.deps.Config.MaxTokens
	if outputReserve < 0 {
		outputReserve = 0
	}
	share := effectiveMaxRequestShare(s.deps.Config.Compaction)
	inputTokens := messageTokens + toolTokens
	hardInputCap := contextWindow - outputReserve
	shareTarget := int(float64(contextWindow)*share) - outputReserve
	compactTarget := min(hardInputCap, shareTarget)
	return FinalRequestEstimate{
		MessageTokens:       messageTokens,
		ToolTokens:          toolTokens,
		InputTokens:         inputTokens,
		OutputReserveTokens: outputReserve,
		HardInputCapTokens:  hardInputCap,
		CompactTargetTokens: compactTarget,
		ContextWindow:       contextWindow,
		MaxRequestShare:     share,
	}, nil
}

func countBudgetInput(deps *PipelineDeps, model string, req providers.ChatRequest) (int, int, error) {
	if deps != nil && deps.BudgetCounter != nil {
		messages, err := deps.BudgetCounter.CountMessages(req.Messages)
		if err != nil {
			return 0, 0, fmt.Errorf("count request messages: %w", err)
		}
		tools, err := deps.BudgetCounter.CountToolSchemas(req.Tools)
		if err != nil {
			return 0, 0, fmt.Errorf("count request tools: %w", err)
		}
		return messages, tools, nil
	}
	// Isolated pipeline tests may still wire only the legacy counter. Runtime
	// always provides BudgetCounter.
	return countRequestMessages(deps, model, req.Messages), countRequestTools(deps, model, req.Tools), nil
}

func countRequestMessages(deps *PipelineDeps, model string, messages []providers.Message) int {
	if deps != nil && deps.TokenCounter != nil {
		return deps.TokenCounter.CountMessages(model, messages)
	}
	total := 0
	for _, msg := range messages {
		total += utf8.RuneCountInString(msg.Content)/3 + 4
		for _, tc := range msg.ToolCalls {
			total += utf8.RuneCountInString(tc.ID)/3 + utf8.RuneCountInString(tc.Name)/3
		}
	}
	return total
}

func countRequestTools(deps *PipelineDeps, model string, tools []providers.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	if deps != nil && deps.TokenCounter != nil {
		return deps.TokenCounter.CountToolSchemas(model, tools)
	}
	blob, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return utf8.RuneCountInString(string(blob)) / 3
}

func (e FinalRequestEstimate) withinLimit() bool {
	return e.ContextWindow > 0 &&
		e.HardInputCapTokens > 0 &&
		e.CompactTargetTokens > 0 &&
		e.InputTokens <= e.HardInputCapTokens &&
		e.InputTokens <= e.CompactTargetTokens
}

func (s *ThinkStage) prepareFinalRequest(ctx context.Context, state *RunState, toolDefs []providers.ToolDefinition) (providers.ChatRequest, FinalRequestEstimate, error) {
	req := s.buildChatRequest(state, toolDefs)
	estimate, err := s.finalRequestEstimate(state, req)
	if err != nil {
		return req, estimate, err
	}
	if estimate.ContextWindow <= 0 {
		// A nil counter means this lightweight pipeline instance did not opt into
		// request budgeting (primarily isolated stage tests). Runtime wiring always
		// provides a counter, so an unresolved runtime window still fails closed.
		if s.deps.TokenCounter == nil {
			return req, estimate, nil
		}
		return req, estimate, fmt.Errorf("context_window_unresolved: no configured agent context window")
	}
	if estimate.HardInputCapTokens <= 0 || estimate.CompactTargetTokens <= 0 {
		s.logFinalRequestGuard(state, estimate, "abort", "invalid_budget")
		return req, estimate, fmt.Errorf("final request context budget unavailable: context_window=%d output_reserve=%d hard_input_cap=%d compact_target=%d",
			estimate.ContextWindow, estimate.OutputReserveTokens, estimate.HardInputCapTokens, estimate.CompactTargetTokens)
	}
	if estimate.withinLimit() {
		s.logFinalRequestGuard(state, estimate, "allow", "initial")
		return req, estimate, nil
	}

	s.logFinalRequestGuard(state, estimate, "reduce", "initial")
	steps := []string{"prune_history", "compact_history", "shrink_memory"}
	for _, step := range steps {
		changed, err := s.reduceFinalRequestContext(ctx, state, estimate, step)
		if err != nil {
			return req, estimate, err
		}
		if !changed {
			continue
		}
		req = s.buildChatRequest(state, toolDefs)
		estimate, err = s.finalRequestEstimate(state, req)
		if err != nil {
			return req, estimate, err
		}
		if estimate.withinLimit() {
			s.logFinalRequestGuard(state, estimate, "allow", step)
			return req, estimate, nil
		}
		s.logFinalRequestGuard(state, estimate, "reduce", step)
	}

	s.logFinalRequestGuard(state, estimate, "abort", "exhausted")
	return req, estimate, fmt.Errorf("final request context budget exceeded: estimated_input=%d compact_target=%d hard_input_cap=%d context_window=%d max_request_share=%.2f",
		estimate.InputTokens, estimate.CompactTargetTokens, estimate.HardInputCapTokens, estimate.ContextWindow, estimate.MaxRequestShare)
}

func (s *ThinkStage) reduceFinalRequestContext(ctx context.Context, state *RunState, estimate FinalRequestEstimate, step string) (bool, error) {
	switch step {
	case "prune_history":
		return s.pruneForFinalRequestBudget(state, estimate), nil
	case "compact_history":
		return s.compactForFinalRequestBudget(ctx, state)
	case "shrink_memory":
		return s.shrinkMemoryForFinalRequestBudget(state), nil
	default:
		return false, nil
	}
}

func (s *ThinkStage) pruneForFinalRequestBudget(state *RunState, estimate FinalRequestEstimate) bool {
	if s.deps.PruneMessages == nil {
		return false
	}
	history := state.Messages.History()
	if len(history) == 0 {
		return false
	}
	fixedMessages := []providers.Message{state.Messages.System()}
	fixedMessages = append(fixedMessages, state.Messages.Pending()...)
	fixedMessageTokens := countRequestMessages(s.deps, state.Model, fixedMessages)
	budget := estimate.CompactTargetTokens - estimate.ToolTokens - fixedMessageTokens
	if budget <= 0 {
		budget = 1
	}
	pruned, stats := s.deps.PruneMessages(history, budget)
	changed := stats.ResultsTrimmed > 0 || stats.ResultsCleared > 0 || stats.Compacted || len(pruned) != len(history)
	if !changed {
		return false
	}
	if s.deps.SanitizeHistory != nil {
		pruned, _ = s.deps.SanitizeHistory(pruned)
	}
	state.Messages.SetHistory(pruned)
	return true
}

func (s *ThinkStage) compactForFinalRequestBudget(ctx context.Context, state *RunState) (bool, error) {
	if s.deps.CompactMessages == nil {
		return false, nil
	}
	history := state.Messages.History()
	if len(history) == 0 {
		return false, nil
	}
	savedPending := state.Messages.Pending()
	compacted, err := s.deps.CompactMessages(ctx, history, state.Model)
	if err != nil {
		return false, fmt.Errorf("compact final request context: %w", err)
	}
	state.Messages.ReplaceHistory(compacted)
	for _, msg := range savedPending {
		state.Messages.AppendPending(msg)
	}
	state.Prune.MidLoopCompacted = true
	state.Compact.CompactionCount++
	return true, nil
}

func (s *ThinkStage) shrinkMemoryForFinalRequestBudget(state *RunState) bool {
	section := strings.TrimSpace(state.Context.MemorySection)
	if section == "" {
		return false
	}
	sys := state.Messages.System()
	content := sys.Content
	candidates := []string{"\n\n" + state.Context.MemorySection, state.Context.MemorySection, section}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if strings.Contains(content, candidate) {
			sys.Content = strings.Replace(content, candidate, "", 1)
			state.Messages.SetSystem(sys)
			state.Context.MemorySection = ""
			return true
		}
	}
	return false
}

func (s *ThinkStage) logFinalRequestGuard(state *RunState, estimate FinalRequestEstimate, action, step string) {
	if estimate.ContextWindow <= 0 {
		return
	}
	slog.Info("final_context.guard",
		"session_key", state.Input.SessionKey,
		"run_id", state.RunID,
		"model", state.Model,
		"context_window", estimate.ContextWindow,
		"max_request_share", estimate.MaxRequestShare,
		"hard_input_cap_tokens", estimate.HardInputCapTokens,
		"compact_target_input_tokens", estimate.CompactTargetTokens,
		"message_tokens", estimate.MessageTokens,
		"tool_tokens", estimate.ToolTokens,
		"input_tokens", estimate.InputTokens,
		"output_reserve_tokens", estimate.OutputReserveTokens,
		"action", action,
		"reduction_step", step,
	)
}
