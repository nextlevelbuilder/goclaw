package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestFreshToolResultCap_TrimsOversizedPending verifies Gap B: a fresh (pending)
// tool result far above FreshResultCapTokens is trimmed head + tail before the
// request is built, so the final request stays within budget. The trim keeps an
// important tail (70/30 when the tail has result/error keywords) and the
// structure marker. History is untouched by the cap.
func TestFreshToolResultCap_TrimsOversizedPending(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 10000,
			MaxTokens:     500,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 1},
		GetPruningConfig: func() *config.ContextPruningConfig {
			return &config.ContextPruningConfig{FreshResultCapTokens: 100}
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{})

	bigResult := "This is the head of a very large tool result. " + strings.Repeat("filler content ", 500) + "\nResult: SUCCESS total=42"
	state.Messages.AppendPending(providers.Message{Role: "tool", Content: bigResult, ToolCallID: "tc1", ToolName: "web_search"})

	req, _, err := stage.prepareFinalRequest(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("prepareFinalRequest error: %v", err)
	}

	// Find the trimmed tool message in the request.
	var got string
	for _, m := range req.Messages {
		if m.Role == "tool" {
			got = m.Content
		}
	}
	if got == "" {
		t.Fatal("no tool message found in request")
	}
	if strings.Contains(got, bigResult) {
		t.Error("request still contains the full oversized result — cap not applied")
	}
	if !strings.Contains(got, "Result: SUCCESS total=42") {
		t.Errorf("important tail not preserved, got tail: %q", got)
	}
	if !strings.Contains(got, "[Tool result trimmed:") {
		t.Errorf("trim marker missing: %q", got)
	}
	// ~capChars = 2000 chars cap, trimmed contents must be well under.
	if len([]rune(got)) > 3000 {
		t.Errorf("trimmed result still huge: %d runes", len([]rune(got)))
	}
}

// TestFreshToolResultCap_MediaPreserved verifies Gap B media guard: read_image
// style results are never trimmed even when oversized — they carry irreplaceable
// vision descriptions.
func TestFreshToolResultCap_MediaPreserved(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 10000,
			MaxTokens:     500,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 1},
		GetPruningConfig: func() *config.ContextPruningConfig {
			return &config.ContextPruningConfig{FreshResultCapTokens: 10}
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Messages.AppendPending(providers.Message{Role: "tool", Content: strings.Repeat("description ", 200), ToolCallID: "tc_img", ToolName: "read_image"})

	req, _, err := stage.prepareFinalRequest(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("prepareFinalRequest error: %v", err)
	}
	for _, m := range req.Messages {
		if m.Role == "tool" && m.ToolCallID == "tc_img" {
			if strings.Contains(m.Content, "[Tool result trimmed") {
				t.Error("media result was trimmed — vision descriptions must be preserved")
			}
			if len([]rune(m.Content)) < len([]rune(strings.Repeat("description ", 200))) {
				t.Error("media result content was shortened")
			}
		}
	}
}

// TestFreshToolResultCap_ZeroConfig_NoChange verifies Gap B zero-config behavior:
// no GetPruningConfig / cap 0 → pending tool results pass through untouched.
func TestFreshToolResultCap_ZeroConfig_NoChange(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config:      PipelineConfig{ContextWindow: 10000, MaxTokens: 500},
		TokenCounter: &mockTokenCounter{countPerMessage: 1},
		// GetPruningConfig nil — cap disabled.
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	orig := strings.Repeat("very long fresh result ", 200)
	state.Messages.AppendPending(providers.Message{Role: "tool", Content: orig, ToolCallID: "tc1"})

	req, _, err := stage.prepareFinalRequest(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("prepareFinalRequest error: %v", err)
	}
	var got string
	for _, m := range req.Messages {
		if m.Role == "tool" {
			got = m.Content
		}
	}
	if got != orig {
		t.Error("zero-config must not alter pending tool results")
	}
}

// TestIterationProgress_WiredPerIteration verifies Gap F: each pipeline iteration
// runs with tools.WithIterationProgress set, so adaptive tool caps (web_fetch)
// observe the current iteration. The run wires a temporary context read inside
// CallLLM via a context-getter callback.
func TestIterationProgress_WiredPerIteration(t *testing.T) {
	t.Parallel()
	progressSeen := make(chan tools.IterationProgress, 2)
	deps := PipelineDeps{
		Config: PipelineConfig{
			MaxIterations: 2,
			MaxTokens:     500,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 1},
		CallLLM: func(ctx context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			if p, ok := tools.IterationProgressFromCtx(ctx); ok {
				progressSeen <- p
			}
			// First call requests a tool call so the loop runs a second iteration;
			// second call finishes. The repeated content keeps the message long
			// enough that the response is not treated as a NO_REPLY.
			return &providers.ChatResponse{
				Content:      "hi",
				FinishReason: "stop",
				ToolCalls:    []providers.ToolCall{{ID: "c1", Name: "some_tool", Arguments: map[string]any{}}},
			}, nil
		},
		// Mock tool execution so the tool call in iteration 1 doesn't fail the run.
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", Content: "ok", ToolCallID: "c1"}}, nil
		},
	}
	state := defaultState()
	state.Context.EffectiveContextWindow = 10000
	if _, err := NewDefaultPipeline(deps).Run(context.Background(), state); err != nil {
		t.Fatalf("pipeline Run() error: %v", err)
	}
	close(progressSeen)
	var seen []tools.IterationProgress
	for p := range progressSeen {
		seen = append(seen, p)
	}
	if len(seen) != 2 {
		t.Fatalf("IterationProgress seen %d times, want 2 (one per iteration)", len(seen))
	}
	if seen[0].Current != 1 || seen[1].Current != 2 {
		t.Errorf("iteration progress = %+v → %+v, want Current 1 then 2", seen[0], seen[1])
	}
	if seen[0].Max != 2 {
		t.Errorf("Max = %d, want 2 (MaxIterations)", seen[0].Max)
	}
}