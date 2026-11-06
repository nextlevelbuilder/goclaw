package agent

// Regression tests for the image_generation sentinel flowing through the
// pipeline callbacks. The sentinel is a provider-native tool with a nil
// Function, so every consumer of the assembled tool list must tolerate it.
// A panic here kills the run before the LLM call and Telegram users only see
// silence.

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

func TestMakeBuildFilteredTools_ImageGenSentinelDoesNotPanic(t *testing.T) {
	l := buildImageGenLoop(true, &imageCapableProvider{imageGen: true})
	l.maxIterations = 10 // the zero value would hit final-iteration stripping
	state := pipeline.NewRunState(
		&pipeline.RunInput{SessionKey: "sess-1", RunID: "run-1", UserID: "user-1"},
		&workspace.WorkspaceContext{ActivePath: "/tmp"}, "gpt-6.1-sol", nil,
	)

	defs, err := l.makeBuildFilteredTools(&RunRequest{})(state)
	if err != nil {
		t.Fatalf("makeBuildFilteredTools() error: %v", err)
	}
	if !hasImageGenTool(defs) {
		t.Error("expected image_generation tool to survive the callback")
	}
}

func TestThinkStage_ImageGenSentinelReachesProvider(t *testing.T) {
	l := buildImageGenLoop(true, &imageCapableProvider{imageGen: true})
	l.maxIterations = 10 // the zero value would hit final-iteration stripping
	var sent []providers.ToolDefinition
	stage := pipeline.NewThinkStage(&pipeline.PipelineDeps{
		Config:             pipeline.PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		BuildFilteredTools: l.makeBuildFilteredTools(&RunRequest{}),
		CallLLM: func(_ context.Context, _ *pipeline.RunState, req providers.ChatRequest) (*providers.ChatResponse, error) {
			sent = req.Tools
			return &providers.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
		},
	})
	state := pipeline.NewRunState(
		&pipeline.RunInput{SessionKey: "sess-1", RunID: "run-1", UserID: "user-1"},
		&workspace.WorkspaceContext{ActivePath: "/tmp"}, "gpt-6.1-sol", nil,
	)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !hasImageGenTool(sent) {
		t.Error("expected image_generation tool in the provider request")
	}
}

func TestShouldRetryTaskMCP_SkipsToolsWithoutFunction(t *testing.T) {
	req := providers.ChatRequest{
		Tools: []providers.ToolDefinition{
			{Type: "image_generation"},
			{Type: "function", Function: &providers.ToolFunctionSchema{Name: "mcp_bx24__task_search"}},
		},
		Messages: []providers.Message{{Role: "user", Content: "tìm task của tôi"}},
	}

	if !shouldRetryTaskMCP(req) {
		t.Error("expected retry when a task MCP tool follows the image_generation sentinel")
	}
}
