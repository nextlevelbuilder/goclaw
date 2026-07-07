package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type finalThinkingStreamProvider struct{}

func (p finalThinkingStreamProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: "final", Thinking: "non-stream thinking"}, nil
}

func (p finalThinkingStreamProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: "final", Thinking: "final streamed thinking"}, nil
}

func (p finalThinkingStreamProvider) DefaultModel() string { return "test-model" }
func (p finalThinkingStreamProvider) Name() string         { return "test-provider" }

func TestMakeCallLLM_StreamsFinalThinkingWhenNoThinkingChunkArrives(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}
	req := &RunRequest{
		RunID:      "run-1",
		SessionKey: "sess-1",
		Channel:    "telegram",
		Stream:     true,
	}
	state := &pipeline.RunState{
		Provider:  finalThinkingStreamProvider{},
		Model:     "test-model",
		Iteration: 0,
	}

	resp, err := loop.makeCallLLM(req, col.onEvent)(context.Background(), state, providers.ChatRequest{})
	if err != nil {
		t.Fatalf("makeCallLLM returned error: %v", err)
	}
	if resp == nil || resp.Thinking != "final streamed thinking" {
		t.Fatalf("stream response = %+v, want final thinking preserved", resp)
	}

	thinking := col.filter(protocol.ChatEventThinking)
	if len(thinking) != 1 {
		t.Fatalf("thinking events = %+v, want exactly one final thinking event", thinking)
	}
	payload, ok := thinking[0].Payload.(map[string]string)
	if !ok || payload["content"] != "final streamed thinking" {
		t.Fatalf("thinking payload = %+v", thinking[0].Payload)
	}
}

func TestPromptCacheOptionsHelpers(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	agentID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	key1 := defaultPromptCacheKey(tenantID, agentID, "codex", "session-a")
	key2 := defaultPromptCacheKey(tenantID, agentID, "codex", "session-a")
	key3 := defaultPromptCacheKey(tenantID, agentID, "codex", "session-b")
	if key1 != key2 {
		t.Fatalf("defaultPromptCacheKey not stable: %q != %q", key1, key2)
	}
	if key1 == key3 {
		t.Fatal("defaultPromptCacheKey should vary by session")
	}
	if !strings.HasPrefix(key1, "goclaw/") {
		t.Fatalf("defaultPromptCacheKey = %q, want goclaw/ prefix", key1)
	}

	opts := map[string]any{}
	setDefaultPromptCacheOptions(opts, tenantID, agentID, "codex", "session-a")
	if opts[providers.OptPromptCacheKey] != key1 {
		t.Fatalf("prompt cache key = %v, want %s", opts[providers.OptPromptCacheKey], key1)
	}
	if opts[providers.OptPromptCacheRetention] != "24h" {
		t.Fatalf("prompt cache retention = %v, want 24h", opts[providers.OptPromptCacheRetention])
	}

	opts = map[string]any{
		providers.OptPromptCacheKey:       "custom-key",
		providers.OptPromptCacheRetention: "in_memory",
	}
	setDefaultPromptCacheOptions(opts, tenantID, agentID, "codex", "session-a")
	if opts[providers.OptPromptCacheKey] != "custom-key" || opts[providers.OptPromptCacheRetention] != "in_memory" {
		t.Fatalf("custom prompt cache options were overwritten: %+v", opts)
	}
}

func TestSupportsPromptCacheParams(t *testing.T) {
	if !supportsPromptCacheParams(providers.NewCodexProvider("codex", nil, "", "")) {
		t.Fatal("CodexProvider should support prompt cache params")
	}
	if supportsPromptCacheParams(finalThinkingStreamProvider{}) {
		t.Fatal("generic provider should not support prompt cache params")
	}
}

// A Function-nil tool definition (e.g. the native image_generation sentinel,
// providers.ToolDefinition{Type: "image_generation"}) must not panic the
// mcp-def counter. Regression for the v3.14.0 nil-pointer crash.
func TestCountMCPToolDefs_SkipsNilFunction(t *testing.T) {
	defs := []providers.ToolDefinition{
		{Type: "image_generation"}, // Function == nil
		{Function: &providers.ToolFunctionSchema{Name: "mcp_notion_search"}},
		{Function: &providers.ToolFunctionSchema{Name: " mcp_slack_post "}},
		{Function: &providers.ToolFunctionSchema{Name: "read_file"}},
	}

	if got := countMCPToolDefs(defs); got != 2 {
		t.Errorf("countMCPToolDefs = %d, want 2", got)
	}
}

// The image_generation sentinel must carry a non-nil Function so the many
// pipeline/provider sites that read td.Function.Name (think_stage, codex_build,
// shouldRetryTaskMCP, history tool names, …) never nil-deref. Root-cause guard
// for the v3.14.0 crash — one landmine removed instead of guarding every site.
func TestImageGenToolDef_FunctionNonNil(t *testing.T) {
	if imageGenToolDef.Type != "image_generation" {
		t.Fatalf("sentinel Type = %q, want image_generation", imageGenToolDef.Type)
	}
	if imageGenToolDef.Function == nil {
		t.Fatal("sentinel Function must be non-nil to avoid downstream nil-deref")
	}
	if imageGenToolDef.Function.Name != "image_generation" {
		t.Errorf("sentinel Function.Name = %q, want image_generation", imageGenToolDef.Function.Name)
	}
}
