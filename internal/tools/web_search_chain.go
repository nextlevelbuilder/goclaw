package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// web_search_chain.go — per-tenant provider chain resolution.
//
// On every Execute() call, resolveChain() returns the ordered slice of
// SearchProviders for the current tenant.  A 60-second TTL cache per
// tenant UUID amortizes DB reads (≈1 round-trip/min/tenant on cache miss).
//
// Chain construction rules (buildChainFromStorage):
//  1. Parse tenant settings from ctx (builtin_tool_tenant_configs.settings).
//  2. Use NormalizeWebSearchProviderOrder to determine iteration order.
//  3. DDG is always appended last — force-enabled, no API key required.
//  4. For other providers: skip if tenant explicitly disabled, or if no API
//     key found in config_secrets for the current tenant.
//  5. Keyless providers (keylessSearchProviders) need no key and appear only
//     when the tenant names them in provider_order.
//
// Tenant settings schema (stored in builtin_tool_tenant_configs.settings):
//
//	{
//	  "provider_order": ["brave", "exa"],     // optional reorder
//	  "brave":      { "enabled": false },     // optional per-provider disable
//	  "duckduckgo": { "enabled": true }
//	}

// tenantChainTTL is the cache TTL for per-tenant provider chains.
const tenantChainTTL = 60 * time.Second

// tenantChainEntry is one cached chain record.
type tenantChainEntry struct {
	chain   []SearchProvider
	expires time.Time
}

// tenantChainCache is a simple RWMutex-guarded map from tenant UUID to
// provider chain with TTL expiry.
type tenantChainCache struct {
	mu      sync.RWMutex
	entries map[uuid.UUID]tenantChainEntry
	now     func() time.Time // injected for testing; defaults to time.Now
}

func newTenantChainCache() *tenantChainCache {
	return &tenantChainCache{
		entries: make(map[uuid.UUID]tenantChainEntry),
		now:     time.Now, // default to real time
	}
}

// Get returns the cached chain for tid if it exists and has not expired.
func (c *tenantChainCache) Get(tid uuid.UUID) ([]SearchProvider, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[tid]
	if !ok || c.now().After(e.expires) {
		return nil, false
	}
	return e.chain, true
}

// Set stores the chain for tid with the configured TTL.
func (c *tenantChainCache) Set(tid uuid.UUID, chain []SearchProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[tid] = tenantChainEntry{chain: chain, expires: c.now().Add(tenantChainTTL)}
}

// Invalidate removes the cache entry for a single tenant.
func (c *tenantChainCache) Invalidate(tid uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, tid)
}

// InvalidateAll drops all cached entries (used on master-admin write).
func (c *tenantChainCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[uuid.UUID]tenantChainEntry)
}

// WebSearchProviderOverride is the per-provider override envelope. Only
// non-nil fields override the default. Unknown fields in the JSON blob are
// ignored to stay forward-compatible with future tuning knobs.
type WebSearchProviderOverride struct {
	Enabled    *bool `json:"enabled,omitempty"`
	MaxResults int   `json:"max_results,omitempty"`
}

// WebSearchChainOverride is the full tenant settings shape for web_search.
// All fields optional — an empty/nil override results in the default chain.
type WebSearchChainOverride struct {
	ProviderOrder []string                             `json:"provider_order,omitempty"`
	Providers     map[string]WebSearchProviderOverride `json:"-"`
	// Per-provider sections are unmarshaled into Providers via custom logic
	// below so admins can keep the natural JSON shape:
	//   { "brave": {...}, "duckduckgo": {...} }
}

// UnmarshalJSON accepts the flat admin-facing shape:
//
//	{ "provider_order": [...], "brave": {...}, "duckduckgo": {...} }
//
// Keeps ProviderOrder top-level and collects every other object field into
// the Providers map keyed by provider name.
func (w *WebSearchChainOverride) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if orderRaw, ok := raw["provider_order"]; ok {
		if err := json.Unmarshal(orderRaw, &w.ProviderOrder); err != nil {
			return err
		}
		delete(raw, "provider_order")
	}
	if len(raw) > 0 {
		w.Providers = make(map[string]WebSearchProviderOverride, len(raw))
		for name, blob := range raw {
			var po WebSearchProviderOverride
			if err := json.Unmarshal(blob, &po); err != nil {
				// Skip unknown keys that aren't provider overrides (forward-compat).
				slog.Debug("web_search: skipping unrecognized override key", "key", name, "error", err)
				continue
			}
			w.Providers[name] = po
		}
	}
	return nil
}

