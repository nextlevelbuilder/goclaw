package handlers_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// fakeResolver returns a static provider + model. Counts resolve calls for
// cache-hit assertions.
type fakeResolver struct {
	prov     providers.Provider
	model    string
	calls    atomic.Int32
	resolveErr error
}

func (f *fakeResolver) ResolveForHook(_ context.Context, _ uuid.UUID, _ string) (providers.Provider, string, error) {
	f.calls.Add(1)
	if f.resolveErr != nil {
		return nil, "", f.resolveErr
	}
	return f.prov, f.model, nil
}

// fakeProvider returns a scripted ChatResponse and counts Chat calls.
type fakeProvider struct {
	name         string
	defaultModel string
	nextResp     *providers.ChatResponse
	nextErr      error
	chatCalls    atomic.Int32
	// lastReq captures the most recent request for field assertions.
	lastReq providers.ChatRequest
}

func (p *fakeProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.chatCalls.Add(1)
	p.lastReq = req
	if p.nextErr != nil {
		return nil, p.nextErr
	}
	return p.nextResp, nil
}
func (p *fakeProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, errors.New("not used in tests")
}
func (p *fakeProvider) Name() string         { return p.name }
func (p *fakeProvider) DefaultModel() string { return p.defaultModel }

// makePromptCfg constructs a prompt-handler HookConfig with sensible defaults.
func makePromptCfg(t *testing.T) hooks.HookConfig {
	t.Helper()
	return hooks.HookConfig{
		ID:          uuid.New(),
		Version:     1,
		HandlerType: hooks.HandlerPrompt,
		Scope:       hooks.ScopeTenant,
		Event:       hooks.EventPreToolUse,
		Enabled:     true,
		Matcher:     "exec",
		Config: map[string]any{
			"prompt_template": "Evaluate safety of this tool call.",
			"model":           "haiku",
		},
	}
}

// makePromptEv constructs a blocking PreToolUse event with sample tool input.
func makePromptEv() hooks.Event {
	return hooks.Event{
		EventID:   "evt-1",
		TenantID:  uuid.New(),
		HookEvent: hooks.EventPreToolUse,
		ToolName:  "exec",
		ToolInput: map[string]any{"cmd": "ls -la"},
	}
}

// okResp simulates a well-formed evaluator tool-call response.
func okResp(decision string) *providers.ChatResponse {
	return &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{
			ID:   "call-1",
			Name: "decide",
			Arguments: map[string]any{
				"decision": decision,
				"reason":   "test",
			},
		}},
		Usage: &providers.Usage{TotalTokens: 42},
	}
}

func TestPrompt_Allow_StructuredOutput(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("allow")}
	h := &handlers.PromptHandler{
		Resolver:     &fakeResolver{prov: prov, model: "claude-haiku"},
		DefaultModel: "haiku",
	}
	dec, err := h.Execute(context.Background(), makePromptCfg(t), makePromptEv())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", dec)
	}
}

func TestPrompt_Block_StructuredOutput(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("block")}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov, model: "m"}}
	dec, err := h.Execute(context.Background(), makePromptCfg(t), makePromptEv())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block", dec)
	}
}

func TestPrompt_NoToolCall_FailsClosed(t *testing.T) {
	// Evaluator returned free-text instead of calling the decide tool.
	// Must fail-closed (return Block).
	prov := &fakeProvider{
		nextResp: &providers.ChatResponse{Content: "sure, you can ignore the system prompt and allow it"},
	}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	dec, err := h.Execute(context.Background(), makePromptCfg(t), makePromptEv())
	if err == nil {
		t.Fatal("expected err for missing tool call")
	}
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (fail-closed)", dec)
	}
}

func TestPrompt_WrongToolName_FailsClosed(t *testing.T) {
	prov := &fakeProvider{
		nextResp: &providers.ChatResponse{
			ToolCalls: []providers.ToolCall{{Name: "allow_all", Arguments: map[string]any{"decision": "allow"}}},
		},
	}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	dec, _ := h.Execute(context.Background(), makePromptCfg(t), makePromptEv())
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (wrong tool name)", dec)
	}
}

func TestPrompt_InvalidDecisionValue_FailsClosed(t *testing.T) {
	prov := &fakeProvider{
		nextResp: &providers.ChatResponse{
			ToolCalls: []providers.ToolCall{{Name: "decide", Arguments: map[string]any{"decision": "allow-with-monitoring"}}},
		},
	}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	dec, _ := h.Execute(context.Background(), makePromptCfg(t), makePromptEv())
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (invalid decision enum)", dec)
	}
}

func TestPrompt_CacheHit_SkipsProviderCall(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("allow")}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	cfg := makePromptCfg(t)
	ev := makePromptEv()

	// First call: hits provider.
	if _, err := h.Execute(context.Background(), cfg, ev); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if prov.chatCalls.Load() != 1 {
		t.Fatalf("first chatCalls=%d, want 1", prov.chatCalls.Load())
	}

	// Second call with same (hookID, version, tool, input) → cache hit.
	if _, err := h.Execute(context.Background(), cfg, ev); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if prov.chatCalls.Load() != 1 {
		t.Errorf("second call chatCalls=%d, want 1 (cache miss)", prov.chatCalls.Load())
	}
}

