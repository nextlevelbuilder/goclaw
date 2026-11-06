package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestThinkStage_NativeToolStaysOutOfAllowedTools verifies that provider-native
// tools, which have a nil Function, are sent to the provider without entering
// the execution allowlist: only the provider can run them.
func TestThinkStage_NativeToolStaysOutOfAllowedTools(t *testing.T) {
	t.Parallel()
	var sent []providers.ToolDefinition
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		BuildFilteredTools: func(_ *RunState) ([]providers.ToolDefinition, error) {
			return []providers.ToolDefinition{
				{Type: "function", Function: &providers.ToolFunctionSchema{Name: "datetime"}},
				{Type: "image_generation"},
			}, nil
		},
		CallLLM: func(_ context.Context, _ *RunState, req providers.ChatRequest) (*providers.ChatResponse, error) {
			sent = req.Tools
			return &providers.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(sent) != 2 {
		t.Errorf("provider tools = %d, want 2", len(sent))
	}
	if len(state.Tool.AllowedTools) != 1 || !state.Tool.AllowedTools["datetime"] {
		t.Errorf("AllowedTools = %v, want only datetime", state.Tool.AllowedTools)
	}
}
