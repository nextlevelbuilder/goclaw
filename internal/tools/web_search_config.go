package tools

import (
	"slices"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// WebSearchConfig holds configuration for the web search tool.
type WebSearchConfig struct {
	ProviderOrder    []string
	ExaAPIKey        string
	ExaEnabled       bool
	ExaMaxResults    int
	TavilyAPIKey     string
	TavilyEnabled    bool
	TavilyMaxResults int
	BraveAPIKey      string
	BraveEnabled     bool
	BraveMaxResults  int
	DDGEnabled       bool
	DDGMaxResults    int
	CacheTTL         time.Duration
}

// WebSearchConfigFromConfig creates a WebSearchConfig from the global config.
func WebSearchConfigFromConfig(cfg *config.Config) WebSearchConfig {
	return WebSearchConfig{
		ProviderOrder:    cfg.Tools.Web.ProviderOrder,
		ExaEnabled:       cfg.Tools.Web.Exa.Enabled,
		ExaAPIKey:        cfg.Tools.Web.Exa.APIKey,
		ExaMaxResults:    cfg.Tools.Web.Exa.MaxResults,
		TavilyEnabled:    cfg.Tools.Web.Tavily.Enabled,
		TavilyAPIKey:     cfg.Tools.Web.Tavily.APIKey,
		TavilyMaxResults: cfg.Tools.Web.Tavily.MaxResults,
		BraveEnabled:     cfg.Tools.Web.Brave.Enabled,
		BraveAPIKey:      cfg.Tools.Web.Brave.APIKey,
		BraveMaxResults:  cfg.Tools.Web.Brave.MaxResults,
		DDGEnabled:       true,
		DDGMaxResults:    cfg.Tools.Web.DuckDuckGo.MaxResults,
	}
}

func buildSearchProviders(cfg WebSearchConfig) []SearchProvider {
	var providers []SearchProvider
	for _, providerID := range NormalizeWebSearchProviderOrder(cfg.ProviderOrder) {
		switch providerID {
		case searchProviderExa:
			if cfg.ExaEnabled && cfg.ExaAPIKey != "" {
				providers = append(providers, newExaSearchProvider(cfg.ExaAPIKey, cfg.ExaMaxResults))
			}
		case searchProviderTavily:
			if cfg.TavilyEnabled && cfg.TavilyAPIKey != "" {
				providers = append(providers, newTavilySearchProvider(cfg.TavilyAPIKey, cfg.TavilyMaxResults))
			}
		case searchProviderBrave:
			if cfg.BraveEnabled && cfg.BraveAPIKey != "" {
				providers = append(providers, newBraveSearchProvider(cfg.BraveAPIKey, cfg.BraveMaxResults))
			}
		case searchProviderDuckDuckGo:
			if cfg.DDGEnabled {
				providers = append(providers, newDuckDuckGoSearchProvider(cfg.DDGMaxResults))
			}
		}
	}
	return providers
}

// NormalizeWebSearchProviderOrder normalizes user-specified provider order.
// Explicit providers appear first in their specified order, remaining known
// providers are appended (DuckDuckGo always last as free fallback).
func NormalizeWebSearchProviderOrder(order []string) []string {
	result := make([]string, 0, len(defaultSearchProviderOrder))
	seen := make(map[string]bool, len(defaultSearchProviderOrder))

	for _, raw := range order {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == searchProviderDuckDuckGo || id == "" {
			continue // DDG always last
		}
		if !isKnownSearchProvider(id) || seen[id] {
			continue
		}
		result = append(result, id)
		seen[id] = true
	}
	// Append remaining known providers not yet listed (except DDG).
	for _, id := range defaultSearchProviderOrder {
		if id == searchProviderDuckDuckGo {
			continue
		}
		if !seen[id] {
			result = append(result, id)
		}
	}
	return append(result, searchProviderDuckDuckGo)
}

func isKnownSearchProvider(id string) bool {
	return slices.Contains(defaultSearchProviderOrder, id)
}

// --- Shared provider helpers ---

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func coalesceSearchText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func clampProviderResultCount(requested, providerMax int) int {
	if requested <= 0 {
		requested = defaultSearchCount
	}
	if providerMax > 0 && requested > providerMax {
		return providerMax
	}
	return requested
}

func normalizeProviderMaxResults(value int) int {
	if value <= 0 {
		return defaultSearchCount
	}
	if value > maxSearchCount {
		return maxSearchCount
	}
	return value
}
