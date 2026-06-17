package pipeline

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
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
	if len(injected) == 0 {
		state.Observe.FinalContent = resp.Content
		state.Observe.FinalThinking = resp.Thinking
		return
	}

	state.Messages.AppendPending(providers.Message{
		Role:      "assistant",
		Content:   resp.Content,
		Thinking:  resp.Thinking,
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
