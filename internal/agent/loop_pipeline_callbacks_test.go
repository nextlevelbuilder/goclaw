package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

type pipelineRecordingSessionStore struct {
	*nopSessionStore
	added []providers.Message
}

func (s *pipelineRecordingSessionStore) AddMessage(_ context.Context, _ string, msg providers.Message) {
	s.added = append(s.added, msg)
}

// retryOnceProvider fails the first Chat call with a transient 429 via RetryDo
// (which fires the injected retry hook), then succeeds — mimicking a flaky
// provider whose internal RetryDo consumes the run's retry hook.
type retryOnceProvider struct {
	calls int
	cfg   providers.RetryConfig
}

func (p *retryOnceProvider) Chat(ctx context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return providers.RetryDo(ctx, p.cfg, func() (*providers.ChatResponse, error) {
		p.calls++
		if p.calls == 1 {
			return nil, &providers.HTTPError{Status: 429, Body: "rate limited"}
		}
		return &providers.ChatResponse{Content: "ok"}, nil
	})
}

func (p *retryOnceProvider) ChatStream(ctx context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, providers.ChatRequest{})
}

func (p *retryOnceProvider) DefaultModel() string { return "test-model" }
func (p *retryOnceProvider) Name() string         { return "test-provider" }

func TestMakeCallLLMEmitsRetryingOnTransientProviderError(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}
	req := &RunRequest{RunID: "run-1", SessionKey: "sess-1", Channel: "telegram"}
	state := &pipeline.RunState{
		Provider: &retryOnceProvider{
			cfg: providers.RetryConfig{Attempts: 2, MinDelay: time.Millisecond, MaxDelay: time.Millisecond},
		},
		Model:     "test-model",
		Iteration: 0,
	}

	resp, err := loop.makeCallLLM(req, col.onEvent)(context.Background(), state, providers.ChatRequest{})
	if err != nil {
		t.Fatalf("makeCallLLM returned error: %v", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	retrying := col.filter(protocol.AgentEventRunRetrying)
	if len(retrying) != 1 {
		t.Fatalf("retrying events = %+v, want exactly one", retrying)
	}
	payload, ok := retrying[0].Payload.(map[string]string)
	if !ok || payload["attempt"] != "1" || payload["maxAttempts"] != "2" {
		t.Fatalf("retrying payload = %+v, want attempt=1 maxAttempts=2", retrying[0].Payload)
	}
}

type enforcementErrorThenFallbackProvider struct {
	requests     []providers.ChatRequest
	hasDeadlines []bool
}

func (p *enforcementErrorThenFallbackProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	_, hasDeadline := ctx.Deadline()
	p.requests = append(p.requests, req)
	p.hasDeadlines = append(p.hasDeadlines, hasDeadline)
	if len(p.requests) == 1 {
		return nil, errors.New("enforcement provider failed")
	}
	return &providers.ChatResponse{Content: "direct answer", FinishReason: "stop"}, nil
}

func (p *enforcementErrorThenFallbackProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func (p *enforcementErrorThenFallbackProvider) DefaultModel() string { return "test-model" }
func (p *enforcementErrorThenFallbackProvider) Name() string         { return "test-provider" }

// enforcementTransientErrorProvider fails the first enforcement call with a
// transient error, then honours the directive on the retry.
type enforcementTransientErrorProvider struct {
	firstErr     error
	requests     []providers.ChatRequest
	hasDeadlines []bool
	deadlineIn   []time.Duration
}

func (p *enforcementTransientErrorProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	deadline, hasDeadline := ctx.Deadline()
	p.requests = append(p.requests, req)
	p.hasDeadlines = append(p.hasDeadlines, hasDeadline)
	if hasDeadline {
		p.deadlineIn = append(p.deadlineIn, time.Until(deadline))
	} else {
		p.deadlineIn = append(p.deadlineIn, 0)
	}
	if len(p.requests) == 1 {
		return nil, p.firstErr
	}
	return &providers.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []providers.ToolCall{
			{ID: "c1", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "launch"}},
		},
	}, nil
}

