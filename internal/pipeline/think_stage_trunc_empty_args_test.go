package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestThinkStage_WriteFileEmptyArgsTreatedAsTruncated verifies that when
// Gemini returns finish_reason="tool_calls" with an empty-args call to a
// mutating tool (write_file), the stage treats it as truncation and retries.
// Trace: 019d8f33-2de1-7ab2-9a32-9df92cd610dd.
func TestThinkStage_WriteFileEmptyArgsTreatedAsTruncated(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{
					{ID: "tc1", Name: "write_file", Arguments: map[string]any{}},
				},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (retry)", stage.Result())
	}
	if state.Think.TruncRetries != 1 {
		t.Errorf("TruncRetries = %d, want 1", state.Think.TruncRetries)
	}
	pending := state.Messages.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending len = %d, want 2 (assistant partial + user hint)", len(pending))
	}
}

// TestThinkStage_DatetimeEmptyArgsNoRetry is the critical regression guard
// for Phase 03. Nullary/optional-args tools (datetime, heartbeat) routinely
// call with empty args; the truncation heuristic MUST skip them.
func TestThinkStage_DatetimeEmptyArgsNoRetry(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{
					{ID: "tc1", Name: "datetime", Arguments: map[string]any{}},
				},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Think.TruncRetries != 0 {
		t.Errorf("TruncRetries = %d, want 0 (datetime is nullary — no retry)", state.Think.TruncRetries)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (normal tool flow)", stage.Result())
	}
}

// TestThinkStage_HeartbeatEmptyArgsNoRetry sanity checks a second nullary tool.
func TestThinkStage_HeartbeatEmptyArgsNoRetry(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{
					{ID: "tc1", Name: "heartbeat", Arguments: map[string]any{}},
				},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Think.TruncRetries != 0 {
		t.Errorf("TruncRetries = %d, want 0 (heartbeat is nullary)", state.Think.TruncRetries)
	}
}

// TestThinkStage_WriteFileEmptyArgsExhaustRetries verifies that after 3
// iterations of empty-args write_file, the stage returns AbortRun — the
// existing truncation ceiling applies to the new heuristic too.
func TestThinkStage_WriteFileEmptyArgsExhaustRetries(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{
					{ID: "tc1", Name: "write_file", Arguments: map[string]any{}},
				},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Think.TruncRetries = 2 // next call is the 3rd

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != AbortRun {
		t.Errorf("Result() = %v, want AbortRun after 3rd retry", stage.Result())
	}
}

// TestThinkStage_WriteFileWithArgsNoRetry confirms non-empty args on an
// allowlisted tool pass through normally — only empty args trigger retry.
func TestThinkStage_WriteFileWithArgsNoRetry(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{
					{ID: "tc1", Name: "write_file", Arguments: map[string]any{
						"path":    "USER.md",
						"content": "Hello",
					}},
				},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Think.TruncRetries != 0 {
		t.Errorf("TruncRetries = %d, want 0 (args present)", state.Think.TruncRetries)
	}
}
