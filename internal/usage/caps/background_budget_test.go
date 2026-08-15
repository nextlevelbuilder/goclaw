package caps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// A background/utility LLM call (carries an agent ID but no wired per-agent
// budget — e.g. vault.classify / vault.batch_summarize) must NOT fail closed
// with an AgentBudgetWiringError. It should fall through to a safe default
// budget and reach the provider. Regression guard for the vault-enrichment
// break introduced when the agent-only guard was added.
func TestServiceChat_BackgroundBudgetFallback_ReachesProvider(t *testing.T) {
	// nil store service: falls back to a direct provider.Chat after the guard,
	// isolating the budget-wiring behaviour from usage-cap policy plumbing.
	var svc *Service
	provider := &fakeChatProvider{name: "openrouter", model: "token/model"}

	ctx := store.WithAgentID(context.Background(), uuid.New()) // agent-scoped, but NO window/max_tokens wired
	_, err := svc.Chat(ctx, provider, providers.ChatRequest{
		Model:    "token/model",
		Messages: []providers.Message{{Role: "user", Content: "classify this short doc"}},
		Options:  map[string]any{providers.OptMaxTokens: 4096},
	}, ChatOptions{
		AgentID:         uuid.New(),
		ProviderName:    "openrouter",
		Purpose:         "vault.classify",
		MaxOutputTokens: 4096,
	})
	if err != nil {
		var wiringErr *AgentBudgetWiringError
		if errors.As(err, &wiringErr) {
			t.Fatalf("background call must not fail closed with a wiring error: %v", err)
		}
		t.Fatalf("background call returned unexpected error: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (background call must reach transport)", provider.calls)
	}
}

// The fallback is a SAFE default, not a bypass: a background call whose real
// input exceeds the default window is still aborted before transport by the
// window guard. Proves fallback != "guess a model window and wave it through".
func TestServiceChat_BackgroundBudgetFallback_StillGuardsOversizedInput(t *testing.T) {
	var svc *Service
	provider := &fakeChatProvider{name: "openrouter", model: "token/model"}

	// Force a tiny window via opts so the guard has a small ceiling; leave
	// max_tokens unwired so the fallback fills only that half.
	huge := strings.Repeat("token ", 60_000) // well over a small window once counted
	ctx := store.WithAgentID(context.Background(), uuid.New())
	_, err := svc.Chat(ctx, provider, providers.ChatRequest{
		Model:    "token/model",
		Messages: []providers.Message{{Role: "user", Content: huge}},
		Options:  map[string]any{providers.OptMaxTokens: 1024},
	}, ChatOptions{
		AgentID:            uuid.New(),
		ProviderName:       "openrouter",
		Purpose:            "vault.batch_summarize",
		AgentContextWindow: 8_000, // operator-provided window is preserved by the fallback
	})
	if err == nil {
		t.Fatal("expected the window guard to abort an oversized background request")
	}
	var ctxErr *ContextWindowExceededError
	if !errors.As(err, &ctxErr) {
		t.Fatalf("expected *ContextWindowExceededError, got %T: %v", err, err)
	}
	if ctxErr.ContextWindow != 8_000 {
		t.Fatalf("guard used window %d, want 8000 (operator value preserved, not defaulted)", ctxErr.ContextWindow)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0 (oversized request must not reach transport)", provider.calls)
	}
}

// A non-agent-scoped call (no agent ID anywhere) must be untouched by the
// fallback path — it is neither guarded nor defaulted, matching prior behaviour.
func TestServiceChat_NonAgentScoped_NoFallbackNoGuard(t *testing.T) {
	var svc *Service
	provider := &fakeChatProvider{name: "openrouter", model: "token/model"}

	huge := strings.Repeat("token ", 60_000)
	_, err := svc.Chat(context.Background(), provider, providers.ChatRequest{
		Model:    "token/model",
		Messages: []providers.Message{{Role: "user", Content: huge}},
		Options:  map[string]any{providers.OptMaxTokens: 1024},
	}, ChatOptions{
		ProviderName: "openrouter",
		Purpose:      "system.utility",
	})
	if err != nil {
		t.Fatalf("non-agent-scoped call must pass through untouched, got %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
}

// backgroundBudgetFallback fills only the missing halves and preserves any
// operator-provided value; it never lets the reserve meet/exceed the window.
func TestBackgroundBudgetFallback_FillsOnlyMissing(t *testing.T) {
	// Both missing: window defaulted, max_tokens taken from opts.MaxOutputTokens.
	got := backgroundBudgetFallback("vault.classify", AgentBudget{}, ChatOptions{MaxOutputTokens: 4096})
	if got.ContextWindow != defaultBackgroundContextWindow {
		t.Errorf("ContextWindow = %d, want %d", got.ContextWindow, defaultBackgroundContextWindow)
	}
	if got.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096 (from opts.MaxOutputTokens)", got.MaxTokens)
	}

	// opts has no max_tokens either: falls back to the package default.
	got = backgroundBudgetFallback("vault.classify", AgentBudget{}, ChatOptions{})
	if got.MaxTokens != defaultBackgroundMaxTokens {
		t.Errorf("MaxTokens = %d, want %d (package default)", got.MaxTokens, defaultBackgroundMaxTokens)
	}

	// Operator window preserved; only max_tokens defaulted.
	got = backgroundBudgetFallback("vault.classify", AgentBudget{ContextWindow: 32_000}, ChatOptions{MaxOutputTokens: 2048})
	if got.ContextWindow != 32_000 {
		t.Errorf("ContextWindow = %d, want 32000 (preserved)", got.ContextWindow)
	}
	if got.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048", got.MaxTokens)
	}

	// Reserve must never meet/exceed the window.
	got = backgroundBudgetFallback("vault.classify", AgentBudget{ContextWindow: 1000, MaxTokens: 4000}, ChatOptions{})
	if got.MaxTokens >= got.ContextWindow {
		t.Errorf("MaxTokens %d must be < ContextWindow %d", got.MaxTokens, got.ContextWindow)
	}
}
