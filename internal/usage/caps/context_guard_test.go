package caps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// stubGuardProvider records whether Chat was invoked and returns a canned reply.
type stubGuardProvider struct {
	called bool
}

func (p *stubGuardProvider) Name() string         { return "stubguard" }
func (p *stubGuardProvider) DefaultModel() string { return "claude-3-5-sonnet" }
func (p *stubGuardProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	p.called = true
	return &providers.ChatResponse{Content: "ok"}, nil
}
func (p *stubGuardProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	p.called = true
	return &providers.ChatResponse{Content: "ok"}, nil
}

// bigMessage returns a user message whose complete-input count is at least
// target tokens, using the fixed model/provider-independent budget counter.
func bigMessage(t *testing.T, target int) providers.Message {
	t.Helper()
	low, high := 1, target*8
	for low < high {
		mid := low + (high-low)/2
		msg := providers.Message{Role: "user", Content: strings.Repeat("word ", mid)}
		count, err := contextGuardCounter.CountMessages([]providers.Message{msg})
		if err != nil {
			t.Fatal(err)
		}
		if count < target {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return providers.Message{Role: "user", Content: strings.Repeat("word ", low)}
}

// TestCapsChat_NonAgentCallSkipsWindowGuard proves a call with no AgentID and no
// agent budget is NOT forced against any model window: it passes straight to the
// provider even when huge. Model window is not a budget authority.
func TestCapsChat_NonAgentCallSkipsWindowGuard(t *testing.T) {
	var svc *Service // nil service => Lite path
	prov := &stubGuardProvider{}
	// ~200k tokens: would blow any model window, but this is not agent-scoped.
	msg := bigMessage(t, 200_000)
	req := providers.ChatRequest{
		Model:    "gpt-4o",
		Messages: []providers.Message{msg},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}
	if _, err := svc.Chat(context.Background(), prov, req, ChatOptions{Purpose: "non-agent"}); err != nil {
		t.Fatalf("non-agent call must not be window-guarded, got %v", err)
	}
	if !prov.called {
		t.Fatal("provider.Chat should have been called for a non-agent call")
	}
}

// TestCapsChat_AgentMissingBudgetUsesSafeDefault proves an agent-scoped call via
// Service.Chat (AgentID set) that reaches the gate WITHOUT a wired budget does
// NOT fail closed with a wiring error. These are background/utility calls (e.g.
// vault.classify) that run outside an agent turn; failing closed silently
// degrades enrichment. Instead a safe default budget is filled and the call
// reaches transport. NOTE: the agent-RUN tool path guards via GuardContextWindow
// directly (see internal/tools/usage_caps.go) and still fails closed — this
// relaxation is scoped to Service.Chat only.
func TestCapsChat_AgentMissingBudgetUsesSafeDefault(t *testing.T) {
	var svc *Service
	prov := &stubGuardProvider{}
	req := providers.ChatRequest{
		Model:    "gpt-4o",
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
		Options:  map[string]any{providers.OptMaxTokens: 1024},
	}
	_, err := svc.Chat(context.Background(), prov, req, ChatOptions{
		AgentID: uuid.New(),
		Purpose: "vault.classify",
	})
	if err != nil {
		var wiringErr *AgentBudgetWiringError
		if errors.As(err, &wiringErr) {
			t.Fatalf("background call must not fail closed with a wiring error: %v", err)
		}
		t.Fatalf("unexpected error from safe-default background call: %v", err)
	}
	if !prov.called {
		t.Fatal("provider.Chat should have been called after filling a safe default budget")
	}
}

// TestCapsChat_AgentBudgetSameRequestDiffersByWindow is the core agent-only
// contract test: the SAME request is blocked for a 128k agent and allowed for a
// 200k agent, decided only by the agent's configured window, never the model's.
func TestCapsChat_AgentBudgetSameRequestDiffersByWindow(t *testing.T) {
	var svc *Service
	agentID := uuid.New()
	// ~140k tokens: fits a 200k window, exceeds a 128k window.
	msg := bigMessage(t, 140_000)
	req := providers.ChatRequest{
		Model:    "gpt-4o", // 128k model window — irrelevant to the decision
		Messages: []providers.Message{msg},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}

	// 128k agent: blocked.
	prov128 := &stubGuardProvider{}
	_, err := svc.Chat(context.Background(), prov128, req, ChatOptions{
		AgentID:            agentID,
		Purpose:            "cap-128k",
		AgentContextWindow: 128_000,
		AgentMaxTokens:     8_192,
	})
	if err == nil {
		t.Fatal("128k agent: expected abort for 140k request")
	}
	if prov128.called {
		t.Fatal("128k agent: provider must NOT be called when the guard aborts (transport calls must be 0)")
	}
	var ctxErr *ContextWindowExceededError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextWindowExceededError, got %T: %v", err, err)
	}
	if ctxErr.ContextWindow != 128_000 {
		t.Fatalf("guard reported window %d, want 128000 (agent window, not model)", ctxErr.ContextWindow)
	}

	// 200k agent: allowed, provider reached.
	prov200 := &stubGuardProvider{}
	if _, err := svc.Chat(context.Background(), prov200, req, ChatOptions{
		AgentID:            agentID,
		Purpose:            "cap-200k",
		AgentContextWindow: 200_000,
		AgentMaxTokens:     8_192,
	}); err != nil {
		t.Fatalf("200k agent: expected allow for 140k request, got %v", err)
	}
	if !prov200.called {
		t.Fatal("200k agent: provider should have been called")
	}
}
