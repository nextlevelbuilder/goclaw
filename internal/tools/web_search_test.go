package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubSearchProvider struct {
	name       string
	err        error
	results    []searchResult
	calls      int
	lastParams searchParams
}

func (s *stubSearchProvider) Name() string { return s.name }

func (s *stubSearchProvider) Search(_ context.Context, params searchParams) ([]searchResult, error) {
	s.calls++
	s.lastParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

func TestNewWebSearchTool_PreservesDefaultDDGOnlyFlow(t *testing.T) {
	tool := NewWebSearchTool(WebSearchConfig{
		DDGEnabled:    true,
		DDGMaxResults: 5,
	})
	if tool == nil {
		t.Fatal("expected tool")
	}
	if len(tool.providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(tool.providers))
	}
	if got := tool.providers[0].Name(); got != "duckduckgo" {
		t.Fatalf("provider = %q, want duckduckgo", got)
	}
}

func TestNormalizeWebSearchProviderOrder_DedupesAndForcesDDGLast(t *testing.T) {
	got := NormalizeWebSearchProviderOrder([]string{
		" brave ",
		"duckduckgo",
		"brave",
		"unknown",
		"exa",
	})
	want := []string{"brave", "exa", "tavily", "duckduckgo"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("provider[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestNewWebSearchTool_HonorsConfiguredProviderOrderAndAppendsEnabledDDG(t *testing.T) {
	tool := NewWebSearchTool(WebSearchConfig{
		ProviderOrder:   []string{"exa", "brave"},
		ExaEnabled:      true,
		ExaAPIKey:       "exa-secret",
		ExaMaxResults:   3,
		BraveEnabled:    true,
		BraveAPIKey:     "brave-secret",
		BraveMaxResults: 4,
		DDGEnabled:      true,
		DDGMaxResults:   5,
	})
	if tool == nil {
		t.Fatal("expected tool")
	}

	got := []string{
		tool.providers[0].Name(),
		tool.providers[1].Name(),
		tool.providers[2].Name(),
	}
	want := []string{"exa", "brave", "duckduckgo"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("provider[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewWebSearchTool_SkipsUnavailableProvidersButKeepsDDGFallback(t *testing.T) {
	tool := NewWebSearchTool(WebSearchConfig{
		ProviderOrder: []string{"exa", "tavily"},
		ExaEnabled:    true,
		TavilyEnabled: true,
		DDGEnabled:    true,
	})
	if tool == nil {
		t.Fatal("expected tool")
	}
	if len(tool.providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(tool.providers))
	}
	if got := tool.providers[0].Name(); got != "duckduckgo" {
		t.Fatalf("provider = %q, want duckduckgo", got)
	}
}

func TestNewWebSearchTool_WiresProviderSpecificMaxResults(t *testing.T) {
	tool := NewWebSearchTool(WebSearchConfig{
		ProviderOrder:    []string{"exa", "tavily", "brave", "duckduckgo"},
		ExaEnabled:       true,
		ExaAPIKey:        "exa-secret",
		ExaMaxResults:    2,
		TavilyEnabled:    true,
		TavilyAPIKey:     "tavily-secret",
		TavilyMaxResults: 3,
		BraveEnabled:     true,
		BraveAPIKey:      "brave-secret",
		BraveMaxResults:  4,
		DDGEnabled:       true,
		DDGMaxResults:    5,
	})
	if tool == nil {
		t.Fatal("expected tool")
	}

	if got := tool.providers[0].(*exaSearchProvider).maxResults; got != 2 {
		t.Fatalf("exa maxResults = %d, want 2", got)
	}
	if got := tool.providers[1].(*tavilySearchProvider).maxResults; got != 3 {
		t.Fatalf("tavily maxResults = %d, want 3", got)
	}
	if got := tool.providers[2].(*braveSearchProvider).maxResults; got != 4 {
		t.Fatalf("brave maxResults = %d, want 4", got)
	}
	if got := tool.providers[3].(*duckDuckGoSearchProvider).maxResults; got != 5 {
		t.Fatalf("ddg maxResults = %d, want 5", got)
	}
}

func TestNewWebSearchTool_DefaultsBlankProviderMaxResultsToDefaultCount(t *testing.T) {
	tool := NewWebSearchTool(WebSearchConfig{
		ProviderOrder: []string{"exa", "tavily", "brave", "duckduckgo"},
		ExaEnabled:    true,
		ExaAPIKey:     "exa-secret",
		TavilyEnabled: true,
		TavilyAPIKey:  "tavily-secret",
		BraveEnabled:  true,
		BraveAPIKey:   "brave-secret",
		DDGEnabled:    true,
	})
	if tool == nil {
		t.Fatal("expected tool")
	}

	if got := tool.providers[0].(*exaSearchProvider).maxResults; got != defaultSearchCount {
		t.Fatalf("exa maxResults = %d, want %d", got, defaultSearchCount)
	}
	if got := tool.providers[1].(*tavilySearchProvider).maxResults; got != defaultSearchCount {
		t.Fatalf("tavily maxResults = %d, want %d", got, defaultSearchCount)
	}
	if got := tool.providers[2].(*braveSearchProvider).maxResults; got != defaultSearchCount {
		t.Fatalf("brave maxResults = %d, want %d", got, defaultSearchCount)
	}
	if got := tool.providers[3].(*duckDuckGoSearchProvider).maxResults; got != defaultSearchCount {
		t.Fatalf("ddg maxResults = %d, want %d", got, defaultSearchCount)
	}
}

func TestWebSearchTool_UpdateConfigRebuildsProviders(t *testing.T) {
	tool := NewWebSearchTool(WebSearchConfig{
		DDGEnabled: true,
	})
	if tool == nil {
		t.Fatal("expected tool")
	}

	tool.UpdateConfig(WebSearchConfig{
		ProviderOrder: []string{"exa"},
		ExaEnabled:    true,
		ExaAPIKey:     "exa-secret",
	})

	if len(tool.providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(tool.providers))
	}
	if got := tool.providers[0].Name(); got != "exa" {
		t.Fatalf("provider = %q, want exa", got)
	}
}

func TestWebSearchTool_Execute_FailsOverToNextProvider(t *testing.T) {
	first := &stubSearchProvider{name: "exa", err: errors.New("boom")}
	second := &stubSearchProvider{name: "duckduckgo", results: []searchResult{{
		Title:       "Example",
		URL:         "https://example.com",
		Description: "Snippet",
	}}}
	tool := &WebSearchTool{
		providers: []SearchProvider{first, second},
		cache:     newWebCache(16, time.Minute),
	}

	result := tool.Execute(context.Background(), map[string]any{
		"query": "test query",
		"count": float64(7),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("calls = (%d,%d), want (1,1)", first.calls, second.calls)
	}
	if second.lastParams.Count != 7 {
		t.Fatalf("count = %d, want 7", second.lastParams.Count)
	}
	if !strings.Contains(result.ForLLM, "via duckduckgo") {
		t.Fatalf("expected provider marker in result, got: %s", result.ForLLM)
	}
}
