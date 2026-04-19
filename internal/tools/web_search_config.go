package tools

import (
	"slices"
	"strings"
)

// buildProviderByName returns the SearchProvider for a known name.
// Returns nil for unknown names. DDG ignores apiKey (not required).
// maxResults <= 0 falls back to defaultSearchCount.
func buildProviderByName(name, apiKey string, maxResults int) SearchProvider {
	if maxResults <= 0 {
		maxResults = defaultSearchCount
	}
	switch name {
	case searchProviderExa:
		return newExaSearchProvider(apiKey, maxResults)
	case searchProviderTavily:
		return newTavilySearchProvider(apiKey, maxResults)
	case searchProviderBrave:
		return newBraveSearchProvider(apiKey, maxResults)
	case searchProviderDuckDuckGo:
		return newDuckDuckGoSearchProvider(maxResults)
	default:
		return nil
	}
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
