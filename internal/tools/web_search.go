package tools

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Matching TS src/agents/tools/web-search.ts constants.
const (
	defaultSearchCount   = 5
	maxSearchCount       = 10
	searchTimeoutSeconds = 30
	braveSearchEndpoint  = "https://api.search.brave.com/res/v1/web/search"
	exaSearchEndpoint    = "https://api.exa.ai/search"
	tavilySearchEndpoint = "https://api.tavily.com/search"
	webSearchUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_2) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

const (
	searchProviderExa        = "exa"
	searchProviderTavily     = "tavily"
	searchProviderBrave      = "brave"
	searchProviderDuckDuckGo = "duckduckgo"
)

var defaultSearchProviderOrder = []string{
	searchProviderExa,
	searchProviderTavily,
	searchProviderBrave,
	searchProviderDuckDuckGo,
}

// SearchProvider abstracts a web search backend.
type SearchProvider interface {
	Search(ctx context.Context, params searchParams) ([]searchResult, error)
	Name() string
}

type searchParams struct {
	Query      string
	Count      int
	Country    string
	SearchLang string
	UILang     string
	Freshness  string
}

type searchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// --- Freshness validation (matching TS) ---

var (
	freshnessShortcuts = map[string]bool{"pd": true, "pw": true, "pm": true, "py": true}
	freshnessRangeRe   = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})to(\d{4}-\d{2}-\d{2})$`)
)

func normalizeFreshness(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return ""
	}
	if freshnessShortcuts[v] {
		return v
	}
	if m := freshnessRangeRe.FindStringSubmatch(v); len(m) == 3 {
		start, errS := time.Parse("2006-01-02", m[1])
		end, errE := time.Parse("2006-01-02", m[2])
		if errS == nil && errE == nil && !start.After(end) {
			return v
		}
	}
	return ""
}

// --- WebSearchTool ---

// WebSearchTool implements the web_search tool with per-tenant provider chain
// resolution. Providers are resolved per-request from config_secrets and
// builtin_tool_tenant_configs.settings, cached per tenant for 60 seconds.
type WebSearchTool struct {
	secrets    store.ConfigSecretsStore
	cache      *webCache
	chainCache *tenantChainCache
}

// NewWebSearchTool constructs a WebSearchTool. msgBus may be nil (e.g. desktop
// edition) — cache invalidation then relies on TTL alone.
func NewWebSearchTool(secrets store.ConfigSecretsStore, msgBus *bus.MessageBus) *WebSearchTool {
	t := &WebSearchTool{
		secrets:    secrets,
		cache:      newWebCache(defaultCacheMaxEntries, defaultCacheTTL),
		chainCache: newTenantChainCache(),
	}

	if msgBus != nil {
		msgBus.Subscribe("web_search:cache_invalidate", func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindBuiltinTools || payload.Key != "web_search" {
				return
			}
			if payload.TenantID == uuid.Nil {
				// Master admin write — wipe all tenants.
				t.chainCache.InvalidateAll()
			} else {
				t.chainCache.Invalidate(payload.TenantID)
			}
		})
	}

	return t
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for current information. Returns titles, URLs, and snippets from search results."
}

func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query string.",
			},
			"count": map[string]any{
				"type":        "number",
				"description": "Number of results to return (1-10).",
				"minimum":     1.0,
				"maximum":     float64(maxSearchCount),
			},
			"country": map[string]any{
				"type":        "string",
				"description": "2-letter country code for region-specific results (e.g., 'DE', 'US', 'ALL'). Default: 'US'.",
			},
			"search_lang": map[string]any{
				"type":        "string",
				"description": "ISO language code for search results (e.g., 'de', 'en', 'fr').",
			},
			"ui_lang": map[string]any{
				"type":        "string",
				"description": "ISO language code for UI elements.",
			},
			"freshness": map[string]any{
				"type":        "string",
				"description": "Filter results by discovery time. Supports 'pd' (past day), 'pw' (past week), 'pm' (past month), 'py' (past year), and date range 'YYYY-MM-DDtoYYYY-MM-DD'.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query is required")
	}

	count := defaultSearchCount
	if c, ok := args["count"].(float64); ok && int(c) >= 1 && int(c) <= maxSearchCount {
		count = int(c)
	}

	country, _ := args["country"].(string)
	searchLang, _ := args["search_lang"].(string)
	uiLang, _ := args["ui_lang"].(string)
	freshness, _ := args["freshness"].(string)

	params := searchParams{
		Query:      query,
		Count:      count,
		Country:    country,
		SearchLang: searchLang,
		UILang:     uiLang,
		Freshness:  freshness,
	}

	// Check cache (scoped per channel to prevent cross-channel cache poisoning)
	channel := ToolChannelFromCtx(ctx)
	cacheKey := fmt.Sprintf("%s:%s", channel, buildSearchCacheKey(params))
	if cached, ok := t.cache.get(cacheKey); ok {
		slog.Debug("web_search cache hit", "query", query)
		return NewResult(cached)
	}

	// Resolve per-request provider chain from tenant config_secrets + settings overlay.
	chain := t.resolveChain(ctx)

	// Try providers in order (first success wins)
	var lastErr error
	for _, provider := range chain {
		results, err := provider.Search(ctx, params)
		if err != nil {
			slog.Warn("web_search provider failed", "provider", provider.Name(), "error", err)
			lastErr = err
			continue
		}

		formatted := formatSearchResults(query, results, provider.Name())
		wrapped := wrapExternalContent(formatted, "Web Search", false)

		t.cache.set(cacheKey, wrapped)
		return NewResult(wrapped)
	}

	if lastErr != nil {
		return ErrorResult(fmt.Sprintf("all search providers failed: %v", lastErr))
	}
	return ErrorResult("no search providers configured")
}

func buildSearchCacheKey(p searchParams) string {
	parts := []string{
		p.Query,
		fmt.Sprintf("%d", p.Count),
		orDefault(p.Country, "default"),
		orDefault(p.SearchLang, "default"),
		orDefault(p.UILang, "default"),
		orDefault(p.Freshness, "default"),
	}
	return strings.Join(parts, ":")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func formatSearchResults(query string, results []searchResult, provider string) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found for: %s", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s (via %s)\n\n", query, provider))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Description != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Description))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
