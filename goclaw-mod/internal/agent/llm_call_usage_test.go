package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type usageStubProvider struct{}

func (usageStubProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: "hi", FinishReason: "stop",
		Usage: &providers.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheReadTokens: 80}}, nil
}
func (usageStubProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return usageStubProvider{}.Chat(context.Background(), providers.ChatRequest{})
}
func (usageStubProvider) DefaultModel() string { return "stub-model" }
func (usageStubProvider) Name() string         { return "stubprov" }

func TestLLMCallUsageRecorded(t *testing.T) {
	prov := usageStubProvider{}
	l := &Loop{id: "a"}
	req := &RunRequest{RunID: "r1", SessionKey: "s1", Channel: "ws"}
	state := &pipeline.RunState{RunID: "r1", Provider: prov, Model: "stub-model"}

	_, err := l.makeCallLLM(req, func(AgentEvent) {})(context.Background(), state, providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("makeCallLLM: %v", err)
	}
	if len(state.Calls) != 1 {
		t.Fatalf("Calls len = %d, want 1", len(state.Calls))
	}
	c := state.Calls[0]
	if c.Type != "llm_call" || c.Provider != "stubprov" || c.Model != "stub-model" {
		t.Errorf("wrong attribution: %+v", c)
	}
	if c.PromptTokens != 100 || c.CacheReadTokens != 80 {
		t.Errorf("wrong tokens: %+v", c.Usage)
	}
}
