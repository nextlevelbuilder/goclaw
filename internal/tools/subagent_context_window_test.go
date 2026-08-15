package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// callTrackingProvider records whether Chat/ChatStream ran, so a test can prove
// the pre-transport guard blocked BEFORE any transport call.
type callTrackingProvider struct {
	called bool
}

func (p *callTrackingProvider) Name() string         { return "tracking" }
func (p *callTrackingProvider) DefaultModel() string { return "claude-sonnet-4-5-20250929" }
func (p *callTrackingProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	p.called = true
	return &providers.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
}
func (p *callTrackingProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	p.called = true
	return &providers.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
}

func subagentBigMessage(model string, targetChars int) providers.Message {
	return providers.Message{Role: "user", Content: strings.Repeat("word ", targetChars)}
}

// TestSubagentSpawn_CapturesOriginContextWindow proves the Spawn path captures
// the CALLING agent's context window (set in ctx by the agent loop's
// injectContext) into SubagentTask.OriginContextWindow — the value the guard
// later uses. This locks the plumbing the previous round only claimed.
func TestSubagentSpawn_CapturesOriginContextWindow(t *testing.T) {
	provider := &recordingSubagentProvider{}
	manager := NewSubagentManager(provider, nil, "manager-default", nil, NewRegistry, SubagentConfig{
		MaxConcurrent: 4, MaxSpawnDepth: 3, MaxChildrenPerAgent: 8,
	})
	manager.SetAgentBudget(200_000, 32_000) // manager default (would be wrong for a 128k caller)

	// Calling agent configured at 128k/8192, propagated via ctx (as injectContext does).
	ctx := store.WithAgentContextWindow(context.Background(), 128_000)
	ctx = store.WithAgentMaxTokens(ctx, 8_192)
	ctx = store.WithTenantID(ctx, uuid.New())
	ctx = store.WithAgentID(ctx, uuid.New())
	ctx = WithToolAgentKey(ctx, "parent")

	_, _, _, err := manager.RunSync(ctx, "parent", 0, "task", "label", "", "chan", "chat")
	if err != nil {
		t.Fatalf("RunSync error: %v", err)
	}

	// Find the task the manager created and assert it captured the CALLER's
	// 128k/8192 budget, not the manager default 200k/32000.
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if len(manager.tasks) == 0 {
		t.Fatal("no subagent task recorded")
	}
	for _, task := range manager.tasks {
		if task.OriginContextWindow != 128_000 {
			t.Fatalf("OriginContextWindow = %d, want 128000 (calling agent, not manager default)", task.OriginContextWindow)
		}
		if task.OriginMaxTokens != 8_192 {
			t.Fatalf("OriginMaxTokens = %d, want 8192 (calling agent, not manager default)", task.OriginMaxTokens)
		}
	}
}

// TestChatSubagentWithUsageCap_HonorsOriginWindow is the production-path guard
// for finding #4: a subagent request that fits the 200k model window but exceeds
// the calling agent's 128k cap must be blocked before transport.
func TestChatSubagentWithUsageCap_HonorsOriginWindow(t *testing.T) {
	model := "claude-sonnet-4-5-20250929" // 200k model window, cl100k tokenizer
	manager := NewSubagentManager(nil, nil, "manager-default", nil, NewRegistry, SubagentConfig{})
	// Manager default is 200k; the per-task origin window must override it.
	manager.SetAgentBudget(200_000, 32_000)

	// ~140k tokens: fits 200k model window, exceeds a 128k agent cap.
	req := providers.ChatRequest{
		Model:    model,
		Messages: []providers.Message{subagentBigMessage(model, 140_000)},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}

	// Task from a 128k-configured calling agent.
	task128 := &SubagentTask{ID: "t128", OriginContextWindow: 128_000, OriginMaxTokens: 8_192}
	prov := &callTrackingProvider{}
	_, err := manager.chatSubagentWithUsageCap(context.Background(), task128, prov, model, req, 0, 0)
	if err == nil {
		t.Fatal("expected abort: 140k request exceeds 128k origin window")
	}
	if prov.called {
		t.Fatal("provider must NOT be called when the guard aborts (transport calls must be 0)")
	}
	var ctxErr *usagecaps.ContextWindowExceededError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextWindowExceededError, got %T: %v", err, err)
	}
	if ctxErr.ContextWindow != 128_000 {
		t.Fatalf("guard used window %d, want 128000 (origin, not model/manager)", ctxErr.ContextWindow)
	}
}

// TestChatSubagentWithUsageCap_TwoAgentsIsolated proves two subagents from
// agents configured at 128k and 200k are guarded independently by ONE shared
// manager: the same 140k request is blocked for the 128k caller but allowed for
// the 200k caller.
func TestChatSubagentWithUsageCap_TwoAgentsIsolated(t *testing.T) {
	model := "claude-sonnet-4-5-20250929"
	manager := NewSubagentManager(nil, nil, "manager-default", nil, NewRegistry, SubagentConfig{})
	manager.SetAgentBudget(200_000, 32_000)

	req := providers.ChatRequest{
		Model:    model,
		Messages: []providers.Message{subagentBigMessage(model, 140_000)},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}

	// 128k caller: blocked.
	prov128 := &callTrackingProvider{}
	if _, err := manager.chatSubagentWithUsageCap(context.Background(), &SubagentTask{ID: "a128", OriginContextWindow: 128_000, OriginMaxTokens: 8_192}, prov128, model, req, 0, 0); err == nil {
		t.Fatal("128k caller: expected abort for 140k request")
	}
	if prov128.called {
		t.Fatal("128k caller: provider must not be called")
	}

	// 200k caller: allowed (140k < 200k), provider reached.
	prov200 := &callTrackingProvider{}
	if _, err := manager.chatSubagentWithUsageCap(context.Background(), &SubagentTask{ID: "a200", OriginContextWindow: 200_000, OriginMaxTokens: 8_192}, prov200, model, req, 0, 0); err != nil {
		t.Fatalf("200k caller: expected allow for 140k request, got %v", err)
	}
	if !prov200.called {
		t.Fatal("200k caller: provider should have been called")
	}
}
