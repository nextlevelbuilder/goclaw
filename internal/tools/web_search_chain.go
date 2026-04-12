package tools

import (
	"context"
	"encoding/json"
	"log/slog"
)

// web_search_chain.go — tenant-aware provider chain resolution.
//
// The web_search tool is registered as a singleton at gateway startup with a
// hardcoded provider list built from the global config (see NewWebSearchTool
// in web_search.go). To honor per-tenant overrides without re-constructing
// the singleton, Execute() calls ResolveWebSearchChain per request to decide
// which of its existing providers to try and in what order.
//
// Tenant settings schema (stored in builtin_tool_tenant_configs.settings):
//
//	{
//	  "provider_order": ["duckduckgo", "brave"],  // optional reorder
//	  "brave":      { "enabled": false },         // optional per-provider disable
//	  "duckduckgo": { "enabled": true }
//	}
//
// Secrets (API keys) stay in config_secrets — the tenant cannot inject keys
// via this settings blob. Tenant admins that need to supply their own key
// use the existing tenant-scoped config_secrets path.

// WebSearchProviderOverride is the per-provider override envelope. Only
// non-nil fields override the default. Unknown fields in the JSON blob are
// ignored to stay forward-compatible with future tuning knobs.
type WebSearchProviderOverride struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// WebSearchChainOverride is the full tenant settings shape for web_search.
// All fields optional — an empty/nil override results in the default chain.
type WebSearchChainOverride struct {
	ProviderOrder []string                              `json:"provider_order,omitempty"`
	Providers     map[string]WebSearchProviderOverride  `json:"-"`
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

// ResolveWebSearchChain returns the ordered slice of providers to try for
// the current request, honoring any tenant override in ctx.
//
// Precedence:
//  1. Tenant override via BuiltinToolSettingsFromCtx["web_search"]
//     — reorders via provider_order and/or disables individual providers
//  2. Defaults — the singleton's built-in provider order
//
// A provider is included only if it:
//   - exists in the defaults (tenant can't inject new providers this way)
//   - is not explicitly disabled (enabled=false) by the tenant
//
// Returns a fresh slice even on the fast-path so the caller can iterate
// freely — the underlying providers are shared singletons (stateless).
func ResolveWebSearchChain(ctx context.Context, defaults []SearchProvider) []SearchProvider {
	if len(defaults) == 0 {
		return nil
	}

	// Fast path: no ctx settings → return defaults unchanged.
	settings := BuiltinToolSettingsFromCtx(ctx)
	raw, hasKey := settings["web_search"]
	if !hasKey || len(raw) == 0 {
		return defaults
	}

	var override WebSearchChainOverride
	if err := json.Unmarshal(raw, &override); err != nil {
		slog.Warn("web_search: failed to parse tenant override, using defaults", "error", err)
		return defaults
	}

	// Build a name → provider index from defaults so we can reorder / filter
	// without losing the actual SearchProvider instance.
	byName := make(map[string]SearchProvider, len(defaults))
	for _, p := range defaults {
		byName[p.Name()] = p
	}

	// isDisabled returns true if the tenant explicitly disabled a provider.
	isDisabled := func(name string) bool {
		po, ok := override.Providers[name]
		return ok && po.Enabled != nil && !*po.Enabled
	}

	// If tenant set provider_order, use it as the authoritative iteration
	// order. Providers not in the order list fall off (admin intent: "only
	// try these, in this order"). Unknown providers in the list are skipped
	// with a warning — no injection path.
	if len(override.ProviderOrder) > 0 {
		chain := make([]SearchProvider, 0, len(override.ProviderOrder))
		for _, name := range override.ProviderOrder {
			p, ok := byName[name]
			if !ok {
				slog.Warn("web_search: tenant provider_order references unknown provider", "name", name)
				continue
			}
			if isDisabled(name) {
				continue
			}
			chain = append(chain, p)
		}
		return chain
	}

	// No provider_order but per-provider enabled flags may still filter.
	// Preserve default order, drop disabled entries.
	if len(override.Providers) > 0 {
		chain := make([]SearchProvider, 0, len(defaults))
		for _, p := range defaults {
			if isDisabled(p.Name()) {
				continue
			}
			chain = append(chain, p)
		}
		return chain
	}

	// Override present but empty — use defaults.
	return defaults
}