func (p *enforcementTransientErrorProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func (p *enforcementTransientErrorProvider) DefaultModel() string { return "test-model" }
func (p *enforcementTransientErrorProvider) Name() string         { return "test-provider" }

type enforcementRetryThenFallbackProvider struct {
	requests []providers.ChatRequest
}

func (p *enforcementRetryThenFallbackProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.requests = append(p.requests, req)
	if len(p.requests) < 3 {
		return &providers.ChatResponse{Content: "text-only", FinishReason: "stop"}, nil
	}
	return &providers.ChatResponse{Content: "direct answer", FinishReason: "stop"}, nil
}

func (p *enforcementRetryThenFallbackProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func (p *enforcementRetryThenFallbackProvider) DefaultModel() string { return "test-model" }
func (p *enforcementRetryThenFallbackProvider) Name() string         { return "test-provider" }

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
	sessions := &pipelineRecordingSessionStore{nopSessionStore: &nopSessionStore{}}
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

func TestMakeCallLLM_EnforcementErrorFallsBackWithParentContextAndProfessionalTools(t *testing.T) {
	provider := &enforcementErrorThenFallbackProvider{}
	loop := &Loop{id: "test-agent"}
	req := &RunRequest{
		RunID:      "run-1",
		SessionKey: "sess-1",
		Channel:    "ws",
		TeamWorkDirective: &TeamWorkDirective{
			Mode:            "team",
			OriginalMessage: "research this",
			RequiredTool:    "team_tasks",
			TeamRole:        "lead",
		},
	}
	state := &pipeline.RunState{Provider: provider, Model: "test-model"}
	chatReq := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: "base prompt\n\n## TEAM WORK ROUTING LOCK\ninternal lock"},
			{Role: "user", Content: "research this"},
		},
		Tools: []providers.ToolDefinition{
			{Function: &providers.ToolFunctionSchema{Name: "team_tasks"}},
			{Function: &providers.ToolFunctionSchema{Name: "web_search"}},
		},
	}

	resp, err := loop.makeCallLLM(req, func(AgentEvent) {})(context.Background(), state, chatReq)
	if err != nil {
		t.Fatalf("makeCallLLM returned fallback error: %v", err)
	}
	if resp == nil || resp.Content != "direct answer" {
		t.Fatalf("fallback response = %+v, want direct answer", resp)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want enforcement + self fallback", len(provider.requests))
	}
	if !provider.hasDeadlines[0] || provider.hasDeadlines[1] {
		t.Fatalf("call deadlines = %v, want child deadline then live parent context", provider.hasDeadlines)
	}
	if len(provider.requests[0].Tools) != 1 || provider.requests[0].Tools[0].Function.Name != "team_tasks" {
		t.Fatalf("enforcement tools = %+v, want only team_tasks", provider.requests[0].Tools)
	}
	if got := provider.requests[0].Options[providers.OptAllowedToolNames]; !allowedToolNamesEqual(got, []string{"team_tasks"}) {
		t.Fatalf("enforcement allowed tool names = %#v, want [team_tasks]", got)
	}
	if len(provider.requests[1].Tools) != 1 || provider.requests[1].Tools[0].Function.Name != "web_search" {
		t.Fatalf("fallback tools = %+v, want professional tools with orchestration removed", provider.requests[1].Tools)
	}
	if got := provider.requests[1].Options[providers.OptAllowedToolNames]; !allowedToolNamesEqual(got, []string{"web_search"}) {
		t.Fatalf("fallback allowed tool names = %#v, want [web_search]", got)
	}
	for _, msg := range provider.requests[1].Messages {
		if strings.Contains(msg.Content, "TEAM WORK ROUTING LOCK") {
			t.Fatalf("fallback retained routing lock: %q", msg.Content)
		}
	}
}

func TestMakeCallLLM_EnforcementRetryRefreshesAllowedToolNames(t *testing.T) {
	provider := &enforcementRetryThenFallbackProvider{}
	loop := &Loop{id: "test-agent"}
	req := &RunRequest{
		RunID:      "run-retry",
		SessionKey: "sess-retry",
		Channel:    "ws",
		TeamWorkDirective: &TeamWorkDirective{
			Mode:            "team",
			OriginalMessage: "research this",
			RequiredTool:    "team_tasks",
			TeamRole:        "lead",
		},
	}
	state := &pipeline.RunState{Provider: provider, Model: "test-model"}
	chatReq := providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "research this"}},
		Tools: []providers.ToolDefinition{
			{Function: &providers.ToolFunctionSchema{Name: "team_tasks"}},
			{Function: &providers.ToolFunctionSchema{Name: "web_search"}},
		},
	}

	if _, err := loop.makeCallLLM(req, func(AgentEvent) {})(context.Background(), state, chatReq); err != nil {
		t.Fatalf("makeCallLLM returned error: %v", err)
	}
	if len(provider.requests) != 3 {
		t.Fatalf("provider calls = %d, want initial + retry + fallback", len(provider.requests))
	}
	for i, request := range provider.requests[:2] {
		if got := request.Options[providers.OptAllowedToolNames]; !allowedToolNamesEqual(got, []string{"team_tasks"}) {
			t.Fatalf("directive request %d allowed tool names = %#v, want [team_tasks]", i, got)
		}
	}
	if got := provider.requests[2].Options[providers.OptAllowedToolNames]; !allowedToolNamesEqual(got, []string{"web_search"}) {
		t.Fatalf("fallback allowed tool names = %#v, want [web_search]", got)
	}
}

