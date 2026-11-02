package tools

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// keylessSearchProvider describes a provider that answers without an API key.
type keylessSearchProvider struct {
	// optionalKey providers read tools.web.<name>.api_key when one is stored
	// and switch to their keyed endpoint; the others never read a secret.
	optionalKey bool
}

// keylessSearchProviders stay out of defaultSearchProviderOrder: a keyless
// provider joins a tenant's chain only when the tenant names it in
// provider_order, so a fresh install never sends queries to it unasked.
var keylessSearchProviders = map[string]keylessSearchProvider{
	searchProviderParallel: {},
	searchProviderKeenable: {optionalKey: true},
}

func isKeylessSearchProvider(id string) bool {
	_, ok := keylessSearchProviders[id]
	return ok
}

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
	case searchProviderParallel:
		return newParallelSearchProvider(maxResults)
	case searchProviderSerply:
		return newSerplySearchProvider(apiKey, maxResults)
	case searchProviderKeenable:
		return newKeenableSearchProvider(apiKey, maxResults)
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
	return isKeylessSearchProvider(id) || slices.Contains(defaultSearchProviderOrder, id)
}

// --- Shared provider helpers ---

// truncateStr cuts s to at most max bytes plus "...", backing off to a rune
// boundary so a multi-byte character is never split.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
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
