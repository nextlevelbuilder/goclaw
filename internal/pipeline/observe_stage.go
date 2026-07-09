package pipeline

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ObserveStage runs per iteration after ToolStage. Drains InjectCh,
// accumulates final content when no tool calls, tracks block replies.
// Does NOT implement StageWithResult — never controls flow.
type ObserveStage struct {
	deps *PipelineDeps
}

// NewObserveStage creates an ObserveStage.
func NewObserveStage(deps *PipelineDeps) *ObserveStage {
	return &ObserveStage{deps: deps}
}

func (s *ObserveStage) Name() string { return "observe" }

// Execute drains injected messages, accumulates final content + block replies.
func (s *ObserveStage) Execute(_ context.Context, state *RunState) error {
	injected := s.drainInjectedMessages()

	resp := state.Think.LastResponse
	if resp == nil {
		appendPendingMessages(state, injected)
		return nil
	}

	// Track block replies only for tool-iteration responses. Final answers do
	// not count, otherwise gateway dedup can suppress delivery.
	if resp.Content != "" && len(resp.ToolCalls) > 0 {
		state.Observe.BlockReplies++
		state.Observe.LastBlockReply = resp.Content
	}

	if len(resp.ToolCalls) == 0 {
		s.observeFinalResponse(state, resp, injected)
	} else {
		appendPendingMessages(state, injected)
	}

	s.accumulateAssistantImages(state, resp)
	return nil
}

func (s *ObserveStage) drainInjectedMessages() []providers.Message {
	if s.deps.DrainInjectCh == nil {
		return nil
	}
	return s.deps.DrainInjectCh()
}

func (s *ObserveStage) observeFinalResponse(state *RunState, resp *providers.ChatResponse, injected []providers.Message) {
	content, thinking := splitTaggedThinkingContent(resp.Content, resp.Thinking)

	// Fire post_model_response hook for final responses (no tool calls).
	// This is a blocking hook that can prevent user delivery and inject a retry.
	if s.deps.Hooks != nil && len(injected) == 0 {
		ev := hooks.Event{
			EventID:       uuid.NewString(),
			SessionID:     state.Input.SessionKey,
			TenantID:      store.TenantIDFromContext(state.Ctx),
			AgentID:       store.AgentIDFromContext(state.Ctx),
			HookEvent:     hooks.EventPostModelResponse,
			ModelResponse: content,
			Thinking:      thinking,
			ToolCalls:     resp.ToolCalls,
		}
		result, err := s.deps.FireHook(state.Ctx, ev)
		if err != nil {
			slog.Debug("post_model_response hook error", "error", err)
		}
		if result.Decision == hooks.DecisionBlock {
			// Hook blocked delivery: keep assistant message in history, inject
			// rejection reason as user message, and continue to next iteration.
			state.Messages.AppendPending(providers.Message{
				Role:     "assistant",
				Content:  content,
				Thinking: thinking,
			})
			rejectionMsg := result.DecisionReason
			if rejectionMsg == "" {
				rejectionMsg = "Response blocked by policy."
			}
			state.Messages.AppendPending(providers.Message{
				Role:    "user",
				Content: rejectionMsg,
			})
			state.Observe.BlockedByHook = true
			state.Observe.HookRejectionReason = rejectionMsg
			state.Observe.ContinueAfterFinal = true
			return
		}
	}

	if len(injected) == 0 {
		state.Observe.FinalContent = content
		state.Observe.FinalThinking = thinking
		return
	}

	state.Messages.AppendPending(providers.Message{
		Role:      "assistant",
		Content:   content,
		Thinking:  thinking,
		Transient: true,
	})
	appendPendingMessages(state, injected)
	state.Observe.FinalContent = ""
	state.Observe.FinalThinking = ""
	state.Observe.ContinueAfterFinal = true
}

func (s *ObserveStage) accumulateAssistantImages(state *RunState, resp *providers.ChatResponse) {
	if len(resp.Images) == 0 {
		return
	}
	for _, img := range resp.Images {
		if img.Partial {
			continue
		}
		state.Observe.AssistantImages = append(state.Observe.AssistantImages, img)
	}
	// Clear on response so a re-processing pass (for example a retry) does not double-count.
	resp.Images = nil
}

func appendPendingMessages(state *RunState, messages []providers.Message) {
	for _, msg := range messages {
		state.Messages.AppendPending(msg)
	}
}
