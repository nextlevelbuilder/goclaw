package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestThinkStage_AccumulatesCacheTokens guards the pipeline aggregation of cache
// tokens across turns. Regression: async agent runs reported 0 cache tokens in
// webhook usage / usage_events despite traces showing heavy cache use, because
// ThinkStage summed only prompt/completion/total/thinking and dropped the cache
// fields when folding each turn's usage into state.Think.TotalUsage.
func TestThinkStage_AccumulatesCacheTokens(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "stop",
				Content:      "done",
				Usage: &providers.Usage{
					PromptTokens:                      100,
					CompletionTokens:                  20,
					TotalTokens:                       120,
					CacheReadTokens:                   80,
					CacheCreationTokens:               10,
					PromptTokensIncludeCachedSegments: true,
				},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	// Two iterations to prove accumulation (+=), not assignment.
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() 1: %v", err)
	}
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() 2: %v", err)
	}

	u := state.Think.TotalUsage
	if u.CacheReadTokens != 160 {
		t.Errorf("CacheReadTokens = %d, want 160", u.CacheReadTokens)
	}
	if u.CacheCreationTokens != 20 {
		t.Errorf("CacheCreationTokens = %d, want 20", u.CacheCreationTokens)
	}
	if !u.PromptTokensIncludeCachedSegments {
		t.Error("PromptTokensIncludeCachedSegments = false, want true")
	}
	if u.PromptTokens != 200 || u.CompletionTokens != 40 {
		t.Errorf("base tokens = %d/%d, want 200/40", u.PromptTokens, u.CompletionTokens)
	}
}
