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
			"max_tokens": s.deps.Config.MaxTokens,
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

	// 5. Accumulate usage
	if resp.Usage != nil {
		state.Think.TotalUsage.PromptTokens += resp.Usage.PromptTokens
		state.Think.TotalUsage.CompletionTokens += resp.Usage.CompletionTokens
		state.Think.TotalUsage.TotalTokens += resp.Usage.TotalTokens
	}

	// 6. Handle truncation (finish_reason == "length")
	if resp.FinishReason == "length" {
		state.Think.TruncRetries++
		if state.Think.TruncRetries >= maxTruncRetries {
			s.result = AbortRun
			return nil
		}
		state.Messages.AppendPending(providers.Message{
			Role:    "user",
			Content: "Your response was cut off. Please continue from where you stopped. Be more concise.",
		})
		return nil // Continue to next iteration for retry
	}
	state.Think.TruncRetries = 0 // reset on success

	// 7. Append assistant response to pending messages
	assistantMsg := providers.Message{
		Role:     "assistant",
		Content:  resp.Content,
		Thinking: resp.Thinking,
	}
	if len(resp.ToolCalls) > 0 {
		assistantMsg.ToolCalls = resp.ToolCalls
	}
	state.Messages.AppendPending(assistantMsg)

	// 8. Flow control: no tool calls = final answer -> BreakLoop
	if len(resp.ToolCalls) == 0 {
		s.result = BreakLoop
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
