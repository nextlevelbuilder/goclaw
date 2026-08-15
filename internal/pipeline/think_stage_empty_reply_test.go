package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- ThinkStage empty-reply nudge ---

func emptyReplyState(iteration int) *RunState {
	state := defaultState()
	state.Iteration = iteration
	return state
}

// TestThinkStage_EmptyFinalReply_NudgesModel verifies that a final response with
// no text and no tool calls triggers a bounded nudge (Continue) instead of
// BreakLoop, so the user gets a real answer rather than a "..." placeholder.
func TestThinkStage_EmptyFinalReply_NudgesModel(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(0)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (nudge for a real answer)", stage.Result())
	}
	if state.Think.EmptyReplyRetries != 1 {
		t.Errorf("EmptyReplyRetries = %d, want 1", state.Think.EmptyReplyRetries)
	}
	pending := state.Messages.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending len = %d, want 1 nudge message", len(pending))
	}
	if pending[0].Role != "user" || !strings.Contains(pending[0].Content, emptyReplyHint) {
		t.Errorf("nudge = %q/%q, want user/%q", pending[0].Role, pending[0].Content, emptyReplyHint)
	}
	if !pending[0].Transient {
		t.Errorf("nudge should be Transient so it never pollutes persisted history")
	}
}

// TestThinkStage_EmptyFinalReply_LastIterationBreaks verifies the nudge is
// skipped on the final iteration: there is no iteration left to answer it, so
// the run breaks and FinalizeStage provides the fallback.
func TestThinkStage_EmptyFinalReply_LastIterationBreaks(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 3, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(2) // last of 3 iterations

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop on final iteration", stage.Result())
	}
	if state.Think.EmptyReplyRetries != 0 {
		t.Errorf("EmptyReplyRetries = %d, want 0 (no nudge on final iteration)", state.Think.EmptyReplyRetries)
	}
	if len(state.Messages.Pending()) != 0 {
		t.Errorf("pending = %v, want empty (no nudge on final iteration)", state.Messages.Pending())
	}
}

// TestThinkStage_EmptyFinalReply_RetriesExhaustedBreaks verifies the nudge is
// bounded: after maxEmptyReplyRetries unanswered nudges, the run breaks.
func TestThinkStage_EmptyFinalReply_RetriesExhaustedBreaks(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(5)
	state.Think.EmptyReplyRetries = maxEmptyReplyRetries

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop after retries exhausted", stage.Result())
	}
	if len(state.Messages.Pending()) != 0 {
		t.Errorf("pending = %v, want empty after retries exhausted", state.Messages.Pending())
	}
}

// TestThinkStage_EmptyFinalReply_MediaOnlyBreaks verifies media-only runs break
// immediately: the media IS the deliverable, no text caption needed (matching
// v2 hasDeliverableOutput semantics).
func TestThinkStage_EmptyFinalReply_MediaOnlyBreaks(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(0)
	state.Tool.MediaResults = []MediaResult{{Path: "/tmp/img.png", ContentType: "image/png"}}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop for media-only run", stage.Result())
	}
	if state.Think.EmptyReplyRetries != 0 {
		t.Errorf("EmptyReplyRetries = %d, want 0 (media-only)", state.Think.EmptyReplyRetries)
	}
	if len(state.Messages.Pending()) != 0 {
		t.Errorf("pending = %v, want empty for media-only run", state.Messages.Pending())
	}
}

// TestThinkStage_EmptyFinalReply_ForwardMediaBreaks covers the forwarded-media
// deliverable path (inbound attachments carried through the run).
func TestThinkStage_EmptyFinalReply_ForwardMediaBreaks(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(0)
	state.Input.ForwardMedia = []bus.MediaFile{{Path: "/tmp/doc.pdf", MimeType: "application/pdf"}}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop for forwarded-media run", stage.Result())
	}
	if len(state.Messages.Pending()) != 0 {
		t.Errorf("pending = %v, want empty for forwarded-media run", state.Messages.Pending())
	}
}

// TestThinkStage_EmptyFinalReply_ContentSuffixBreaks covers the content-suffix
// deliverable path (e.g. WS image markdown appended at finalize).
func TestThinkStage_EmptyFinalReply_ContentSuffixBreaks(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(0)
	state.Input.ContentSuffix = "\n![img](/media/img.png)"

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop for content-suffix run", stage.Result())
	}
	if len(state.Messages.Pending()) != 0 {
		t.Errorf("pending = %v, want empty for content-suffix run", state.Messages.Pending())
	}
}

