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
}

func ResolveReasoningDecision(provider Provider, model, requestedEffort, fallback, source string) ReasoningDecision {
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
