package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// searchStubProvider returns a fixed result so we can assert which provider ran.
type searchStubProvider struct {
	name    string
	called  *bool
	result  searchResult
	failure error
}

func (s *searchStubProvider) Name() string { return s.name }
func (s *searchStubProvider) Search(_ context.Context, _ searchParams) ([]searchResult, error) {
	if s.called != nil {
		*s.called = true
	}
	if s.failure != nil {
		return nil, s.failure
	}
	return []searchResult{s.result}, nil
}

// makeStubbedTool installs a fixed chain of searchStubProviders for the master tenant
// so Execute() bypasses the real provider chain resolution.
func makeStubbedTool(t *testing.T, providers ...*searchStubProvider) (*WebSearchTool, context.Context) {
	t.Helper()
	tool := &WebSearchTool{
		secrets:    newFakeSecretsStore(),
		cache:      newWebCache(defaultCacheMaxEntries, defaultCacheTTL),
		chainCache: newTenantChainCache(),
	}
	tid := uuid.New()
	ctx := store.WithTenantID(context.Background(), tid)
	chain := make([]SearchProvider, len(providers))
	for i, p := range providers {
		chain[i] = p
	}
	tool.chainCache.Set(tid, chain)
	return tool, ctx
}

// TestExecute_ProviderParam_NarrowsChainToOne — when caller passes provider="exa",
// the brave provider must NOT be called even though it sits earlier in the chain.
func TestExecute_ProviderParam_NarrowsChainToOne(t *testing.T) {
	braveCalled, exaCalled := false, false
	tool, ctx := makeStubbedTool(t,
		&searchStubProvider{name: "brave", called: &braveCalled, result: searchResult{Title: "brave-hit", URL: "https://b.example/x"}},
		&searchStubProvider{name: "exa", called: &exaCalled, result: searchResult{Title: "exa-hit", URL: "https://e.example/x"}},
	)

	res := tool.Execute(ctx, map[string]any{
		"query":    "solana dex volume",
		"provider": "exa",
	})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if braveCalled {
		t.Errorf("brave was called but provider=\"exa\" requested a narrowed chain")
	}
	if !exaCalled {
		t.Errorf("exa not called despite provider=\"exa\"")
	}
	if !strings.Contains(res.ForLLM, "exa-hit") {
		t.Errorf("expected exa result in output, got: %s", res.ForLLM)
	}
}

// TestExecute_ProviderParam_CaseInsensitive — "EXA", "Exa", "exa" all work.
func TestExecute_ProviderParam_CaseInsensitive(t *testing.T) {
	for _, want := range []string{"EXA", "Exa", "exa"} {
		t.Run(want, func(t *testing.T) {
			exaCalled := false
			tool, ctx := makeStubbedTool(t,
				&searchStubProvider{name: "brave", called: nil, result: searchResult{Title: "brave-hit"}},
				&searchStubProvider{name: "exa", called: &exaCalled, result: searchResult{Title: "exa-hit"}},
			)
			res := tool.Execute(ctx, map[string]any{"query": "q", "provider": want})
			if res.IsError {
				t.Fatalf("error: %s", res.ForLLM)
			}
			if !exaCalled {
				t.Errorf("exa should be called for provider=%q", want)
			}
		})
	}
}

// TestExecute_ProviderParam_Unknown — caller asking for a provider not in the
// tenant chain gets a clear error listing what IS available.
func TestExecute_ProviderParam_Unknown(t *testing.T) {
	tool, ctx := makeStubbedTool(t,
		&searchStubProvider{name: "brave", result: searchResult{}},
		&searchStubProvider{name: "exa", result: searchResult{}},
	)
	res := tool.Execute(ctx, map[string]any{"query": "q", "provider": "google"})
	if !res.IsError {
		t.Fatalf("expected error for unknown provider, got success: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "google") || !strings.Contains(res.ForLLM, "brave") || !strings.Contains(res.ForLLM, "exa") {
		t.Errorf("error should name unknown provider AND list available ones, got: %s", res.ForLLM)
	}
}

// TestExecute_NoProviderParam_FallsBackToFirstSuccessWins — omitting provider
// preserves the existing behaviour: chain order is honoured, first success wins.
func TestExecute_NoProviderParam_FallsBackToFirstSuccessWins(t *testing.T) {
	braveCalled, exaCalled := false, false
	tool, ctx := makeStubbedTool(t,
		&searchStubProvider{name: "brave", called: &braveCalled, result: searchResult{Title: "brave-hit"}},
		&searchStubProvider{name: "exa", called: &exaCalled, result: searchResult{Title: "exa-hit"}},
	)
	res := tool.Execute(ctx, map[string]any{"query": "q"})
	if res.IsError {
		t.Fatalf("error: %s", res.ForLLM)
	}
	if !braveCalled {
		t.Errorf("brave should be called first when no provider param")
	}
	if exaCalled {
		t.Errorf("exa should NOT be called when brave succeeded (first-success-wins)")
	}
}

// TestExecute_ProviderParam_CacheIsolated — same query but different `provider`
// args must NOT collide in the cache, otherwise cross-engine corroboration
// would just replay one engine's result twice.
func TestExecute_ProviderParam_CacheIsolated(t *testing.T) {
	braveCalled, exaCalled := false, false
	tool, ctx := makeStubbedTool(t,
		&searchStubProvider{name: "brave", called: &braveCalled, result: searchResult{Title: "brave-hit"}},
		&searchStubProvider{name: "exa", called: &exaCalled, result: searchResult{Title: "exa-hit"}},
	)
	_ = tool.Execute(ctx, map[string]any{"query": "q", "provider": "brave"})
	_ = tool.Execute(ctx, map[string]any{"query": "q", "provider": "exa"})
	if !braveCalled || !exaCalled {
		t.Fatalf("both providers should run for distinct provider args (brave=%v, exa=%v)", braveCalled, exaCalled)
	}
}
