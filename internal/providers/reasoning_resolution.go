package providers

import "strings"

const (
	ReasoningFallbackDowngrade       = "downgrade"
	ReasoningFallbackDisable         = "off"
	ReasoningFallbackProviderDefault = "provider_default"
)

type ReasoningDecision struct {
	Source              string   `json:"source,omitempty"`
	RequestedEffort     string   `json:"requested_effort,omitempty"`
	EffectiveEffort     string   `json:"effective_effort,omitempty"`
	Fallback            string   `json:"fallback,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	KnownModel          bool     `json:"known_model,omitempty"`
	SupportedLevels     []string `json:"supported_levels,omitempty"`
	UsedProviderDefault bool     `json:"used_provider_default,omitempty"`
	// StripThinking signals that ChatResponse.Thinking should be dropped by
	// stream handlers (model leaks reasoning even when effort="off").
	// Usage.ThinkingTokens is preserved for billing accuracy.
	StripThinking bool `json:"strip_thinking,omitempty"`
}

// modelLeaksReasoning returns true for models known to emit reasoning tokens
// into the user-visible content even when reasoning effort is "off". Stream
// handlers drop these from ChatResponse.Thinking to avoid leaking raw CoT to
// end users. Billing tokens (Usage.ThinkingTokens) are NOT affected.
func modelLeaksReasoning(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	// Known leakers: Moonshot Kimi (any variant), DeepSeek-Reasoner family,
	// and Gemma 4+ variants (for example "gemma4:8b" or "google/gemma-4-27b-it").
	// Extend this list only after confirming the model does not honor effort="off".
	return strings.Contains(m, "kimi") ||
		strings.Contains(m, "deepseek-reasoner") ||
		strings.Contains(m, "gemma4") ||
		strings.Contains(m, "gemma-4")
}

func ResolveReasoningDecision(provider Provider, model, requestedEffort, fallback, source string) (out ReasoningDecision) {
	// Post-process: when resolved effort is "off" on a known leaky model,
	// mark the decision for downstream stream handlers to strip thinking output.
	defer func() {
		if out.EffectiveEffort == "off" && modelLeaksReasoning(model) {
			out.StripThinking = true
		}
	}()
	decision := ReasoningDecision{
		Source:          normalizeReasoningSource(source),
		RequestedEffort: NormalizeReasoningEffort(requestedEffort),
		Fallback:        NormalizeReasoningFallback(fallback),
	}
	if decision.RequestedEffort == "" {
		decision.RequestedEffort = "off"
	}
	if decision.RequestedEffort == "off" {
		decision.EffectiveEffort = "off"
		return decision
	}
	tc, ok := provider.(ThinkingCapable)
	if !ok || !tc.SupportsThinking() {
		decision.EffectiveEffort = "off"
		decision.Reason = "provider does not support reasoning controls"
		return decision
	}
	capability := LookupReasoningCapability(model)
	if capability == nil {
		if decision.RequestedEffort == "auto" {
			decision.UsedProviderDefault = true
			decision.Reason = "unknown model; leaving provider default reasoning"
			return decision
		}
		decision.EffectiveEffort = decision.RequestedEffort
		decision.Reason = "unknown model; passing requested reasoning effort through"
		return decision
	}

	decision.KnownModel = true
	decision.SupportedLevels = append([]string(nil), capability.Levels...)
	if decision.RequestedEffort == "auto" {
		decision.EffectiveEffort = capability.DefaultEffort
		decision.UsedProviderDefault = true
		decision.Reason = "auto uses the model default reasoning effort"
		return decision
	}
	if capability.Supports(decision.RequestedEffort) {
		decision.EffectiveEffort = decision.RequestedEffort
		return decision
	}

	switch decision.Fallback {
	case ReasoningFallbackDisable:
		decision.EffectiveEffort = "off"
		decision.Reason = "requested reasoning effort is unsupported; disabled by fallback policy"
	case ReasoningFallbackProviderDefault:
		decision.EffectiveEffort = capability.DefaultEffort
		decision.UsedProviderDefault = true
		decision.Reason = "requested reasoning effort is unsupported; using model default"
	default:
		decision.EffectiveEffort = downgradeReasoningLevel(decision.RequestedEffort, capability.Levels)
		if decision.EffectiveEffort == "" {
			decision.EffectiveEffort = "off"
			decision.Reason = "requested reasoning effort is unsupported; disabled because no lower supported level exists"
			return decision
		}
		decision.Reason = "requested reasoning effort is unsupported; downgraded to the highest supported level not exceeding the request"
	}
	return decision
}

func (d ReasoningDecision) RequestEffort() string {
	if d.UsedProviderDefault || d.EffectiveEffort == "" || d.EffectiveEffort == "off" {
		return ""
	}
	return d.EffectiveEffort
}

func (d ReasoningDecision) HasObservation() bool {
	return d.Source != "" && d.Source != "unset"
}

// NormalizeReasoningEffort returns the canonical lowercase effort level if valid, else "".
func NormalizeReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "auto", "none", "minimal", "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

// NormalizeReasoningFallback returns the canonical fallback policy; defaults to "downgrade".
func NormalizeReasoningFallback(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ReasoningFallbackDisable, ReasoningFallbackProviderDefault:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ReasoningFallbackDowngrade
	}
}

func normalizeReasoningSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "reasoning", "thinking_level", "provider_default":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unset"
	}
}

func downgradeReasoningLevel(requested string, supported []string) string {
	ordered := reasoningOrder()
	requestRank, ok := ordered[requested]
	if !ok || len(supported) == 0 {
		return ""
	}
	bestLevel := ""
	bestRank := -1
	for _, level := range supported {
		rank, ok := ordered[level]
		if !ok {
			continue
		}
		if rank <= requestRank && rank > bestRank {
			bestLevel = level
			bestRank = rank
		}
	}
	return bestLevel
}

func reasoningOrder() map[string]int {
	return map[string]int{
		"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5,
	}
}
