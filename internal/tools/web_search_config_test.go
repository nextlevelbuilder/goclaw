package tools

import (
	"reflect"
	"testing"
)

func TestNormalizeWebSearchProviderOrder_Empty(t *testing.T) {
	got := NormalizeWebSearchProviderOrder(nil)
	want := []string{"exa", "tavily", "brave", "duckduckgo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeWebSearchProviderOrder(nil) = %v, want %v", got, want)
	}
}

func TestNormalizeWebSearchProviderOrder_UserSpecified(t *testing.T) {
	got := NormalizeWebSearchProviderOrder([]string{"brave", "exa"})
	// brave first, exa second (user order), tavily appended, ddg last
	want := []string{"brave", "exa", "tavily", "duckduckgo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeWebSearchProviderOrder_DDGIgnored(t *testing.T) {
	// DDG in user order is ignored (always last)
	got := NormalizeWebSearchProviderOrder([]string{"duckduckgo", "tavily"})
	want := []string{"tavily", "exa", "brave", "duckduckgo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeWebSearchProviderOrder_Dedup(t *testing.T) {
	got := NormalizeWebSearchProviderOrder([]string{"exa", "exa", "brave"})
	want := []string{"exa", "brave", "tavily", "duckduckgo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeWebSearchProviderOrder_UnknownSkipped(t *testing.T) {
	got := NormalizeWebSearchProviderOrder([]string{"bing", "tavily"})
	want := []string{"tavily", "exa", "brave", "duckduckgo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildSearchProviders_AllEnabled(t *testing.T) {
	cfg := WebSearchConfig{
		ExaEnabled:    true,
		ExaAPIKey:     "exa-key",
		TavilyEnabled: true,
		TavilyAPIKey:  "tavily-key",
		BraveEnabled:  true,
		BraveAPIKey:   "brave-key",
		DDGEnabled:    true,
	}
	providers := buildSearchProviders(cfg)
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	want := []string{"exa", "tavily", "brave", "duckduckgo"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestBuildSearchProviders_OnlyBraveAndDDG(t *testing.T) {
	cfg := WebSearchConfig{
		BraveEnabled: true,
		BraveAPIKey:  "brave-key",
		DDGEnabled:   true,
	}
	providers := buildSearchProviders(cfg)
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	// Exa + Tavily not enabled, so only brave + ddg
	want := []string{"brave", "duckduckgo"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestBuildSearchProviders_CustomOrder(t *testing.T) {
	cfg := WebSearchConfig{
		ProviderOrder: []string{"tavily", "brave"},
		TavilyEnabled: true,
		TavilyAPIKey:  "key",
		BraveEnabled:  true,
		BraveAPIKey:   "key",
		DDGEnabled:    true,
	}
	providers := buildSearchProviders(cfg)
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	// tavily first (user), brave second, exa appended (no key so skipped), ddg last
	want := []string{"tavily", "brave", "duckduckgo"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestBuildSearchProviders_NoProviders(t *testing.T) {
	cfg := WebSearchConfig{}
	providers := buildSearchProviders(cfg)
	if len(providers) != 0 {
		t.Errorf("expected empty providers, got %d", len(providers))
	}
}

func TestClampProviderResultCount(t *testing.T) {
	tests := []struct {
		requested, max, want int
	}{
		{5, 10, 5},
		{15, 10, 10},
		{0, 10, defaultSearchCount},
		{5, 0, 5},
	}
	for _, tt := range tests {
		got := clampProviderResultCount(tt.requested, tt.max)
		if got != tt.want {
			t.Errorf("clampProviderResultCount(%d, %d) = %d, want %d", tt.requested, tt.max, got, tt.want)
		}
	}
}

func TestNormalizeProviderMaxResults(t *testing.T) {
	tests := []struct {
		input, want int
	}{
		{0, defaultSearchCount},
		{-1, defaultSearchCount},
		{7, 7},
		{15, maxSearchCount},
	}
	for _, tt := range tests {
		got := normalizeProviderMaxResults(tt.input)
		if got != tt.want {
			t.Errorf("normalizeProviderMaxResults(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
