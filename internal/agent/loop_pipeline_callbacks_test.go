package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
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

type requestCaptureProvider struct {
	request providers.ChatRequest
}

func (p *requestCaptureProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.request = req
	return &providers.ChatResponse{Content: "ok"}, nil
}

func (p *requestCaptureProvider) ChatStream(_ context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	p.request = req
	return &providers.ChatResponse{Content: "ok"}, nil
}

func (p *requestCaptureProvider) DefaultModel() string { return "test-model" }
func (p *requestCaptureProvider) Name() string         { return "test-provider" }

type recordingSessionStore struct {
	*nopSessionStore
	added []providers.Message
}

func (s *recordingSessionStore) AddMessage(_ context.Context, _ string, msg providers.Message) {
	s.added = append(s.added, msg)
}

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

func TestMakeCallLLMPropagatesDelegationArtifactBridgeOptions(t *testing.T) {
	provider := &requestCaptureProvider{}
	loop := &Loop{id: "target-agent", agentUUID: uuid.New()}
	req := &RunRequest{RunID: "run-1", SessionKey: "delegate:session"}
	state := &pipeline.RunState{Provider: provider, Model: "test-model"}
	ctx := tools.WithToolWorkspace(context.Background(), "/runtime/outputs")
	ctx = tools.WithDelegationID(ctx, "delegation-id")
	ctx = tools.WithDelegationArtifactInputs(ctx, "/runtime/inputs")

	if _, err := loop.makeCallLLM(req, func(AgentEvent) {})(ctx, state, providers.ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	if provider.request.Options[providers.OptDelegationID] != "delegation-id" ||
		provider.request.Options[providers.OptDelegationInputs] != "/runtime/inputs" {
		t.Fatalf("delegation options = %#v", provider.request.Options)
	}
}

func TestEnrichedInputMediaPersistsForNextTurn(t *testing.T) {
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "photo.png")
	if err := os.WriteFile(sourcePath, minimalPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	sessions := &recordingSessionStore{nopSessionStore: &nopSessionStore{}}
	loop := &Loop{sessions: sessions}
	req := &RunRequest{
		SessionKey: "session-media",
		Message:    `<media:image url="attachment://photo.png">`,
		Media: []bus.MediaFile{{
			Path:     sourcePath,
			MimeType: "image/png",
			Filename: "photo.png",
		}},
	}
	state := &pipeline.RunState{Messages: pipeline.NewMessageBuffer(providers.Message{Role: "system"})}
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: req.Message}})
	ctx := tools.WithToolWorkspace(context.Background(), workspace)

	if err := loop.makeEnrichMedia(req)(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := loop.makeFlushMessages(req)(ctx, req.SessionKey, nil); err != nil {
		t.Fatal(err)
	}

	if len(sessions.added) != 1 {
		t.Fatalf("persisted messages = %d, want 1", len(sessions.added))
	}
	persisted := sessions.added[0]
	if len(persisted.MediaRefs) != 1 {
		t.Fatalf("persisted MediaRefs = %#v, want one image ref", persisted.MediaRefs)
	}
	if strings.Contains(persisted.Content, workspace) {
		t.Fatalf("persisted content leaked workspace path: %q", persisted.Content)
	}
	if !strings.Contains(persisted.Content, `path=".uploads/`) {
		t.Fatalf("persisted content lacks logical image path: %q", persisted.Content)
	}

	nextTurnRefs := collectRefsByKind([]providers.Message{persisted}, nil, "image")
	if len(nextTurnRefs) != 1 || nextTurnRefs[0].ID != persisted.MediaRefs[0].ID {
		t.Fatalf("next-turn refs = %#v, want persisted exact ID", nextTurnRefs)
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

func TestResolveEffectiveContextWindow_UsesAgentConfigOnly(t *testing.T) {
	registry := &panicModelRegistry{}
	loop := &Loop{contextWindow: 128_000, modelRegistry: registry}
	if got := loop.resolveEffectiveContextWindow(); got != 128_000 {
		t.Fatalf("resolveEffectiveContextWindow() = %d, want 128000", got)
	}
}

type panicModelRegistry struct{}

func (*panicModelRegistry) Resolve(_, _ string) *providers.ModelSpec {
	panic("model registry must not participate in request budgeting")
}

func (*panicModelRegistry) Register(providers.ModelSpec) {
	panic("model registry must not participate in request budgeting")
}

func (*panicModelRegistry) Catalog(string) []providers.ModelSpec {
	panic("model registry must not participate in request budgeting")
}

func TestMakeUpdateMetadataStoresLastUsagePromptTokens(t *testing.T) {
	sessions := &nopSessionStore{}
	loop := &Loop{
		model:    "test-model",
		provider: finalThinkingStreamProvider{},
		sessions: sessions,
	}
	req := &RunRequest{Channel: "telegram"}

	update := loop.makeUpdateMetadata(req)
	err := update(context.Background(), "sess-1",
		providers.Usage{PromptTokens: 225000, CompletionTokens: 3000},
		providers.Usage{PromptTokens: 70000, CompletionTokens: 100},
		12,
	)
	if err != nil {
		t.Fatalf("update metadata error: %v", err)
	}
	if sessions.inputTokens != 225000 || sessions.outputTokens != 3000 {
		t.Fatalf("accumulated tokens = %d/%d, want total run 225000/3000", sessions.inputTokens, sessions.outputTokens)
	}
	// Upstream 503909d3 calibration: SetLastPromptTokens stores the final call's
	// full context size (Usage.ContextTokens(), which adds cached segments back
	// for Anthropic-style usage) PLUS the final completion — the reply joins
	// history so it occupies the next request's prompt. No cache tokens here, so
	// ContextTokens()=70000; +100 completion = 70100.
	if sessions.setLastTokens != 70100 || sessions.setLastMsgCount != 12 {
		t.Fatalf("last prompt calibration = %d/%d, want last request 70100/12", sessions.setLastTokens, sessions.setLastMsgCount)
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