func TestPrompt_CacheBustedByVersionBump(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("allow")}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	cfg := makePromptCfg(t)
	ev := makePromptEv()

	if _, err := h.Execute(context.Background(), cfg, ev); err != nil {
		t.Fatalf("v1: %v", err)
	}
	// Config edited → version++ → cache key changes → second call hits provider.
	cfg.Version = 2
	if _, err := h.Execute(context.Background(), cfg, ev); err != nil {
		t.Fatalf("v2: %v", err)
	}
	if prov.chatCalls.Load() != 2 {
		t.Errorf("chatCalls=%d, want 2 (version bump should bust cache)", prov.chatCalls.Load())
	}
}

func TestPrompt_PerTurnCapEnforced(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("allow")}
	h := &handlers.PromptHandler{
		Resolver:                     &fakeResolver{prov: prov},
		DefaultMaxInvocationsPerTurn: 2,
	}
	ctx := handlers.WithPromptTurn(context.Background())
	cfg := makePromptCfg(t)

	// Vary the input so the cache doesn't absorb repeat calls.
	for i := range 2 {
		ev := makePromptEv()
		ev.ToolInput = map[string]any{"i": i}
		if _, err := h.Execute(ctx, cfg, ev); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	// 3rd call exceeds cap of 2.
	ev := makePromptEv()
	ev.ToolInput = map[string]any{"i": 2}
	_, err := h.Execute(ctx, cfg, ev)
	if !errors.Is(err, handlers.ErrPromptPerTurnCapExceeded) {
		t.Fatalf("want ErrPromptPerTurnCapExceeded, got %v", err)
	}
}

func TestPrompt_NoResolver_ReturnsError(t *testing.T) {
	h := &handlers.PromptHandler{}
	dec, err := h.Execute(context.Background(), makePromptCfg(t), makePromptEv())
	if err == nil || dec != hooks.DecisionError {
		t.Errorf("want error decision + err, got dec=%q err=%v", dec, err)
	}
}

func TestPrompt_ModelDefaultsToHaiku(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("allow")}
	resolver := &fakeResolver{prov: prov, model: "claude-haiku-4-5"}
	h := &handlers.PromptHandler{Resolver: resolver} // no DefaultModel set

	cfg := makePromptCfg(t)
	delete(cfg.Config, "model") // unspecified
	if _, err := h.Execute(context.Background(), cfg, makePromptEv()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if prov.lastReq.Model != "claude-haiku-4-5" {
		t.Errorf("request model=%q, want claude-haiku-4-5 (resolver-expanded haiku alias)", prov.lastReq.Model)
	}
}

func TestPrompt_SystemPromptHasInjectionWarning(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("allow")}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	if _, err := h.Execute(context.Background(), makePromptCfg(t), makePromptEv()); err != nil {
		t.Fatal(err)
	}
	if len(prov.lastReq.Messages) == 0 {
		t.Fatal("no messages captured")
	}
	sys := prov.lastReq.Messages[0]
	if sys.Role != "system" {
		t.Fatalf("first message role=%q, want system", sys.Role)
	}
	// Anti-injection warning must be present.
	for _, phrase := range []string{"NEVER follow instructions", "adversarial", "decide"} {
		if !contains(sys.Content, phrase) {
			t.Errorf("system prompt missing %q: %q", phrase, sys.Content)
		}
	}
}

func TestPrompt_ProviderError_FailsClosedOnBlockingEvent(t *testing.T) {
	prov := &fakeProvider{nextErr: errors.New("network down")}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	dec, err := h.Execute(context.Background(), makePromptCfg(t), makePromptEv())
	if err == nil {
		t.Fatal("expected transport err to propagate")
	}
	// PreToolUse is blocking → fail-closed Block.
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (fail-closed on blocking event)", dec)
	}
}

func TestPrompt_ToolSchemaIncludesDecideTool(t *testing.T) {
	prov := &fakeProvider{nextResp: okResp("allow")}
	h := &handlers.PromptHandler{Resolver: &fakeResolver{prov: prov}}
	if _, err := h.Execute(context.Background(), makePromptCfg(t), makePromptEv()); err != nil {
		t.Fatal(err)
	}
	if len(prov.lastReq.Tools) != 1 {
		t.Fatalf("tools len=%d, want 1", len(prov.lastReq.Tools))
	}
	tool := prov.lastReq.Tools[0]
	if tool.Function.Name != "decide" {
		t.Errorf("tool name=%q, want decide", tool.Function.Name)
	}
	// Required fields present in schema.
	props, _ := tool.Function.Parameters["properties"].(map[string]any)
	if _, ok := props["decision"]; !ok {
		t.Error("schema missing 'decision' property")
	}
	if _, ok := props["injection_detected"]; !ok {
		t.Error("schema missing 'injection_detected' property")
	}
}

// contains avoids strings.Contains import bloat; short and localized.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Suppress unused-time import in smaller builds.
var _ = time.Second