func allowedToolNamesEqual(got any, want []string) bool {
	names, ok := got.([]string)
	if !ok || len(names) != len(want) {
		return false
	}
	for i := range want {
		if names[i] != want[i] {
			return false
		}
	}
	return true
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

// A transient provider failure on an enforcement call must NOT cost the run its
// validated plan. Live regression: a khanh-developer multi_role turn whose gate
// had approved a 3-step plan lost it when the second enforcement call hit the
// hard-coded 30s deadline — the run silently demoted to a solo answer and no
// team_workflows row was ever created.
func TestMakeCallLLM_EnforcementTransientErrorRetriesBeforeFailingClosed(t *testing.T) {
	provider := &enforcementTransientErrorProvider{firstErr: context.DeadlineExceeded}
	loop := &Loop{id: "test-agent"}
	req := &RunRequest{
		RunID:      "run-transient",
		SessionKey: "sess-transient",
		Channel:    "ws",
		TeamWorkDirective: &TeamWorkDirective{
			Mode:            "team",
			OriginalMessage: "plan the launch",
			RequiredTool:    "team_tasks",
			TeamRole:        "lead",
		},
	}
	state := &pipeline.RunState{Provider: provider, Model: "test-model"}
	chatReq := providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "plan the launch"}},
		Tools: []providers.ToolDefinition{
			{Function: &providers.ToolFunctionSchema{Name: "team_tasks"}},
			{Function: &providers.ToolFunctionSchema{Name: "web_search"}},
		},
	}

	resp, err := loop.makeCallLLM(req, func(AgentEvent) {})(context.Background(), state, chatReq)
	if err != nil {
		t.Fatalf("makeCallLLM returned error: %v", err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want failed enforcement + one retry", len(provider.requests))
	}
	if state.TeamWorkDisabled {
		t.Fatal("a retry that satisfied the directive must not disable team work")
	}
	if resp == nil || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "team_tasks" {
		t.Fatalf("response = %+v, want the team_tasks search call from the retry", resp)
	}
	// The retry must still be enforcement-shaped: team_tasks only.
	if len(provider.requests[1].Tools) != 1 || provider.requests[1].Tools[0].Function.Name != "team_tasks" {
		t.Fatalf("retry tools = %+v, want only team_tasks", provider.requests[1].Tools)
	}
}

// A deterministic failure must NOT be retried — it fails identically the second
// time and only delays the user's answer.
func TestMakeCallLLM_EnforcementDeterministicErrorFailsClosedWithoutRetry(t *testing.T) {
	provider := &enforcementTransientErrorProvider{
		firstErr: &providers.HTTPError{Status: 401, Body: "invalid api key"},
	}
	loop := &Loop{id: "test-agent"}
	req := &RunRequest{
		RunID:      "run-auth",
		SessionKey: "sess-auth",
		Channel:    "ws",
		TeamWorkDirective: &TeamWorkDirective{
			Mode:            "team",
			OriginalMessage: "plan the launch",
			RequiredTool:    "team_tasks",
			TeamRole:        "lead",
		},
	}
	state := &pipeline.RunState{Provider: provider, Model: "test-model"}
	chatReq := providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "plan the launch"}},
		Tools: []providers.ToolDefinition{
			{Function: &providers.ToolFunctionSchema{Name: "team_tasks"}},
			{Function: &providers.ToolFunctionSchema{Name: "web_search"}},
		},
	}

	if _, err := loop.makeCallLLM(req, func(AgentEvent) {})(context.Background(), state, chatReq); err != nil {
		t.Fatalf("makeCallLLM returned error: %v", err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want failed enforcement + self fallback (no enforcement retry)", len(provider.requests))
	}
	if !state.TeamWorkDisabled {
		t.Fatal("a deterministic enforcement failure must fail closed to a self run")
	}
	// Call 2 is the SELF FALLBACK, not an enforcement retry: professional tools
	// with orchestration removed.
	if len(provider.requests[1].Tools) != 1 || provider.requests[1].Tools[0].Function.Name != "web_search" {
		t.Fatalf("second call tools = %+v, want the self fallback's professional tools", provider.requests[1].Tools)
	}
}

// The configured Team Work budget must bound the enforcement call, replacing the
// hard-coded default.
func TestMakeCallLLM_EnforcementUsesConfiguredTimeout(t *testing.T) {
	provider := &enforcementTransientErrorProvider{firstErr: context.DeadlineExceeded}
	loop := &Loop{id: "test-agent"}
	req := &RunRequest{
		RunID:      "run-budget",
		SessionKey: "sess-budget",
		Channel:    "ws",
		TeamWorkDirective: &TeamWorkDirective{
			Mode:               "team",
			OriginalMessage:    "plan the launch",
			RequiredTool:       "team_tasks",
			TeamRole:           "lead",
			EnforcementTimeout: 120 * time.Second,
		},
	}
	state := &pipeline.RunState{Provider: provider, Model: "test-model"}
	chatReq := providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "plan the launch"}},
		Tools:    []providers.ToolDefinition{{Function: &providers.ToolFunctionSchema{Name: "team_tasks"}}},
	}

	if _, err := loop.makeCallLLM(req, func(AgentEvent) {})(context.Background(), state, chatReq); err != nil {
		t.Fatalf("makeCallLLM returned error: %v", err)
	}
	if len(provider.deadlineIn) == 0 || !provider.hasDeadlines[0] {
		t.Fatalf("enforcement call had no deadline: %v", provider.hasDeadlines)
	}
	// Well past the 30s default, comfortably under the configured 120s.
	if provider.deadlineIn[0] <= 100*time.Second {
		t.Fatalf("enforcement deadline = %v, want the configured ~120s budget", provider.deadlineIn[0])
	}
}
