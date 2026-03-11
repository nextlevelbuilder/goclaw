package tracing

import (
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// CalculateCost computes the USD cost for a single LLM call based on token usage and pricing.
// Returns 0 if pricing is nil.
func CalculateCost(pricing *config.ModelPricing, usage *providers.Usage) float64 {
	if pricing == nil || usage == nil {
		return 0
	}
	cost := float64(usage.PromptTokens) * pricing.InputPerMillion / 1_000_000
	cost += float64(usage.CompletionTokens) * pricing.OutputPerMillion / 1_000_000
	if pricing.CacheReadPerMillion > 0 && usage.CacheReadTokens > 0 {
		cost += float64(usage.CacheReadTokens) * pricing.CacheReadPerMillion / 1_000_000
	}
	if pricing.CacheCreatePerMillion > 0 && usage.CacheCreationTokens > 0 {
		cost += float64(usage.CacheCreationTokens) * pricing.CacheCreatePerMillion / 1_000_000
	}
	return cost
}

// LookupPricing finds the model pricing from config.
// Tries "provider/model" first, then just "model".
func LookupPricing(pricingMap map[string]*config.ModelPricing, provider, model string) *config.ModelPricing {
	if pricingMap == nil {
		return nil
	}
	if p, ok := pricingMap[provider+"/"+model]; ok {
		return p
	}
	if p, ok := pricingMap[model]; ok {
		return p
	}
	return nil
}