// TestThinkStage_EmptyFinalReply_WhitespaceOnlyNudges verifies whitespace-only
// content is treated as empty (TrimSpace), nudging rather than delivering " ".
func TestThinkStage_EmptyFinalReply_WhitespaceOnlyNudges(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      " \n\t ",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := emptyReplyState(0)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (whitespace-only nudges)", stage.Result())
	}
	if state.Think.EmptyReplyRetries != 1 {
		t.Errorf("EmptyReplyRetries = %d, want 1", state.Think.EmptyReplyRetries)
	}
}

// --- FinalizeStage localized fallback ---

// TestFinalizeStage_EmptyContent_LocalizedFallback verifies the final
// placeholder is the localized message, never a bare "...".
func TestFinalizeStage_EmptyContent_LocalizedFallback(t *testing.T) {
	t.Parallel()
	ctx := store.WithLocale(context.Background(), "en")
	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = ""

	if err := stage.Execute(ctx, state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	want := i18n.T(store.LocaleFromContext(ctx), i18n.MsgEmptyReplyFallback)
	if state.Observe.FinalContent != want {
		t.Errorf("FinalContent = %q, want localized fallback %q (not \"...\")", state.Observe.FinalContent, want)
	}
	if state.Observe.FinalContent == "..." {
		t.Errorf("FinalContent must never be the old bare \"...\" placeholder")
	}
}

// TestFinalizeStage_EmptyContent_LocaleSpecific verifies the fallback respects
// the active locale (vi here).
func TestFinalizeStage_EmptyContent_LocaleSpecific(t *testing.T) {
	t.Parallel()
	ctx := store.WithLocale(context.Background(), "vi")
	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = ""

	if err := stage.Execute(ctx, state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	want := i18n.T("vi", i18n.MsgEmptyReplyFallback)
	if state.Observe.FinalContent != want {
		t.Errorf("FinalContent = %q, want vi fallback %q", state.Observe.FinalContent, want)
	}
}

// TestFinalizeStage_EmptyContent_MediaOnlySkipsFallback verifies media-only
// runs stay media-only — no text caption injected (v2 parity).
func TestFinalizeStage_EmptyContent_MediaOnlySkipsFallback(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = ""
	state.Tool.MediaResults = []MediaResult{{Path: "/tmp/img.png", ContentType: "image/png"}}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "" {
		t.Errorf("FinalContent = %q, want empty (media-only run)", state.Observe.FinalContent)
	}
}

// TestFinalizeStage_EmptyContent_ForwardMediaSkipsFallback covers the
// forwarded-media deliverable path at finalize.
func TestFinalizeStage_EmptyContent_ForwardMediaSkipsFallback(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = ""
	state.Input.ForwardMedia = []bus.MediaFile{{Path: "/tmp/doc.pdf", MimeType: "application/pdf"}}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "" {
		t.Errorf("FinalContent = %q, want empty (forwarded-media run)", state.Observe.FinalContent)
	}
}

// TestFinalizeStage_EmptyContent_ContentSuffixSkipsFallback covers the
// content-suffix deliverable path (suffix still appended, no placeholder).
func TestFinalizeStage_EmptyContent_ContentSuffixSkipsFallback(t *testing.T) {
	t.Parallel()
	suffix := "\n![img](/media/img.png)"
	deps := &PipelineDeps{
		DeduplicateMediaSuffix: func(content, toAppend string) string {
			if strings.HasSuffix(content, toAppend) {
				return ""
			}
			return toAppend
		},
	}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = ""
	state.Input.ContentSuffix = suffix

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != suffix {
		t.Errorf("FinalContent = %q, want suffix-only %q (no placeholder)", state.Observe.FinalContent, suffix)
	}
}

// TestFinalizeStage_EmptyContent_IsSilentSkipsFallback verifies silent
// (NO_REPLY) runs keep empty delivery — the fallback must not fire.
func TestFinalizeStage_EmptyContent_IsSilentSkipsFallback(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		IsSilentReply: func(_ string) bool { return true },
	}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = ""

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "" {
		t.Errorf("FinalContent = %q, want empty (silent run suppressed)", state.Observe.FinalContent)
	}
}
