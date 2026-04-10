package providers

import "strings"

// isOpenAINativeEndpoint returns true for endpoints confirmed to be native OpenAI
// infrastructure that accepts the "developer" message role.
// Azure OpenAI, proxies, and other OpenAI-compatible backends only support "system".
// Matching OpenClaw TS: model-compat.ts → isOpenAINativeEndpoint().
func isOpenAINativeEndpoint(apiBase string) bool {
	// Extract hostname from the API base URL.
	lower := strings.ToLower(apiBase)
	return strings.Contains(lower, "api.openai.com")
}

// isFireworksEndpoint returns true for Fireworks AI endpoints.
// Fireworks requires stream=true for max_tokens > 4096.
func (p *OpenAIProvider) isFireworksEndpoint() bool {
	return strings.Contains(strings.ToLower(p.apiBase), "fireworks.ai")
}

// isTogetherEndpoint returns true for Together AI inference hosts.
// Together rejects some OpenAI extensions (e.g. stream_options, reasoning_effort) with HTTP 400.
// Uses URL, provider_type, and name so reverse-proxied Together endpoints are also detected.
func (p *OpenAIProvider) isTogetherEndpoint() bool {
	b := strings.ToLower(p.apiBase)
	if strings.Contains(b, "together.xyz") || strings.Contains(b, "together.ai") {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(p.providerType)), "together") {
		return true
	}
	if strings.Contains(strings.ToLower(p.name), "together") {
		return true
	}
	return false
}

// isDashScopeAPIBase returns true for Alibaba DashScope OpenAI-compatible endpoints.
func isDashScopeAPIBase(apiBase string) bool {
	return strings.Contains(strings.ToLower(apiBase), "dashscope")
}

// dashScopePassthroughKeys is true when enable_thinking / thinking_budget may be added to the JSON body.
// Uses URL, provider_type, and name so httptest DashScope URLs still work in tests.
func (p *OpenAIProvider) dashScopePassthroughKeys() bool {
	if isDashScopeAPIBase(p.apiBase) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(p.providerType)), "dashscope") {
		return true
	}
	if strings.Contains(strings.ToLower(p.name), "dashscope") {
		return true
	}
	return false
}
