package pipeline

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const maxTruncRetries = 3

// ThinkStage runs per iteration. Calls LLM, handles truncation retries,
// accumulates usage, returns BreakLoop when response has no tool calls.
type ThinkStage struct {
	deps   *PipelineDeps
	result StageResult
}

// NewThinkStage creates a ThinkStage.
func NewThinkStage(deps *PipelineDeps) *ThinkStage {
	return &ThinkStage{deps: deps, result: Continue}
}

func (s *ThinkStage) Name() string       { return "think" }
func (s *ThinkStage) Result() StageResult { return s.result }

// Execute builds tools, calls LLM, handles truncation, sets flow control.
func (s *ThinkStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	// 1. Iteration budget nudges (70% / 90%)
	s.maybeInjectNudge(state)

	// 2. Build filtered tool definitions
	var toolDefs []providers.ToolDefinition
	if s.deps.BuildFilteredTools != nil {
		var err error
		toolDefs, err = s.deps.BuildFilteredTools(state)
		if err != nil {
			return fmt.Errorf("build tools: %w", err)
		}
	}

	// 3. Construct ChatRequest
	req := providers.ChatRequest{
		Messages: state.Messages.All(),
		Tools:    toolDefs,
		Model:    state.Model,
		Options: map[string]any{
			providers.OptMaxTokens: s.deps.Config.MaxTokens,
		},
	}

	// 4. Call LLM (stream or sync — delegated to callback)
	if s.deps.CallLLM == nil {
		return fmt.Errorf("CallLLM callback not configured")
	}
	resp, err := s.deps.CallLLM(ctx, state, req)
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}
	state.Think.LastResponse = resp

	// 5. Accumulate usage (including ThinkingTokens for reasoning models)
	if resp.Usage != nil {
		state.Think.TotalUsage.PromptTokens += resp.Usage.PromptTokens
		state.Think.TotalUsage.CompletionTokens += resp.Usage.CompletionTokens
		state.Think.TotalUsage.TotalTokens += resp.Usage.TotalTokens
		state.Think.TotalUsage.ThinkingTokens += resp.Usage.ThinkingTokens
	}

	// 6. Handle truncation: only retry when tool call args are truncated or malformed.
	// Text-only truncation (no tool calls) is a valid long answer — deliver it.
	truncated := resp.FinishReason == "length" && len(resp.ToolCalls) > 0
	parseErr := !truncated && toolCallsHaveParseErrors(resp.ToolCalls)
	if truncated || parseErr {
		state.Think.TruncRetries++
		if state.Think.TruncRetries >= maxTruncRetries {
			s.result = AbortRun
			return nil
		}
		hint := "[System] Your output was truncated because it exceeded max_tokens. Your tool call arguments were incomplete. Please retry with shorter content — split large writes into multiple smaller calls."
		if parseErr {
			hint = "[System] One or more tool call arguments were malformed (truncated JSON). Please retry with shorter content."
		}
		state.Messages.AppendPending(providers.Message{Role: "assistant", Content: resp.Content})
		state.Messages.AppendPending(providers.Message{Role: "user", Content: hint})
		return nil // Continue to next iteration for retry
	}
	state.Think.TruncRetries = 0 // reset on success

	// 7. Uniquify tool call IDs (OpenAI returns 400 on duplicates across iterations).
	// Skip if raw content present (Anthropic thinking passback) to avoid desync.
	if len(resp.ToolCalls) > 0 && resp.RawAssistantContent == nil && s.deps.UniqueToolCallIDs != nil {
		resp.ToolCalls = s.deps.UniqueToolCallIDs(resp.ToolCalls, state.RunID, state.Iteration)
	}

	// 8. Flow control + message append.
	// Final answer (no tool calls): FinalizeStage builds the definitive assistant
	// message with sanitization + MediaRefs, so skip AppendPending here to avoid
	// a duplicate. Matches v2 behavior where loop breaks before appending.
	if len(resp.ToolCalls) == 0 {
		s.result = BreakLoop
		return nil
	}

	// Tool iteration: append assistant message for LLM context continuity.
	assistantMsg := providers.Message{
		Role:                "assistant",
		Content:             resp.Content,
		Thinking:            resp.Thinking,
		ToolCalls:           resp.ToolCalls,
		Phase:               resp.Phase,               // Codex phase metadata
		RawAssistantContent: resp.RawAssistantContent, // Anthropic thinking blocks passback
	}
	state.Messages.AppendPending(assistantMsg)

	// Emit block.reply for intermediate assistant content during tool iterations.
	// Non-streaming channels (Zalo, Discord, WhatsApp) need this for delivery.
	if resp.Content != "" && s.deps.EmitBlockReply != nil {
		s.deps.EmitBlockReply(resp.Content)
	}

	return nil
}

// maybeInjectNudge injects iteration budget warnings at 70% and 90%.
func (s *ThinkStage) maybeInjectNudge(state *RunState) {
	maxIter := s.deps.Config.MaxIterations
	if maxIter <= 0 {
		return
	}
	pct := float64(state.Iteration) / float64(maxIter)

	if pct >= 0.9 && !state.Evolution.Nudge90Sent {
		state.Evolution.Nudge90Sent = true
		state.Messages.AppendPending(providers.Message{
			Role:    "user",
			Content: "[System] URGENT: You are at 90% of your iteration budget. Wrap up immediately — deliver final results now.",
		})
	} else if pct >= 0.7 && !state.Evolution.Nudge70Sent {
		state.Evolution.Nudge70Sent = true
		state.Messages.AppendPending(providers.Message{
			Role:    "user",
			Content: "[System] You have used 70% of your iteration budget. Start wrapping up your work.",
		})
	}
}

// toolCallsHaveParseErrors returns true if any tool call has a non-empty ParseError,
// indicating the arguments JSON was malformed or truncated by the provider.
func toolCallsHaveParseErrors(calls []providers.ToolCall) bool {
	for _, tc := range calls {
		if tc.ParseError != "" {
			return true
		}
	}
	return false
}