// resolveChain returns the ordered provider slice for the tenant in ctx.
// It checks the TTL cache first; on miss it calls buildChainFromStorage.
func (t *WebSearchTool) resolveChain(ctx context.Context) []SearchProvider {
	tid := store.TenantIDFromContext(ctx) // uuid.Nil OK → master fallback via secretsStore

	if chain, ok := t.chainCache.Get(tid); ok {
		return chain
	}

	chain := BuildChainFromStorage(ctx, t.secrets)
	t.chainCache.Set(tid, chain)
	return chain
}

// BuildChainFromStorage constructs the provider chain for the current
// request by combining tenant settings from ctx with API keys from secrets.
// Exported for testing.
func BuildChainFromStorage(ctx context.Context, secrets store.ConfigSecretsStore) []SearchProvider {
	// Parse tenant override (may be nil/empty → all defaults).
	var override WebSearchChainOverride
	settings := BuiltinToolSettingsFromCtx(ctx)
	if raw, ok := settings["web_search"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &override); err != nil {
			slog.Warn("web_search: failed to parse tenant override, using defaults", "error", err)
		}
	}

	// isDisabled returns true if the tenant explicitly disabled a provider.
	isDisabled := func(name string) bool {
		if po, ok := override.Providers[name]; ok {
			return po.Enabled != nil && !*po.Enabled
		}
		return false
	}

	// maxResultsFor applies the tenant's per-provider max_results override.
	maxResultsFor := func(name string) int {
		if po, ok := override.Providers[name]; ok && po.MaxResults > 0 {
			return po.MaxResults
		}
		return defaultSearchCount
	}

	order := NormalizeWebSearchProviderOrder(override.ProviderOrder)

	var chain []SearchProvider
	for _, name := range order {
		if name == searchProviderDuckDuckGo {
			// DDG is force-enabled — always last, no API key needed.
			chain = append(chain, buildProviderByName(name, "", maxResultsFor(name)))
			continue
		}

		if isDisabled(name) {
			continue
		}
		if spec, ok := keylessSearchProviders[name]; ok {
			// Keyless providers are absent from the default order, so reaching
			// this branch means the tenant named this one in provider_order.
			key := ""
			if spec.optionalKey {
				var usable bool
				if key, usable = optionalSearchKey(ctx, secrets, name); !usable {
					continue
				}
			}
			chain = append(chain, buildProviderByName(name, key, maxResultsFor(name)))
			continue
		}

		key, err := secrets.Get(ctx, "tools.web."+name+".api_key")
		if err != nil || key == "" {
			// No key → provider not configured for this tenant; skip silently.
			continue
		}

		p := buildProviderByName(name, key, maxResultsFor(name))
		if p == nil {
			slog.Warn("web_search: unknown provider name in chain", "name", name)
			continue
		}
		chain = append(chain, p)
	}

	return chain
}

// optionalSearchKey reads the optional API key of a keyless provider. A
// missing row means no key, and the provider stays on its keyless endpoint.
// Any other error (database unavailable, decrypt failure after an encryption
// key rotation) skips the provider with a warning: the tenant may have stored
// a key, and quietly moving it onto the shared keyless quota would hide that.
func optionalSearchKey(ctx context.Context, secrets store.ConfigSecretsStore, name string) (key string, usable bool) {
	key, err := secrets.Get(ctx, "tools.web."+name+".api_key")
	switch {
	case err == nil:
		return key, true
	case errors.Is(err, sql.ErrNoRows):
		return "", true
	default:
		slog.Warn("web_search: skipping provider, its stored API key could not be read", "provider", name, "error", err)
		return "", false
	}
}
