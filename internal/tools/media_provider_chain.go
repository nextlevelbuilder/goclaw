package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// MediaProviderEntry represents a single provider in an ordered fallback chain.
type MediaProviderEntry struct {
	ProviderID string         `json:"provider_id,omitempty"` // UUID for tracing (optional)
	Provider   string         `json:"provider"`              // name for registry.Get()
	Model      string         `json:"model"`
	Enabled    bool           `json:"enabled"`
	Timeout    int            `json:"timeout"`               // seconds, default 120
	MaxRetries int            `json:"max_retries"`           // default 2
	Params     map[string]any `json:"params,omitempty"`      // provider-specific config
}

// mediaProviderChain is the settings JSON structure for media tools.
// Only supports chain format: {"providers":[...]}.
// Legacy flat format is auto-migrated at startup (see cmd/gateway_builtin_tools.go).
type mediaProviderChain struct {
	Providers []MediaProviderEntry `json:"providers,omitempty"`
}

// applyDefaults fills in zero-value fields with sensible defaults.
func (e *MediaProviderEntry) applyDefaults() {
	if e.Timeout <= 0 {
		e.Timeout = 120
	}
	if e.MaxRetries <= 0 {
		e.MaxRetries = 2
	}
}

// ResolveMediaProviderChain parses builtin_tools.settings for a media tool and
// returns an ordered list of enabled provider entries. Falls back to hardcoded
// defaults when no user-configured chain exists.
//
// Resolution priority:
//  1. Per-agent config override (provider/model from context) — wrapped as single entry
//  2. builtin_tools.settings (new chain format or legacy flat format)
//  3. Hardcoded default chain for the tool
func ResolveMediaProviderChain(
	ctx context.Context,
	toolName string,
	perAgentProvider, perAgentModel string,
	defaultPriority []string,
	defaultModels map[string]string,
	registry *providers.Registry,
) []MediaProviderEntry {
	// 1. Per-agent override takes highest priority
	if perAgentProvider != "" {
		model := perAgentModel
		if model == "" {
			model = defaultModels[perAgentProvider]
		}
		entry := MediaProviderEntry{
			Provider: perAgentProvider,
			Model:    model,
			Enabled:  true,
		}
		entry.applyDefaults()
		return []MediaProviderEntry{entry}
	}

	// 2. Parse from builtin_tools.settings
	if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings[toolName]; ok && len(raw) > 0 {
			chain := parseChainSettings(raw, defaultModels)
			if len(chain) > 0 {
				return chain
			}
		}
	}

	// 3. Hardcoded default chain — use first available provider
	return buildDefaultChain(defaultPriority, defaultModels, registry)
}

// parseChainSettings parses the settings JSON into a chain, handling both new
// and legacy formats. Returns nil if parsing fails or result is empty.
func parseChainSettings(raw []byte, defaultModels map[string]string) []MediaProviderEntry {
	var chain mediaProviderChain
	if err := json.Unmarshal(raw, &chain); err != nil {
		slog.Warn("media_chain: failed to parse settings", "error", err)
		return nil
	}

	var result []MediaProviderEntry
	for _, e := range chain.Providers {
		if !e.Enabled {
			continue
		}
		if e.Provider == "" {
			continue
		}
		if e.Model == "" {
			e.Model = defaultModels[e.Provider]
		}
		e.applyDefaults()
		result = append(result, e)
	}
	return result
}

// buildDefaultChain creates a chain from the hardcoded priority list,
// including only providers that are currently registered.
func buildDefaultChain(
	priority []string,
	defaultModels map[string]string,
	registry *providers.Registry,
) []MediaProviderEntry {
	var chain []MediaProviderEntry
	for _, name := range priority {
		if _, err := registry.Get(name); err == nil {
			entry := MediaProviderEntry{
				Provider: name,
				Model:    defaultModels[name],
				Enabled:  true,
			}
			entry.applyDefaults()
			chain = append(chain, entry)
		}
	}
	return chain
}

// ChainCallFn is the function signature for provider-specific API calls.
// Receives the credential provider, provider name, model, and params.
type ChainCallFn func(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error)

// ChainResult holds the result of ExecuteWithChain.
type ChainResult struct {
	Data     []byte
	Usage    *providers.Usage
	Provider string
	Model    string
}

