package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type usageToolExecutor struct{}

func (usageToolExecutor) ExecuteWithContext(_ context.Context, _ string, _ map[string]any, _, _, _, _ string, _ tools.AsyncCallback) *tools.Result {
	return &tools.Result{ForLLM: "ok", Provider: "9router", Model: "cx/gpt-5.5",
		Usage: &providers.Usage{PromptTokens: 4677, CompletionTokens: 1799, TotalTokens: 6476}}
}
func (usageToolExecutor) TryActivateDeferred(string) bool          { return false }
func (usageToolExecutor) ProviderDefs() []providers.ToolDefinition { return nil }
func (usageToolExecutor) Get(string) (tools.Tool, bool)            { return nil, false }
func (usageToolExecutor) List() []string                           { return nil }
func (usageToolExecutor) Aliases() map[string]string               { return nil }

func TestToolCallUsageRecorded(t *testing.T) {
	l := &Loop{id: "a", tools: usageToolExecutor{}}
	req := &RunRequest{RunID: "r1", SessionKey: "s1", Channel: "ws"}
	state := &pipeline.RunState{RunID: "r1"}
	tc := providers.ToolCall{ID: "tc-1", Name: "read_image", Arguments: map[string]any{}}

	if _, err := l.makeExecuteToolCall(req, &runState{})(context.Background(), state, tc); err != nil {
		t.Fatalf("makeExecuteToolCall: %v", err)
	}
	if len(state.Calls) != 1 {
		t.Fatalf("Calls len = %d, want 1", len(state.Calls))
	}
	c := state.Calls[0]
	if c.Type != "tool_call" || c.Name != "read_image" || c.Provider != "9router" || c.Model != "cx/gpt-5.5" {
		t.Errorf("wrong attribution: %+v", c)
	}
	if c.PromptTokens != 4677 || c.CompletionTokens != 1799 {
		t.Errorf("wrong tokens: %+v", c.Usage)
	}
}
