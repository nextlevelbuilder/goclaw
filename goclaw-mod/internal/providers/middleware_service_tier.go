package providers

import "log/slog"

// Valid service_tier values per provider.
var anthropicValidTiers = map[string]bool{
	"auto": true, "standard_only": true,
}

var openaiValidTiers = map[string]bool{
	"auto": true, "default": true, "flex": true, "priority": true,
}

// ServiceTierMiddleware injects service_tier from Options.
// Validates values per provider. Skips for OAuth Anthropic (API rejects it).
// Does not override if service_tier is already set in body.
func ServiceTierMiddleware(body map[string]any, cfg MiddlewareConfig) map[string]any {
	tierVal, ok := cfg.Options[OptServiceTier]
	if !ok {
		return body
	}
	tier, isStr := tierVal.(string)
	if !isStr || tier == "" {
		return body
	}

	// Don't override if already set (e.g. by FastModeMiddleware)
	if _, exists := body["service_tier"]; exists {
		return body
	}

	switch cfg.Provider {
	case "anthropic":
		if cfg.AuthType == "oauth" {
			return body // Anthropic OAuth rejects service_tier
		}
		if !anthropicValidTiers[tier] {
			slog.Warn("middleware: invalid anthropic service_tier", "tier", tier)
			return body
		}
	default:
		// OpenAI and compatible
		if !openaiValidTiers[tier] {
			slog.Warn("middleware: invalid openai service_tier", "tier", tier)
			return body
		}
	}

	body["service_tier"] = tier
	return body
}

// FastModeMiddleware maps the fast_mode boolean option to service_tier.
// Anthropic: true→"auto", false→"standard_only".
// OpenAI: true→"priority".
// Does not override if service_tier is already set in body.
func FastModeMiddleware(body map[string]any, cfg MiddlewareConfig) map[string]any {
	val, ok := cfg.Options[OptFastMode]
	if !ok {
		return body
	}
	fast, isBool := val.(bool)
	if !isBool {
		return body
	}

	// Don't override explicit service_tier
	if _, exists := body["service_tier"]; exists {
		return body
	}

	switch cfg.Provider {
	case "anthropic":
		if cfg.AuthType == "oauth" {
			return body
		}
		if fast {
			body["service_tier"] = "auto"
		} else {
			body["service_tier"] = "standard_only"
		}
	default:
		// OpenAI and compatible
		if fast {
			body["service_tier"] = "priority"
		}
	}

	return body
}