// ExecuteWithChain tries each provider in the chain sequentially.
// For each provider, it retries up to MaxRetries times (with the configured timeout).
// Returns the first successful result or the last error encountered.
func ExecuteWithChain(
	ctx context.Context,
	chain []MediaProviderEntry,
	registry *providers.Registry,
	fn ChainCallFn,
) (*ChainResult, error) {
	if len(chain) == 0 {
		return nil, fmt.Errorf("no providers configured")
	}

	var lastErr error
	for _, entry := range chain {
		p, err := registry.Get(entry.Provider)
		if err != nil {
			slog.Warn("media_chain: provider not found, skipping",
				"provider", entry.Provider, "error", err)
			lastErr = fmt.Errorf("provider %q not available", entry.Provider)
			continue
		}

		// credentialProvider is optional — providers that don't expose static
		// credentials (e.g. OAuth-based CodexProvider) pass nil and each
		// callProvider falls back to using the provider's Chat() API.
		cp, _ := p.(credentialProvider)

		// Inject resolved provider type into params so callProvider can route correctly.
		// Clone params to avoid mutating the original entry config.
		resolvedType := ResolveProviderType(p)
		callParams := make(map[string]any, len(entry.Params)+1)
		for k, v := range entry.Params {
			callParams[k] = v
		}
		callParams["_provider_type"] = resolvedType

		// Retry loop for this provider
		for attempt := 1; attempt <= entry.MaxRetries; attempt++ {
			timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(entry.Timeout)*time.Second)

			data, usage, callErr := fn(timeoutCtx, cp, entry.Provider, entry.Model, callParams)
			cancel()

			if callErr == nil {
				return &ChainResult{
					Data:     data,
					Usage:    usage,
					Provider: entry.Provider,
					Model:    entry.Model,
				}, nil
			}

			lastErr = callErr

			// Don't retry on context cancellation (parent ctx cancelled)
			if ctx.Err() != nil {
				return nil, fmt.Errorf("context cancelled: %w", lastErr)
			}

			if attempt < entry.MaxRetries {
				slog.Warn("media_chain: attempt failed, retrying",
					"provider", entry.Provider, "model", entry.Model,
					"attempt", attempt, "max_retries", entry.MaxRetries,
					"error", truncateError(callErr))
			}
		}

		slog.Warn("media_chain: provider exhausted retries, moving to next",
			"provider", entry.Provider, "model", entry.Model,
			"max_retries", entry.MaxRetries, "error", truncateError(lastErr))
	}

	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

// maxMediaDownloadBytes is the maximum size for media file downloads (200 MB).
const maxMediaDownloadBytes = 200 * 1024 * 1024

// limitedReadAll reads up to maxMediaDownloadBytes from r, returning an error if the limit is exceeded.
func limitedReadAll(r io.Reader, maxBytes int64) ([]byte, error) {
	lr := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response exceeds %d bytes limit", maxBytes)
	}
	return data, nil
}

// truncateError returns a short string representation of an error for logging.
func truncateError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// GetParamString extracts a string param from the params map, returning fallback if not found.
func GetParamString(params map[string]any, key, fallback string) string {
	if params == nil {
		return fallback
	}
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// GetParamBool extracts a bool param from the params map, returning fallback if not found.
func GetParamBool(params map[string]any, key string, fallback bool) bool {
	if params == nil {
		return fallback
	}
	if v, ok := params[key].(bool); ok {
		return v
	}
	return fallback
}

// GetParamInt extracts an int param from the params map, returning fallback if not found.
func GetParamInt(params map[string]any, key string, fallback int) int {
	if params == nil {
		return fallback
	}
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// typedProvider is optionally implemented by providers that carry their DB provider_type.
type typedProvider interface {
	ProviderType() string
}

// dbTypeToMediaType maps DB provider_type values to the media routing type
// used by callProvider switch statements.
var dbTypeToMediaType = map[string]string{
	"gemini_native":    "gemini",
	"openai_compat":    "openai_compat",
	"openrouter":       "openrouter",
	"minimax_native":   "minimax",
	"dashscope":        "dashscope",
	"bailian":          "dashscope",
	"anthropic_native": "anthropic",
	"suno":             "suno",
}

// ResolveProviderType returns the media routing type for a provider.
// It first checks the provider's DB type (via typedProvider interface),
// then falls back to name-based heuristics for config-registered providers.
func ResolveProviderType(p providers.Provider) string {
	// Prefer the actual DB provider_type when available
	if tp, ok := p.(typedProvider); ok {
		if pt := tp.ProviderType(); pt != "" {
			if mt, found := dbTypeToMediaType[pt]; found {
				return mt
			}
		}
	}
	// Fallback: infer from provider name (for config-registered providers)
	return providerTypeFromName(p.Name())
}

// providerTypeFromName infers provider type from naming patterns.
// Used as fallback when the provider doesn't carry its DB type.
func providerTypeFromName(name string) string {
	switch {
	case name == "gemini" || strings.HasPrefix(name, "gemini"):
		return "gemini"
	case name == "openrouter":
		return "openrouter"
	case name == "minimax" || strings.HasPrefix(name, "minimax"):
		return "minimax"
	case name == "alibaba" || name == "dashscope" || name == "bailian":
		return "dashscope"
	case name == "openai":
		return "openai"
	case name == "anthropic":
		return "anthropic"
	case name == "suno" || strings.HasPrefix(name, "suno"):
		return "suno"
	case name == "yescale":
		return "openai"
	default:
		return "openai_compat"
	}
}
