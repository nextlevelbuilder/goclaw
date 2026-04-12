package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const maxTruncRetries = 3

// ThinkStage runs per iteration. Calls LLM, handles recovery/truncation,
// accumulates usage, returns BreakLoop when response has no tool calls.
type ThinkStage struct {
	deps              *PipelineDeps
	result            StageResult
	reactiveCompactor *ReactiveCompactor
	recovery          *RecoveryManager
}

// NewThinkStage creates a ThinkStage.
func NewThinkStage(deps *PipelineDeps) *ThinkStage {
	return &ThinkStage{deps: deps, result: Continue}
}

func (s *ThinkStage) Name() string        { return "think" }
func (s *ThinkStage) Result() StageResult { return s.result }

// Execute builds tools, calls LLM, applies context defense/recovery, and sets flow control.
func (s *ThinkStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue
	state.Think.LastError = nil
	state.Think.LastErrorIsAPI = false

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

	// 3. Configure optional streaming tool execution before the LLM call starts.
	s.prepareStreamingExecutor(ctx, state)

	// 4. Call LLM with context defense + escalating recovery.
	resp, err := s.callWithRecovery(ctx, state, toolDefs)
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
		return nil
	}
	state.Think.TruncRetries = 0 // reset on success

	// 7. Uniquify tool call IDs unless the calls were already streamed and executed.
	// Skip when raw content is present (Anthropic thinking passback) to avoid desync.
	if len(resp.ToolCalls) > 0 &&
		resp.RawAssistantContent == nil &&
		s.deps.UniqueToolCallIDs != nil &&
		(state.Tool.StreamExecutor == nil || len(state.Tool.StreamedToolIDs) == 0) {
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
		Phase:               resp.Phase,
		RawAssistantContent: resp.RawAssistantContent,
	}
	state.Messages.AppendPending(assistantMsg)

	// Emit block.reply for intermediate assistant content during tool iterations.
	// Non-streaming channels (Zalo, Discord, WhatsApp) need this for delivery.
	if resp.Content != "" && s.deps.EmitBlockReply != nil {
		s.deps.EmitBlockReply(resp.Content)
	}

	return nil
}

func (s *ThinkStage) callWithRecovery(
	ctx context.Context,
	state *RunState,
	toolDefs []providers.ToolDefinition,
) (*providers.ChatResponse, error) {
	if s.deps.CallLLM == nil {
		return nil, fmt.Errorf("CallLLM callback not configured")
	}

	if s.recovery == nil && s.deps.Config.Recovery != nil {
		s.recovery = NewRecoveryManager(*s.deps.Config.Recovery)
	}
	if s.reactiveCompactor == nil && s.deps.CompactMessages != nil {
		s.reactiveCompactor = NewReactiveCompactor(s.deps.CompactMessages)
	}

	model := state.Model
	maxTokens := s.deps.Config.MaxTokens

	for {
		req := providers.ChatRequest{
			Messages: s.requestMessages(state),
			Tools:    toolDefs,
			Model:    model,
			Options: map[string]any{
				providers.OptMaxTokens: maxTokens,
			},
		}

		resp, err := func() (_ *providers.ChatResponse, err error) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("pipeline: CallLLM panic recovered",
						"panic", r,
						"model", model,
						"session_key", state.Input.SessionKey,
						"run_id", state.Input.RunID,
						"stack", string(debug.Stack()))
					err = fmt.Errorf("CallLLM panic: %v", r)
				}
			}()
			return s.deps.CallLLM(ctx, state, req)
		}()
		if err == nil {
			if s.recovery != nil {
				s.recovery.RecordSuccess()
			}
			state.Think.LastError = nil
			state.Think.LastErrorIsAPI = false
			return resp, nil
		}

		state.Think.LastError = err
		state.Think.LastErrorIsAPI = IsPromptTooLongError(err) || isRetryableAPIError(err) || isMaxTokensError(err)

		if s.reactiveCompactor != nil {
			retry, compactErr := s.reactiveCompactor.HandleError(ctx, state.Messages, err, model)
			if retry {
				continue
			}
			err = compactErr
			state.Think.LastError = err
		}

		if s.recovery == nil {
			return nil, err
		}

		action := s.recovery.Decide(err, maxTokens)
		if !action.ShouldRetry {
			return nil, action.FinalError
		}

		switch action.Tier {
		case TierRetry:
			continue
		case TierEscalateOutput:
			maxTokens = action.NewMaxTokens
			continue
		case TierInjectMessage:
			state.Messages.AppendPending(providers.Message{
				Role:    "user",
				Content: action.InjectMsg,
			})
			continue
		case TierFallbackModel:
			model = action.NewModel
			state.Model = action.NewModel
			if s.deps.ResolveContextWindow != nil && state.Provider != nil {
				if cw := s.deps.ResolveContextWindow(state.Provider.Name(), action.NewModel); cw > 0 {
					state.Context.EffectiveContextWindow = cw
				}
			}
			continue
		default:
			return nil, err
		}
	}
}

func (s *ThinkStage) requestMessages(state *RunState) []providers.Message {
	msgs := state.Messages.All()
	if state != nil {
		if summary := state.EnsureConstraintStore().ForSystemPrompt(); summary != "" {
			msgs = append(msgs, providers.Message{
				Role:    "system",
				Content: summary,
			})
		}
	}
	if s.deps.Config.ContextCollapse != nil {
		return s.deps.Config.ContextCollapse.Project(msgs, state.Iteration, state.Messages.TotalLen())
	}
	return msgs
}

func (s *ThinkStage) prepareStreamingExecutor(ctx context.Context, state *RunState) {
	state.Tool.StreamExecutor = nil
	state.Tool.StreamedToolIDs = nil

	if !s.deps.Config.StreamingToolExec ||
		state.Input == nil ||
		!state.Input.Stream ||
		s.deps.ExecuteToolRaw == nil ||
		s.deps.ProcessToolResult == nil {
		return
	}

	state.Tool.StreamExecutor = NewStreamingToolExecutor(
		s.deps.IsToolConcurrencySafe,
		s.deps.ExecuteToolRaw,
		ctx,
	)
	state.Tool.StreamedToolIDs = make(map[string]bool)
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
